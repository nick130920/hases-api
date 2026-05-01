package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// RoleManual representa el manual de funciones (texto + archivo) de una vacante.
type RoleManual struct {
	VacancyID string `json:"vacancy_id"`
	Body      string `json:"body"`
	FileID    string `json:"file_id,omitempty"`
}

func (s *Server) getRoleManual(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body string
	var fid pgtype.UUID
	err := s.Pool.QueryRow(r.Context(),
		`SELECT role_manual_body, role_manual_file_id FROM vacancies WHERE id=$1`, id,
	).Scan(&body, &fid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "vacancy not found"})
		return
	}
	out := RoleManual{VacancyID: id.String(), Body: body}
	if fid.Valid {
		out.FileID = uuid.UUID(fid.Bytes).String()
	}
	writeJSON(w, http.StatusOK, out)
}

// patchRoleManual acepta JSON con body, o multipart con body + file.
// Roles permitidos: admin, hr.
func (s *Server) patchRoleManual(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	ct := r.Header.Get("Content-Type")
	var newBody *string
	var newFileID *uuid.UUID
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
			return
		}
		if v := r.FormValue("body"); v != "" {
			newBody = &v
		}
		if r.MultipartForm != nil && r.MultipartForm.File != nil {
			if _, ok := r.MultipartForm.File["file"]; ok {
				fid, status, err := s.persistFormFile(r, "file")
				if err != nil {
					writeJSON(w, status, map[string]string{"error": err.Error()})
					return
				}
				newFileID = &fid
			}
		}
	} else {
		var body struct {
			Body *string `json:"body"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
			return
		}
		newBody = body.Body
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE vacancies SET
			role_manual_body    = COALESCE($1, role_manual_body),
			role_manual_file_id = COALESCE($2, role_manual_file_id),
			updated_at = now()
		WHERE id=$3`,
		newBody, newFileID, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "vacancy", &id, "role_manual:update", nil)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
