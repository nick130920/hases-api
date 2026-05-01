package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// uploadInductionMedia agrega un recurso (video/imagen/pdf/audio) a un módulo
// de inducción organizacional. Roles: admin, hr.
func (s *Server) uploadInductionMedia(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	mid, ok := mustUUID(chi.URLParam(r, "mid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "module id"})
		return
	}
	if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	switch kind {
	case "video", "image", "pdf", "audio":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind must be video|image|pdf|audio"})
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	var duration *int
	if d := strings.TrimSpace(r.FormValue("duration_seconds")); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			duration = &n
		}
	}
	fid, status, err := s.persistFormFile(r, "file")
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var mediaID uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO induction_org_media (module_id, file_id, kind, title, sort_order, duration_seconds)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		mid, fid, kind, title, sortOrder, duration).Scan(&mediaID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":               mediaID.String(),
		"file_id":          fid.String(),
		"kind":             kind,
		"title":            title,
		"sort_order":       sortOrder,
		"duration_seconds": duration,
	})
}

func (s *Server) deleteInductionMedia(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	id, ok := mustUUID(chi.URLParam(r, "mediaID"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `DELETE FROM induction_org_media WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listInductionModulesWithMedia (sustituye al listado simple) trae los módulos
// y, opcionalmente, sus media. La estructura sigue siendo retro-compatible.
func (s *Server) listInductionModulesEnriched(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, title, body, sort_order FROM induction_org_modules ORDER BY sort_order`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type module struct {
		ID        string           `json:"id"`
		Title     string           `json:"title"`
		Body      string           `json:"body"`
		SortOrder int              `json:"sort_order"`
		Media     []map[string]any `json:"media"`
	}
	var out []module
	moduleByID := map[string]int{}
	for rows.Next() {
		var id uuid.UUID
		var title, body string
		var so int
		_ = rows.Scan(&id, &title, &body, &so)
		moduleByID[id.String()] = len(out)
		out = append(out, module{ID: id.String(), Title: title, Body: body, SortOrder: so, Media: []map[string]any{}})
	}
	mrows, err := s.Pool.Query(r.Context(), `
		SELECT module_id, id, file_id, kind, title, sort_order, duration_seconds
		FROM induction_org_media ORDER BY module_id, sort_order`)
	if err == nil {
		defer mrows.Close()
		for mrows.Next() {
			var modID, mid, fid uuid.UUID
			var kind, mtitle string
			var so int
			var dur pgtype.Int4
			_ = mrows.Scan(&modID, &mid, &fid, &kind, &mtitle, &so, &dur)
			idx, ok := moduleByID[modID.String()]
			if !ok {
				continue
			}
			m := map[string]any{
				"id": mid.String(), "file_id": fid.String(), "kind": kind,
				"title": mtitle, "sort_order": so,
			}
			if dur.Valid {
				m["duration_seconds"] = int(dur.Int32)
			}
			out[idx].Media = append(out[idx].Media, m)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// workerListInductionModules: lista módulos + media + progreso del worker.
func (s *Server) workerListInductionModules(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	rows, err := s.Pool.Query(r.Context(), `
		SELECT m.id, m.title, m.body, m.sort_order,
		       p.completed_at::text, p.viewed_seconds
		FROM induction_org_modules m
		LEFT JOIN induction_org_progress p ON p.module_id = m.id AND p.application_id = $1
		ORDER BY m.sort_order`, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type module struct {
		ID            string           `json:"id"`
		Title         string           `json:"title"`
		Body          string           `json:"body"`
		SortOrder     int              `json:"sort_order"`
		CompletedAt   string           `json:"completed_at,omitempty"`
		ViewedSeconds int              `json:"viewed_seconds"`
		Media         []map[string]any `json:"media"`
	}
	var out []module
	idx := map[string]int{}
	for rows.Next() {
		var id uuid.UUID
		var title, body string
		var so int
		var ca pgtype.Text
		var vs pgtype.Int4
		_ = rows.Scan(&id, &title, &body, &so, &ca, &vs)
		m := module{ID: id.String(), Title: title, Body: body, SortOrder: so, Media: []map[string]any{}}
		if ca.Valid {
			m.CompletedAt = ca.String
		}
		if vs.Valid {
			m.ViewedSeconds = int(vs.Int32)
		}
		idx[id.String()] = len(out)
		out = append(out, m)
	}

	mrows, err := s.Pool.Query(r.Context(), `
		SELECT module_id, id, file_id, kind, title, sort_order, duration_seconds
		FROM induction_org_media ORDER BY module_id, sort_order`)
	if err == nil {
		defer mrows.Close()
		for mrows.Next() {
			var modID, mid, fid uuid.UUID
			var kind, mtitle string
			var so int
			var dur pgtype.Int4
			_ = mrows.Scan(&modID, &mid, &fid, &kind, &mtitle, &so, &dur)
			i, ok := idx[modID.String()]
			if !ok {
				continue
			}
			m := map[string]any{
				"id": mid.String(), "file_id": fid.String(), "kind": kind,
				"title": mtitle, "sort_order": so,
			}
			if dur.Valid {
				m["duration_seconds"] = int(dur.Int32)
			}
			out[i].Media = append(out[i].Media, m)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// workerProgressTick recibe `viewed_seconds` y lo persiste; si llega
// al `duration_seconds` de algún media del módulo, marca `completed_at`.
func (s *Server) workerProgressTick(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	moduleID, ok := mustUUID(chi.URLParam(r, "moduleID"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "moduleID"})
		return
	}
	var body struct {
		ViewedSeconds int  `json:"viewed_seconds"`
		MarkComplete  bool `json:"mark_complete"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	var maxDuration pgtype.Int4
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT COALESCE(MAX(duration_seconds), 0) FROM induction_org_media WHERE module_id=$1`,
		moduleID).Scan(&maxDuration)

	completed := body.MarkComplete
	if !completed && maxDuration.Valid && body.ViewedSeconds >= int(maxDuration.Int32) && maxDuration.Int32 > 0 {
		completed = true
	}
	var completedAt any
	if completed {
		completedAt = "now()"
	}
	if completed {
		_, err := s.Pool.Exec(r.Context(), `
			INSERT INTO induction_org_progress (application_id, module_id, viewed_seconds, last_viewed_at, completed_at)
			VALUES ($1,$2,$3, now(), now())
			ON CONFLICT (application_id, module_id) DO UPDATE SET
				viewed_seconds = GREATEST(induction_org_progress.viewed_seconds, EXCLUDED.viewed_seconds),
				last_viewed_at = now(),
				completed_at = COALESCE(induction_org_progress.completed_at, now())`,
			aid, moduleID, body.ViewedSeconds)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		_, err := s.Pool.Exec(r.Context(), `
			INSERT INTO induction_org_progress (application_id, module_id, viewed_seconds, last_viewed_at)
			VALUES ($1,$2,$3, now())
			ON CONFLICT (application_id, module_id) DO UPDATE SET
				viewed_seconds = GREATEST(induction_org_progress.viewed_seconds, EXCLUDED.viewed_seconds),
				last_viewed_at = now()`,
			aid, moduleID, body.ViewedSeconds)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "completed": completed, "completed_at_set": completedAt})
}
