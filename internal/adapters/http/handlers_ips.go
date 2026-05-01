package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hases/hases-api/internal/app/pdf"
)

// uploadIPSResultFile: endpoint multipart para subir el PDF del resultado IPS
// y registrarlo. `outcome` viene como form field junto a `recommendations`.
// El endpoint legacy `recordIPSResult` (JSON sin archivo) se conserva.
func (s *Server) uploadIPSResultFile(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
		return
	}
	outcome := strings.TrimSpace(r.FormValue("outcome"))
	switch outcome {
	case "fit", "unfit", "fit_restrictions":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "outcome"})
		return
	}
	recommendations := r.FormValue("recommendations")

	var fid *uuid.UUID
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		if _, ok := r.MultipartForm.File["file"]; ok {
			id, status, err := s.persistFormFile(r, "file")
			if err != nil {
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			fid = &id
		}
	}

	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO ips_results (application_id, outcome, recommendations, attachment_file_id)
		VALUES ($1,$2,$3,$4)`,
		aid, outcome, recommendations, fid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(),
		`UPDATE applications SET status='occ_result_received', updated_at=now() WHERE id=$1`, aid)

	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "ips_result", &aid, "create:"+outcome, nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":      true,
		"outcome": outcome,
	})
}

// recordOccupationalSendWithEmail reescribe el endpoint legacy: además del
// registro, envía email a la IPS con el PDF prellenado adjunto cuando hay
// SMTP configurado. El estado de la postulación pasa a `occ_sent`.
func (s *Server) recordOccupationalSendWithEmail(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		EmailTo string `json:"email_to"`
	}
	_ = readJSON(r, &body)
	emailTo := strings.TrimSpace(body.EmailTo)
	if emailTo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email_to required"})
		return
	}

	var fn, ln, vac string
	if err := s.Pool.QueryRow(r.Context(), `
		SELECT a.first_name, a.last_name, v.title
		FROM applications a JOIN vacancies v ON v.id=a.vacancy_id WHERE a.id=$1`, aid,
	).Scan(&fn, &ln, &vac); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "application not found"})
		return
	}

	pdfBytes, err := pdf.OccupationalExamPDF(fn+" "+ln, aid.String(), vac)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "pdf"})
		return
	}

	if _, err := s.Pool.Exec(r.Context(), `
		INSERT INTO occupational_orders (application_id, sent_at, email_to)
		VALUES ($1, now(), $2)`, aid, emailTo); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(),
		`UPDATE applications SET status='occ_sent', updated_at=now() WHERE id=$1`, aid)

	if s.Mailer != nil && s.Mailer.Enabled() {
		subject := "Examen ocupacional - " + fn + " " + ln + " (" + vac + ")"
		bodyTxt := "Buen dia,\n\nAdjunto el formato del examen ocupacional para el ingreso de " +
			fn + " " + ln + " al cargo " + vac + ".\n\n" +
			"Por favor confirmar fecha y hora del examen.\n\nGracias."
		if err := s.Mailer.SendWithAttachment(emailTo, subject, bodyTxt,
			"examen-ocupacional.pdf", "application/pdf", pdfBytes); err != nil {
			// No bloquear el flujo: el estado ya se grabó. El intento queda en logs.
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":         false,
				"warning":    "smtp_failed",
				"smtp_error": err.Error(),
			})
			return
		}
	}

	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "occupational_order", &aid, "send", nil)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "email_sent": s.Mailer.Enabled()})
}
