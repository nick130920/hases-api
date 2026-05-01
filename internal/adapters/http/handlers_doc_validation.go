package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// DocumentTypeView combina catalog metadata para el frontend.
type DocumentTypeView struct {
	ItemKey          string `json:"item_key"`
	Label            string `json:"label"`
	RequiresVehicle  bool   `json:"requires_vehicle"`
	TypicalRequired  bool   `json:"typical_required"`
	MaxAgeDays       *int   `json:"max_age_days,omitempty"`
	RequiresTemplate bool   `json:"requires_template"`
	RequiresIssuedAt bool   `json:"requires_issued_at"`
	HasTemplate      bool   `json:"has_template"`
	TemplateFileID   string `json:"template_file_id,omitempty"`
}

// listDocumentTypes expone el catálogo de tipos de documento con metadatos
// (max_age_days, requires_template, etc.) para validar en el cliente.
func (s *Server) listDocumentTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT dt.item_key, dt.label, dt.requires_vehicle, dt.typical_required,
		       dt.max_age_days, dt.requires_template, dt.requires_issued_at,
		       tpl.file_id
		FROM document_types dt
		LEFT JOIN document_templates tpl ON tpl.item_key = dt.item_key
		ORDER BY dt.id`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []DocumentTypeView
	for rows.Next() {
		var v DocumentTypeView
		var maxAge pgtype.Int4
		var fid pgtype.UUID
		if err := rows.Scan(&v.ItemKey, &v.Label, &v.RequiresVehicle, &v.TypicalRequired,
			&maxAge, &v.RequiresTemplate, &v.RequiresIssuedAt, &fid); err != nil {
			continue
		}
		if maxAge.Valid {
			n := int(maxAge.Int32)
			v.MaxAgeDays = &n
		}
		if fid.Valid {
			v.HasTemplate = true
			v.TemplateFileID = uuid.UUID(fid.Bytes).String()
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// downloadDocumentTemplate sirve la plantilla oficial de un item_key (ej. HV).
// No requiere autenticación: las plantillas son públicas, así el postulante
// puede descargarlas antes de tener cuenta.
func (s *Server) downloadDocumentTemplate(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "itemKey"))
	if key == "" {
		http.Error(w, "item_key", http.StatusBadRequest)
		return
	}
	var fileID uuid.UUID
	err := s.Pool.QueryRow(r.Context(),
		`SELECT file_id FROM document_templates WHERE item_key=$1`, key,
	).Scan(&fileID)
	if err != nil {
		http.Error(w, "no template", http.StatusNotFound)
		return
	}
	s.streamFileByID(w, r, fileID, false)
}

// uploadDocumentTemplate (admin/hr) sube/reemplaza la plantilla oficial
// para un item_key del catálogo de tipos de documento.
func (s *Server) uploadDocumentTemplate(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	key := strings.TrimSpace(chi.URLParam(r, "itemKey"))
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "item_key"})
		return
	}
	var label string
	err := s.Pool.QueryRow(r.Context(),
		`SELECT label FROM document_types WHERE item_key=$1`, key,
	).Scan(&label)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item_key not in catalog"})
		return
	}
	if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
		return
	}
	fid, status, err := s.persistFormFile(r, "file")
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	_, err = s.Pool.Exec(r.Context(), `
		INSERT INTO document_templates (item_key, label, file_id) VALUES ($1,$2,$3)
		ON CONFLICT (item_key) DO UPDATE SET file_id=EXCLUDED.file_id, label=EXCLUDED.label, uploaded_at=now()`,
		key, label, fid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"file_id": fid.String(), "item_key": key})
}

// validateIssuedAt comprueba que (ahora - issued_at) <= max_age_days.
// Devuelve error con mensaje descriptivo si no cumple.
func validateIssuedAt(issuedAt time.Time, maxAgeDays int) (bool, string) {
	if maxAgeDays <= 0 {
		return true, ""
	}
	limit := time.Now().AddDate(0, 0, -maxAgeDays)
	if issuedAt.Before(limit) {
		return false, "documento expirado: maximo " + itoa(maxAgeDays) + " dias de antiguedad"
	}
	return true, ""
}
