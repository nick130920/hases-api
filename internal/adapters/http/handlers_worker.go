package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hases/hases-api/internal/auth"
)

// applicationIDForWorker resuelve la postulación a la que el JWT actual
// (rol=worker) tiene acceso. Devuelve uuid.Nil y false si no aplica.
func (s *Server) applicationIDForWorker(r *http.Request) (uuid.UUID, bool) {
	cl := ClaimsFromCtx(r)
	if cl == nil || cl.Role != "worker" {
		return uuid.Nil, false
	}
	var aid uuid.UUID
	err := s.Pool.QueryRow(r.Context(),
		`SELECT application_id FROM application_user_links WHERE user_id=$1`, cl.UserID,
	).Scan(&aid)
	if err != nil {
		return uuid.Nil, false
	}
	return aid, true
}

// requireWorker middleware: garantiza rol=worker y carga su application_id.
func (s *Server) requireWorker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aid, ok := s.applicationIDForWorker(r)
		if !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "worker only"})
			return
		}
		ctx := context.WithValue(r.Context(), workerAppKey, aid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type workerCtxKey string

const workerAppKey workerCtxKey = "worker_app_id"

func workerAppID(r *http.Request) uuid.UUID {
	v := r.Context().Value(workerAppKey)
	if id, ok := v.(uuid.UUID); ok {
		return id
	}
	return uuid.Nil
}

// inviteApplicationToPortal genera (o recrea) un token de invitación al
// portal para una postulación. Crea el usuario worker si no existe y devuelve
// el link para enviar al postulante. Roles: admin, hr, hiring_manager.
func (s *Server) inviteApplicationToPortal(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin", "hr", "hiring_manager") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
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
	s.audit(r.Context(), actor, "application", &aid, "invite_to_portal", nil)

	if s.Mailer != nil && s.Mailer.Enabled() {
		var em, fn string
		_ = s.Pool.QueryRow(r.Context(),
			`SELECT email, first_name FROM applications WHERE id=$1`, aid,
		).Scan(&em, &fn)
		if em != "" {
			body := "Hola " + fn + ",\n\n" +
				"Has sido invitado al portal de HASES para continuar tu proceso. " +
				"Define tu contrasena con el siguiente codigo de invitacion: " + token + "\n\n" +
				"Gracias."
			_ = s.Mailer.Send(em, "Invitacion al portal HASES", body)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"invitation_token": token})
}

// upsertWorkerInvitation crea (o reusa) el usuario worker ligado a la
// postulación y le asigna un token nuevo de invitación válido por 7 días.
// Devuelve el token plano para que el llamador lo entregue al postulante.
func (s *Server) upsertWorkerInvitation(ctx context.Context, aid uuid.UUID) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var fn, ln, email string
	if err := tx.QueryRow(ctx,
		`SELECT first_name, last_name, email FROM applications WHERE id=$1`, aid,
	).Scan(&fn, &ln, &email); err != nil {
		return "", errors.New("application not found")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errors.New("application without email")
	}

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Hash temporal aleatorio: el usuario lo reemplaza al aceptar la invitación.
		tempPass := randomToken(16)
		hash, herr := auth.HashPassword(tempPass)
		if herr != nil {
			return "", herr
		}
		fullName := strings.TrimSpace(fn + " " + ln)
		if fullName == "" {
			fullName = email
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, role, active)
			VALUES ($1,$2,$3,'worker', FALSE) RETURNING id`,
			email, hash, fullName).Scan(&userID)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	token := randomToken(24)
	expires := time.Now().Add(7 * 24 * time.Hour)
	_, err = tx.Exec(ctx, `
		INSERT INTO application_user_links (application_id, user_id, invited_at, invitation_token, invitation_expires_at)
		VALUES ($1,$2, now(), $3, $4)
		ON CONFLICT (application_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			invitation_token = EXCLUDED.invitation_token,
			invitation_expires_at = EXCLUDED.invitation_expires_at,
			invited_at = now()`,
		aid, userID, token, expires)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return token, nil
}

// acceptInvitation: el postulante define su contraseña usando el token recibido.
// Activa la cuenta y devuelve un JWT.
func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	body.Token = strings.TrimSpace(body.Token)
	if body.Token == "" || len(body.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token and password (min 6 chars) required"})
		return
	}
	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	var expires pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		SELECT user_id, invitation_expires_at FROM application_user_links
		WHERE invitation_token=$1`, body.Token).Scan(&userID, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if expires.Valid && time.Now().After(expires.Time) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token expired"})
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hash"})
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET password_hash=$1, active=TRUE WHERE id=$2`, hash, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(ctx, `
		UPDATE application_user_links
		SET accepted_at=now(), invitation_token=NULL, invitation_expires_at=NULL
		WHERE user_id=$1`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var email string
	_ = s.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&email)
	token, err := auth.Sign(s.Cfg.JWTSecret, userID, email, "worker", s.Cfg.JWTExpiration)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":            token,
		"expires_in_hours": s.Cfg.JWTExpiration,
	})
}

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- Endpoints /me/... ----------

func (s *Server) workerGetApplication(w http.ResponseWriter, r *http.Request) {
	s.writeApplicationDetail(w, r, workerAppID(r))
}

func (s *Server) workerUploadDocument(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	itemID, ok := mustUUID(chi.URLParam(r, "itemID"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "itemID"})
		return
	}
	s.handleDocumentUpload(w, r, aid, itemID)
}

func (s *Server) workerUpsertProgress(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	var body struct {
		ModuleID uuid.UUID `json:"module_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	cl := ClaimsFromCtx(r)
	var vb pgtype.UUID
	if cl != nil {
		vb.Bytes = [16]byte(cl.UserID)
		vb.Valid = true
	}
	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO induction_org_progress (application_id, module_id, completed_at, validated_by)
		VALUES ($1,$2, now(), $3)
		ON CONFLICT (application_id, module_id) DO UPDATE SET completed_at=EXCLUDED.completed_at, validated_by=EXCLUDED.validated_by`,
		aid, body.ModuleID, vb)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) workerSignature(w http.ResponseWriter, r *http.Request) {
	s.handleInductionSignature(w, r, workerAppID(r))
}

func (s *Server) workerEvidence(w http.ResponseWriter, r *http.Request) {
	s.handleFunctionalEvidence(w, r, workerAppID(r))
}

func (s *Server) workerFunctionalPlan(w http.ResponseWriter, r *http.Request) {
	aid := workerAppID(r)
	row := s.Pool.QueryRow(r.Context(), `
		SELECT v.role_manual_body, v.role_manual_file_id,
		       fp.manual_summary, fp.theory_completed_at::text, fp.practice_started_at::text,
		       fp.practice_completed_at::text, fp.onboarding_completed_at::text
		FROM applications a
		JOIN vacancies v ON v.id = a.vacancy_id
		LEFT JOIN functional_plans fp ON fp.application_id = a.id
		WHERE a.id=$1`, aid)
	var manualBody string
	var manualFid pgtype.UUID
	var manualSummary, theory, practiceS, practiceC, onboarding pgtype.Text
	if err := row.Scan(&manualBody, &manualFid, &manualSummary, &theory, &practiceS, &practiceC, &onboarding); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
		return
	}
	out := map[string]any{
		"role_manual_body": manualBody,
		"manual_summary":   manualSummary.String,
		"theory_completed_at":      theory.String,
		"practice_started_at":      practiceS.String,
		"practice_completed_at":    practiceC.String,
		"onboarding_completed_at":  onboarding.String,
	}
	if manualFid.Valid {
		out["role_manual_file_id"] = uuid.UUID(manualFid.Bytes).String()
	}
	writeJSON(w, http.StatusOK, out)
}
