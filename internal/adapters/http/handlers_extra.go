package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hases/hases-api/internal/auth"
)

// ---------- Vacancies extras ----------

func (s *Server) archiveVacancy(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE vacancies SET status='closed', closed_at=now(), updated_at=now() WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var uid *uuid.UUID
	if cl != nil {
		uid = &cl.UserID
	}
	s.audit(r.Context(), uid, "vacancy", &id, "archive", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// ---------- Applications: completeness ----------

type Completeness struct {
	Total             int  `json:"total"`
	WithFile          int  `json:"with_file"`
	Approved          int  `json:"approved"`
	Rejected          int  `json:"rejected"`
	Pending           int  `json:"pending"`
	RequiredTotal     int  `json:"required_total"`
	RequiredSatisfied int  `json:"required_satisfied"`
	Complete          bool `json:"complete"`
}

func (s *Server) computeCompleteness(ctx context.Context, applicationID uuid.UUID) (Completeness, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT i.required, (d.file_id IS NOT NULL) AS has_file, d.review_status
		FROM application_documents d
		JOIN checklist_items i ON i.id = d.checklist_item_id
		WHERE d.application_id = $1`, applicationID)
	if err != nil {
		return Completeness{}, err
	}
	defer rows.Close()
	var c Completeness
	for rows.Next() {
		var required, hasFile bool
		var status string
		if err := rows.Scan(&required, &hasFile, &status); err != nil {
			return c, err
		}
		c.Total++
		if hasFile {
			c.WithFile++
		}
		switch status {
		case "approved":
			c.Approved++
		case "rejected":
			c.Rejected++
		default:
			c.Pending++
		}
		if required {
			c.RequiredTotal++
			if hasFile && status == "approved" {
				c.RequiredSatisfied++
			}
		}
	}
	c.Complete = c.RequiredTotal > 0 && c.RequiredSatisfied == c.RequiredTotal
	return c, rows.Err()
}

func (s *Server) getCompleteness(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	c, err := s.computeCompleteness(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ---------- Documents: review ----------

func (s *Server) reviewDocument(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	docID, ok := mustUUID(chi.URLParam(r, "docID"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "docID"})
		return
	}
	var body struct {
		ReviewStatus string `json:"review_status"`
		Notes        string `json:"notes"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	switch body.ReviewStatus {
	case "pending", "approved", "rejected":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "review_status"})
		return
	}
	cl := ClaimsFromCtx(r)
	var reviewer pgtype.UUID
	if cl != nil {
		reviewer.Bytes = [16]byte(cl.UserID)
		reviewer.Valid = true
	}
	tag, err := s.Pool.Exec(r.Context(), `
		UPDATE application_documents
		SET review_status=$1, reviewer_notes=$2, reviewed_by=$3, reviewed_at=now()
		WHERE id=$4 AND application_id=$5`,
		body.ReviewStatus, body.Notes, reviewer, docID, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "application_document", &docID, "review:"+body.ReviewStatus, nil)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ---------- Interview sessions: list / patch ----------

func (s *Server) listInterviewSessions(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, application_id, template_id, scheduled_at::text, location, modality, interviewer_notes, created_at::text
		FROM interview_sessions WHERE application_id=$1 ORDER BY created_at DESC`, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var sid, appID, tid uuid.UUID
		var sch pgtype.Text
		var loc, mod, notes, ca string
		_ = rows.Scan(&sid, &appID, &tid, &sch, &loc, &mod, &notes, &ca)
		out = append(out, map[string]any{
			"id":                sid.String(),
			"application_id":    appID.String(),
			"template_id":       tid.String(),
			"scheduled_at":      sch.String,
			"location":          loc,
			"modality":          mod,
			"interviewer_notes": notes,
			"created_at":        ca,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) patchInterviewSession(w http.ResponseWriter, r *http.Request) {
	sid, ok := mustUUID(chi.URLParam(r, "sid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sid"})
		return
	}
	var body struct {
		ScheduledAt      *string `json:"scheduled_at"`
		Location         *string `json:"location"`
		Modality         *string `json:"modality"`
		InterviewerNotes *string `json:"interviewer_notes"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	var sched any
	if body.ScheduledAt != nil && strings.TrimSpace(*body.ScheduledAt) != "" {
		sched = *body.ScheduledAt
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE interview_sessions SET
			scheduled_at = COALESCE($1::timestamptz, scheduled_at),
			location = COALESCE($2, location),
			modality = COALESCE($3, modality),
			interviewer_notes = COALESCE($4, interviewer_notes)
		WHERE id=$5`,
		sched, body.Location, body.Modality, body.InterviewerNotes, sid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ---------- Induction signatures: multipart ----------

func (s *Server) addInductionSignatureMultipart(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	switch kind {
	case "regulation", "policies", "contract":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind"})
		return
	}
	fid, status, err := s.persistFormFile(r, "file")
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var sigID uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO induction_signatures (application_id, kind, signature_file_id, metadata)
		VALUES ($1,$2,$3,'{}'::jsonb) RETURNING id`,
		aid, kind, fid).Scan(&sigID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "induction_signature", &sigID, "create:"+kind, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": sigID.String(), "file_id": fid.String()})
}

// ---------- EPP delivery con firma opcional ----------

func (s *Server) recordEPPDeliveryUnified(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	ct := r.Header.Get("Content-Type")
	var itemsRaw []byte
	var sigFileID *uuid.UUID
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
			return
		}
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
			Items json.RawMessage `json:"items"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
			return
		}
		itemsRaw = body.Items
	}
	if len(itemsRaw) == 0 {
		itemsRaw = []byte("[]")
	}
	if sigFileID != nil {
		_, err := s.Pool.Exec(r.Context(), `
			INSERT INTO epp_deliveries (application_id, items_json, signature_file_id)
			VALUES ($1, COALESCE($2::jsonb, '[]'::jsonb), $3)
			ON CONFLICT (application_id) DO UPDATE SET
				items_json=EXCLUDED.items_json,
				signature_file_id=EXCLUDED.signature_file_id,
				delivered_at=now()`,
			aid, itemsRaw, *sigFileID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		_, err := s.Pool.Exec(r.Context(), `
			INSERT INTO epp_deliveries (application_id, items_json)
			VALUES ($1, COALESCE($2::jsonb, '[]'::jsonb))
			ON CONFLICT (application_id) DO UPDATE SET
				items_json=EXCLUDED.items_json, delivered_at=now()`,
			aid, itemsRaw)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ---------- Functional evidence con archivos ----------

func (s *Server) addFunctionalEvidenceUnified(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	ct := r.Header.Get("Content-Type")
	var phase, notes, actor string
	var fileIDs []uuid.UUID
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
			return
		}
		phase = r.FormValue("phase")
		notes = r.FormValue("notes")
		actor = r.FormValue("actor")
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
			Phase string `json:"phase"`
			Notes string `json:"notes"`
			Actor string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
			return
		}
		phase, notes, actor = body.Phase, body.Notes, body.Actor
	}
	if phase == "" {
		phase = "practice"
	}
	if actor == "" {
		actor = "worker"
	}
	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO functional_evidence (application_id, phase, notes, actor, file_ids)
		VALUES ($1,$2,$3,$4,$5)`,
		aid, phase, notes, actor, fileIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

// ---------- Catálogo: motivos de rechazo ----------

func (s *Server) listRejectionReasons(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT id, label FROM rejection_reasons ORDER BY label`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int
		var label string
		_ = rows.Scan(&id, &label)
		out = append(out, map[string]any{"id": id, "label": label})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createRejectionReason(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Label) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "label"})
		return
	}
	var id int
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO rejection_reasons (label) VALUES ($1)
		ON CONFLICT (label) DO UPDATE SET label=EXCLUDED.label RETURNING id`,
		strings.TrimSpace(body.Label)).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "label": strings.TrimSpace(body.Label)})
}

func (s *Server) deleteRejectionReason(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	idStr := chi.URLParam(r, "id")
	_, err := s.Pool.Exec(r.Context(), `DELETE FROM rejection_reasons WHERE id=$1`, idStr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Users: patch / disable ----------

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		FullName *string `json:"full_name"`
		Role     *string `json:"role"`
		Active   *bool   `json:"active"`
		Password *string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	var hash *string
	if body.Password != nil && strings.TrimSpace(*body.Password) != "" {
		h, err := auth.HashPassword(*body.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hash"})
			return
		}
		hash = &h
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE users SET
			full_name = COALESCE($1, full_name),
			role      = COALESCE($2, role),
			active    = COALESCE($3, active),
			password_hash = COALESCE($4, password_hash)
		WHERE id=$5`,
		body.FullName, body.Role, body.Active, hash, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "user", &id, "patch", nil)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) deactivateUser(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `UPDATE users SET active=false WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "user", &id, "deactivate", nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- file helpers compartidos ----------

// persistFormFile lee r.FormFile(field) y persiste el archivo aplicando
// validación de MIME y límite de tamaño.
func (s *Server) persistFormFile(r *http.Request, field string) (uuid.UUID, int, error) {
	f, hdr, err := r.FormFile(field)
	if err != nil {
		return uuid.Nil, http.StatusBadRequest, errors.New("file")
	}
	defer f.Close()
	return s.writeFileFromReader(r.Context(), f, hdr.Filename, hdr.Header.Get("Content-Type"), hdr.Size)
}

// persistMultipartHeader persiste un FileHeader recibido por multipart.
func (s *Server) persistMultipartHeader(ctx context.Context, hdr *multipart.FileHeader) (uuid.UUID, int, error) {
	f, err := hdr.Open()
	if err != nil {
		return uuid.Nil, http.StatusBadRequest, err
	}
	defer f.Close()
	return s.writeFileFromReader(ctx, f, hdr.Filename, hdr.Header.Get("Content-Type"), hdr.Size)
}

func (s *Server) writeFileFromReader(ctx context.Context, src io.Reader, filename, mime string, declaredSize int64) (uuid.UUID, int, error) {
	if mime == "" {
		mime = "application/octet-stream"
	}
	if !s.Cfg.AllowsMIME(mime) {
		return uuid.Nil, http.StatusUnsupportedMediaType, errors.New("mime not allowed")
	}
	if s.Cfg.UploadMaxBytes > 0 && declaredSize > s.Cfg.UploadMaxBytes {
		return uuid.Nil, http.StatusRequestEntityTooLarge, errors.New("file too large")
	}
	if err := os.MkdirAll(s.Cfg.StorageDir, 0o755); err != nil {
		return uuid.Nil, http.StatusInternalServerError, err
	}
	key := uuid.NewString()
	path := filepath.Join(s.Cfg.StorageDir, key)
	out, err := os.Create(path)
	if err != nil {
		return uuid.Nil, http.StatusInternalServerError, err
	}
	limit := s.Cfg.UploadMaxBytes
	if limit <= 0 {
		limit = 256 << 20
	}
	n, err := io.Copy(out, io.LimitReader(src, limit+1))
	out.Close()
	if err != nil {
		_ = os.Remove(path)
		return uuid.Nil, http.StatusInternalServerError, err
	}
	if n > limit {
		_ = os.Remove(path)
		return uuid.Nil, http.StatusRequestEntityTooLarge, errors.New("file too large")
	}
	var fid uuid.UUID
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO files (storage_key, mime_type, byte_size, original_name)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		key, mime, n, filename).Scan(&fid)
	if err != nil {
		_ = os.Remove(path)
		return uuid.Nil, http.StatusInternalServerError, err
	}
	return fid, http.StatusOK, nil
}
