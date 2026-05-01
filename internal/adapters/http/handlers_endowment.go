package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// recordEndowmentDelivery acepta multipart o JSON, con un campo `kind`
// (epp|dotacion). Reusa el storage de archivos para la firma.
func (s *Server) recordEndowmentDelivery(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	ct := r.Header.Get("Content-Type")
	var kind string
	var itemsRaw []byte
	var sigFileID *uuid.UUID
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
			return
		}
		kind = strings.TrimSpace(r.FormValue("kind"))
		itemsRaw = []byte(r.FormValue("items"))
		if r.MultipartForm != nil && r.MultipartForm.File != nil {
			if _, ok := r.MultipartForm.File["signature"]; ok {
				fid, status, err := s.persistFormFile(r, "signature")
				if err != nil {
					writeJSON(w, status, map[string]string{"error": err.Error()})
					return
				}
				sigFileID = &fid
			}
		}
	} else {
		var body struct {
			Kind  string          `json:"kind"`
			Items json.RawMessage `json:"items"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
			return
		}
		kind = body.Kind
		itemsRaw = body.Items
	}
	switch kind {
	case "epp", "dotacion":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind must be epp|dotacion"})
		return
	}
	if len(itemsRaw) == 0 {
		itemsRaw = []byte("[]")
	}

	if sigFileID != nil {
		_, err := s.Pool.Exec(r.Context(), `
			INSERT INTO endowment_deliveries (application_id, kind, items_json, signature_file_id)
			VALUES ($1, $2, COALESCE($3::jsonb, '[]'::jsonb), $4)
			ON CONFLICT (application_id, kind) DO UPDATE SET
				items_json = EXCLUDED.items_json,
				signature_file_id = EXCLUDED.signature_file_id,
				delivered_at = now()`,
			aid, kind, itemsRaw, *sigFileID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		_, err := s.Pool.Exec(r.Context(), `
			INSERT INTO endowment_deliveries (application_id, kind, items_json)
			VALUES ($1, $2, COALESCE($3::jsonb, '[]'::jsonb))
			ON CONFLICT (application_id, kind) DO UPDATE SET
				items_json = EXCLUDED.items_json,
				delivered_at = now()`,
			aid, kind, itemsRaw)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	// Mantener compatibilidad con `epp_deliveries` (lo lee `startPractice`).
	if kind == "epp" {
		if sigFileID != nil {
			_, _ = s.Pool.Exec(r.Context(), `
				INSERT INTO epp_deliveries (application_id, items_json, signature_file_id)
				VALUES ($1, COALESCE($2::jsonb, '[]'::jsonb), $3)
				ON CONFLICT (application_id) DO UPDATE SET
					items_json=EXCLUDED.items_json,
					signature_file_id=EXCLUDED.signature_file_id,
					delivered_at=now()`,
				aid, itemsRaw, *sigFileID)
		} else {
			_, _ = s.Pool.Exec(r.Context(), `
				INSERT INTO epp_deliveries (application_id, items_json)
				VALUES ($1, COALESCE($2::jsonb, '[]'::jsonb))
				ON CONFLICT (application_id) DO UPDATE SET
					items_json=EXCLUDED.items_json, delivered_at=now()`,
				aid, itemsRaw)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "kind": kind})
}

func (s *Server) listEndowmentDeliveries(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, kind, items_json::text, delivered_at::text, signature_file_id
		FROM endowment_deliveries WHERE application_id=$1 ORDER BY kind`, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var kind, items, dt string
		var fid pgtype.UUID
		_ = rows.Scan(&id, &kind, &items, &dt, &fid)
		row := map[string]any{
			"id": id.String(), "kind": kind,
			"items_json":   json.RawMessage(items),
			"delivered_at": dt,
		}
		if fid.Valid {
			row["signature_file_id"] = uuid.UUID(fid.Bytes).String()
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}
