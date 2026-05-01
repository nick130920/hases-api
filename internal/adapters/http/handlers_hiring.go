package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hases/hases-api/internal/app/mailer"
	"github.com/hases/hases-api/internal/domain"
)

// HiringDecisionBody representa la decisión del empleador.
// `decision` debe ser "hire" o "reject"; `reason_id` es opcional al rechazar.
type HiringDecisionBody struct {
	Decision string `json:"decision"`
	ReasonID *int   `json:"reason_id"`
	Notes    string `json:"notes"`
}

// hiringDecision: el hiring_manager (o admin) registra la decisión final.
// Si "hire": pasa a `induction_org` y dispara la invitación al portal.
// Si "reject": pasa a `rejected` con motivo.
func (s *Server) hiringDecision(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hiring_manager") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body HiringDecisionBody
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	switch body.Decision {
	case "hire", "reject":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "decision must be hire|reject"})
		return
	}

	var current string
	if err := s.Pool.QueryRow(r.Context(),
		`SELECT status FROM applications WHERE id=$1`, aid,
	).Scan(&current); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "application not found"})
		return
	}

	if body.Decision == "reject" {
		var reason string
		if body.ReasonID != nil {
			_ = s.Pool.QueryRow(r.Context(),
				`SELECT label FROM rejection_reasons WHERE id=$1`, *body.ReasonID,
			).Scan(&reason)
		}
		if reason == "" && strings.TrimSpace(body.Notes) != "" {
			reason = strings.TrimSpace(body.Notes)
		}
		if _, err := s.Pool.Exec(r.Context(), `
			UPDATE applications SET status=$2, discarded_reason=$3, updated_at=now()
			WHERE id=$1`,
			aid, domain.StatusRejected, nullIfEmpty(reason)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		cl := ClaimsFromCtx(r)
		var actor *uuid.UUID
		if cl != nil {
			actor = &cl.UserID
		}
		s.audit(r.Context(), actor, "application", &aid, "hiring_decision:reject", nil)
		s.notifyApplicantStatus(r.Context(), aid, domain.StatusRejected)
		writeJSON(w, http.StatusOK, map[string]string{"status": domain.StatusRejected})
		return
	}

	// hire: pasa a induction_org y emite invitación al portal.
	if _, err := s.Pool.Exec(r.Context(), `
		UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`,
		aid, domain.StatusInductionOrg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	token, err := s.upsertWorkerInvitation(r.Context(), aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "application", &aid, "hiring_decision:hire", nil)
	s.notifyApplicantStatus(r.Context(), aid, domain.StatusInductionOrg)

	if s.Mailer != nil && s.Mailer.Enabled() {
		var em, fn, ln string
		_ = s.Pool.QueryRow(r.Context(),
			`SELECT email, first_name, last_name FROM applications WHERE id=$1`, aid,
		).Scan(&em, &fn, &ln)
		if em != "" {
			tpl := mailer.RenderHiringDecision(mailer.HiringDecisionData{
				FullName: strings.TrimSpace(fn + " " + ln),
				Hired:    true,
				Link:     s.invitationLink(token),
			})
			_ = s.Mailer.SendHTML(em, "HASES · Aprobación y acceso al portal", tpl.HTML, tpl.Text)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           domain.StatusInductionOrg,
		"invitation_token": token,
	})
}

// notifyApplicantStatus envía un email contextual cuando cambia el estado de
// la postulación. Hoy se usa principalmente para el caso de rechazo; el resto
// de transiciones puede mantenerse interna sin notificar al candidato.
func (s *Server) notifyApplicantStatus(ctx context.Context, aid uuid.UUID, status string) {
	if s.Mailer == nil || !s.Mailer.Enabled() {
		return
	}
	var em, fn, ln, reason string
	if err := s.Pool.QueryRow(ctx,
		`SELECT email, first_name, last_name, COALESCE(discarded_reason, '')
		 FROM applications WHERE id=$1`, aid,
	).Scan(&em, &fn, &ln, &reason); err != nil || em == "" {
		return
	}
	if status != domain.StatusRejected {
		// Las demás transiciones se manejan por correos específicos
		// (invitación al portal, hiring decision, IPS, etc.) o no requieren
		// notificar al candidato.
		return
	}
	tpl := mailer.RenderHiringDecision(mailer.HiringDecisionData{
		FullName: strings.TrimSpace(fn + " " + ln),
		Hired:    false,
		Reason:   reason,
	})
	_ = s.Mailer.SendHTML(em, "HASES · Resultado del proceso", tpl.HTML, tpl.Text)
}
