package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hases/hases-api/internal/domain"
)

// ---------- Templates por vacante ----------

// listFunctionalActivityTemplates devuelve las plantillas para una vacante.
func (s *Server) listFunctionalActivityTemplates(w http.ResponseWriter, r *http.Request) {
	vid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "vacancy id"})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, phase, sort_order, title, description, evidence_required, audiovisual_file_id
		FROM functional_activity_templates
		WHERE vacancy_id=$1
		ORDER BY phase, sort_order`, vid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var phase, title, desc string
		var so int
		var er bool
		var avFid pgtype.UUID
		_ = rows.Scan(&id, &phase, &so, &title, &desc, &er, &avFid)
		row := map[string]any{
			"id": id.String(), "phase": phase, "sort_order": so, "title": title,
			"description": desc, "evidence_required": er,
			"evidence_notes":    "",
			"evidence_file_ids": []string{},
		}
		if avFid.Valid {
			row["audiovisual_file_id"] = uuid.UUID(avFid.Bytes).String()
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createFunctionalActivityTemplate(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	vid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "vacancy id"})
		return
	}
	var body struct {
		Phase            string  `json:"phase"`
		SortOrder        int     `json:"sort_order"`
		Title            string  `json:"title"`
		Description      string  `json:"description"`
		EvidenceRequired bool    `json:"evidence_required"`
		AudiovisualFile  *string `json:"audiovisual_file_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	switch body.Phase {
	case "theory", "practice":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phase must be theory|practice"})
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}
	var avFid *uuid.UUID
	if body.AudiovisualFile != nil {
		if id, err := uuid.Parse(*body.AudiovisualFile); err == nil {
			avFid = &id
		}
	}
	var tid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO functional_activity_templates
			(vacancy_id, phase, sort_order, title, description, evidence_required, audiovisual_file_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		vid, body.Phase, body.SortOrder, body.Title, body.Description, body.EvidenceRequired, avFid).Scan(&tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": tid.String()})
}

func (s *Server) patchFunctionalActivityTemplate(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	tid, ok := mustUUID(chi.URLParam(r, "tid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tid"})
		return
	}
	var body struct {
		Phase            *string `json:"phase"`
		SortOrder        *int    `json:"sort_order"`
		Title            *string `json:"title"`
		Description      *string `json:"description"`
		EvidenceRequired *bool   `json:"evidence_required"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE functional_activity_templates SET
			phase = COALESCE($1, phase),
			sort_order = COALESCE($2, sort_order),
			title = COALESCE($3, title),
			description = COALESCE($4, description),
			evidence_required = COALESCE($5, evidence_required)
		WHERE id=$6`,
		body.Phase, body.SortOrder, body.Title, body.Description, body.EvidenceRequired, tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) deleteFunctionalActivityTemplate(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	tid, ok := mustUUID(chi.URLParam(r, "tid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tid"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `DELETE FROM functional_activity_templates WHERE id=$1`, tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Activities por aplicación ----------

// snapshotFunctionalActivities crea las `functional_activities` para esta
// aplicación a partir de la plantilla de su vacante. Idempotente: si ya
// existen no las duplica.
func (s *Server) snapshotFunctionalActivities(ctx context.Context, applicationID uuid.UUID) error {
	var existing int
	if err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM functional_activities WHERE application_id=$1`, applicationID,
	).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO functional_activities
			(application_id, template_id, phase, sort_order, title, description, evidence_required, audiovisual_file_id)
		SELECT $1, t.id, t.phase, t.sort_order, t.title, t.description, t.evidence_required, t.audiovisual_file_id
		FROM functional_activity_templates t
		JOIN applications a ON a.vacancy_id = t.vacancy_id
		WHERE a.id = $1`, applicationID)
	return err
}

func (s *Server) listFunctionalActivities(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	if err := s.snapshotFunctionalActivities(r.Context(), aid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.queryFunctionalActivities(r.Context(), aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) workerListFunctionalActivities(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	if err := s.snapshotFunctionalActivities(r.Context(), aid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.queryFunctionalActivities(r.Context(), aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) queryFunctionalActivities(ctx context.Context, aid uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, phase, sort_order, title, description, evidence_required,
		       audiovisual_file_id, completed_at::text, evidence_notes, evidence_file_ids
		FROM functional_activities
		WHERE application_id=$1
		ORDER BY phase, sort_order`, aid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var phase, title, desc, notes string
		var so int
		var er bool
		var avFid pgtype.UUID
		var ca pgtype.Text
		var fileIDs []uuid.UUID
		if err := rows.Scan(&id, &phase, &so, &title, &desc, &er, &avFid, &ca, &notes, &fileIDs); err != nil {
			return nil, err
		}
		row := map[string]any{
			"id": id.String(), "phase": phase, "sort_order": so, "title": title,
			"description": desc, "evidence_required": er, "evidence_notes": notes,
		}
		if ca.Valid {
			row["completed_at"] = ca.String
		}
		if avFid.Valid {
			row["audiovisual_file_id"] = uuid.UUID(avFid.Bytes).String()
		}
		ids := make([]string, 0, len(fileIDs))
		for _, f := range fileIDs {
			ids = append(ids, f.String())
		}
		row["evidence_file_ids"] = ids
		out = append(out, row)
	}
	return out, nil
}

func (s *Server) completeFunctionalActivity(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	actID, ok := mustUUID(chi.URLParam(r, "aid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "aid"})
		return
	}
	s.handleCompleteFunctionalActivity(w, r, aid, actID)
}

func (s *Server) workerCompleteFunctionalActivity(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	actID, ok := mustUUID(chi.URLParam(r, "aid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "aid"})
		return
	}
	s.handleCompleteFunctionalActivity(w, r, aid, actID)
}

// handleCompleteFunctionalActivity acepta multipart con `notes` y `files`
// o JSON con `notes`. Guarda evidencia y marca completed_at.
func (s *Server) handleCompleteFunctionalActivity(w http.ResponseWriter, r *http.Request, aid, actID uuid.UUID) {
	ct := r.Header.Get("Content-Type")
	var notes string
	var fileIDs []uuid.UUID
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
			return
		}
		notes = r.FormValue("notes")
		if r.MultipartForm != nil && r.MultipartForm.File != nil {
			for _, hdrs := range r.MultipartForm.File {
				for _, hdr := range hdrs {
					fid, status, err := s.persistMultipartHeader(r.Context(), hdr)
					if err != nil {
						writeJSON(w, status, map[string]string{"error": err.Error()})
						return
					}
					fileIDs = append(fileIDs, fid)
				}
			}
		}
	} else {
		var body struct {
			Notes string `json:"notes"`
		}
		_ = readJSON(r, &body)
		notes = body.Notes
	}

	var evidenceRequired bool
	var phase string
	err := s.Pool.QueryRow(r.Context(),
		`SELECT evidence_required, phase FROM functional_activities WHERE id=$1 AND application_id=$2`,
		actID, aid,
	).Scan(&evidenceRequired, &phase)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "activity not found"})
		return
	}
	if evidenceRequired && len(fileIDs) == 0 && strings.TrimSpace(notes) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "evidence_required"})
		return
	}

	cl := ClaimsFromCtx(r)
	var completedBy *uuid.UUID
	if cl != nil {
		completedBy = &cl.UserID
	}
	_, err = s.Pool.Exec(r.Context(), `
		UPDATE functional_activities
		SET completed_at = now(),
		    completed_by = $1,
		    evidence_notes = $2,
		    evidence_file_ids = $3
		WHERE id=$4 AND application_id=$5`,
		completedBy, notes, fileIDs, actID, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "phase": phase})
}

// allActivitiesCompleted comprueba que no quedan actividades de cierta fase
// pendientes en una postulación.
func (s *Server) allActivitiesCompleted(ctx context.Context, aid uuid.UUID, phase string) (bool, error) {
	var pending int
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM functional_activities
		WHERE application_id=$1 AND phase=$2 AND completed_at IS NULL`,
		aid, phase).Scan(&pending)
	return pending == 0, err
}

// Asegurar que `completeTheory`, `startPractice` y `completeFunctional`
// validen el cronograma. Ver wrappers en handlers_rest.go (sustitución).
var _ = domain.StatusInductionPractice
