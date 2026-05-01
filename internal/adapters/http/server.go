package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hases/hases-api/internal/app/mailer"
	"github.com/hases/hases-api/internal/app/notifier"
	"github.com/hases/hases-api/internal/app/pdf"
	"github.com/hases/hases-api/internal/auth"
	"github.com/hases/hases-api/internal/config"
	"github.com/hases/hases-api/openapi"
)

type Server struct {
	Pool     *pgxpool.Pool
	Cfg      config.Config
	Mailer   *mailer.Mailer
	Notifier *notifier.Notifier
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	corsOrigins := s.Cfg.CORSAllowedOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"http://localhost:4200", "http://127.0.0.1:4200"}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))
	r.Use(middleware.Timeout(120 * time.Second))

	r.Get("/health", s.health)
	r.Get("/api/v1/health", s.health)
	r.Get("/openapi.yaml", serveOpenAPI)
	r.Get("/api/v1/openapi.yaml", serveOpenAPI)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.login)

		r.Get("/public/vacancies/{slug}", s.publicVacancyBySlug)
		r.Post("/public/applications", s.publicCreateApplication)

		r.Get("/public/document-templates/{itemKey}", s.downloadDocumentTemplate)
		r.Post("/auth/accept-invitation", s.acceptInvitation)

		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler {
				return BearerAuth(s.Cfg.JWTSecret, next)
			})
			r.Get("/me", s.me)

			r.Route("/vacancies", func(r chi.Router) {
				r.Get("/", s.listVacancies)
				r.Post("/", s.createVacancy)
				r.Get("/{id}", s.getVacancy)
				r.Patch("/{id}", s.patchVacancy)
				r.Post("/{id}/publish", s.publishVacancy)
				r.Post("/{id}/archive", s.archiveVacancy)
			})

			r.Route("/checklist-templates", func(r chi.Router) {
				r.Post("/", s.createChecklistTemplate)
				r.Post("/{tid}/items", s.addChecklistItem)
			})

			r.Route("/applications", func(r chi.Router) {
				r.Get("/", s.listApplications)
				r.Get("/{id}", s.getApplication)
				r.Patch("/{id}", s.patchApplication)
				r.Post("/{id}/transition", s.transitionApplication)
				r.Get("/{id}/completeness", s.getCompleteness)
				r.Post("/{id}/documents/{itemID}/upload", s.uploadDocument)
				r.Patch("/{id}/documents/{docID}/review", s.reviewDocument)
			})

			r.Route("/interview-templates", func(r chi.Router) {
				r.Post("/", s.createInterviewTemplate)
				r.Post("/{tid}/questions", s.addInterviewQuestion)
			})
			r.Get("/applications/{id}/interview-sessions", s.listInterviewSessions)
			r.Post("/applications/{id}/interview-sessions", s.createInterviewSession)
			r.Patch("/interview-sessions/{sid}", s.patchInterviewSession)
			r.Put("/interview-sessions/{sid}/responses", s.putInterviewResponses)

			r.Get("/applications/{id}/occupational.pdf", s.downloadOccupationalPDF)
			r.Post("/applications/{id}/occupational/send", s.recordOccupationalSendWithEmail)
			r.Post("/applications/{id}/ips-result", s.recordIPSResult)

			r.Route("/induction/org-modules", func(r chi.Router) {
				r.Get("/", s.listInductionModulesEnriched)
				r.Post("/", s.createInductionModule)
			})
			r.Post("/applications/{id}/induction/org-progress", s.upsertInductionProgress)
			r.Post("/applications/{id}/induction/signatures", s.addInductionSignature)
			r.Post("/applications/{id}/induction/org/complete", s.completeInductionOrg)

			r.Post("/applications/{id}/functional-plan", s.ensureFunctionalPlan)
			r.Post("/applications/{id}/functional/theory-complete", s.completeTheory)
			r.Post("/applications/{id}/epp-delivery", s.recordEPPDelivery)
			r.Post("/applications/{id}/functional/practice-start", s.startPractice)
			r.Post("/applications/{id}/functional/evidence", s.addFunctionalEvidence)
			r.Post("/applications/{id}/functional/complete", s.completeFunctional)

			r.Route("/catalogs/rejection-reasons", func(r chi.Router) {
				r.Get("/", s.listRejectionReasons)
				r.Post("/", s.createRejectionReason)
				r.Delete("/{id}", s.deleteRejectionReason)
			})

			r.Get("/company-settings", s.getCompanySettings)
			r.Patch("/company-settings", s.patchCompanySettings)

			r.Get("/audit-logs", s.listAuditLogs)
			r.Get("/files/{fid}", s.downloadFile)
			r.Get("/reports/applications.csv", s.exportApplicationsCSV)

			r.Route("/users", func(r chi.Router) {
				r.Get("/", s.listUsers)
				r.Post("/", s.createUser)
				r.Patch("/{id}", s.patchUser)
				r.Delete("/{id}", s.deactivateUser)
			})

			r.Get("/document-types", s.listDocumentTypes)
			r.Post("/document-templates/{itemKey}", s.uploadDocumentTemplate)

			r.Get("/vacancies/{id}/role-manual", s.getRoleManual)
			r.Patch("/vacancies/{id}/role-manual", s.patchRoleManual)

			r.Post("/applications/{id}/invite", s.inviteApplicationToPortal)
			r.Post("/applications/{id}/hiring-decision", s.hiringDecision)

			// Worker portal endpoints under /me/*. Using chi.Group with absolute
			// paths (instead of chi.Route("/me", ...)) so the requireWorker
			// middleware does not capture the bare GET /me endpoint above,
			// which is the identity endpoint shared by all roles.
			r.Group(func(r chi.Router) {
				r.Use(s.requireWorker)
				r.Get("/me/application", s.workerGetApplication)
				r.Post("/me/application/documents/{itemID}/upload", s.workerUploadDocument)
				r.Get("/me/induction/org-modules", s.workerListInductionModules)
				r.Post("/me/induction/org-progress", s.workerUpsertProgress)
				r.Patch("/me/induction/progress/{moduleID}", s.workerProgressTick)
				r.Post("/me/induction/signatures", s.workerSignature)
				r.Get("/me/functional-plan", s.workerFunctionalPlan)
				r.Post("/me/functional/evidence", s.workerEvidence)
				r.Get("/me/functional/activities", s.workerListFunctionalActivities)
				r.Post("/me/functional/activities/{aid}/complete", s.workerCompleteFunctionalActivity)
			})

			r.Get("/applications/{id}/functional/activities", s.listFunctionalActivities)
			r.Get("/vacancies/{id}/functional-activities", s.listFunctionalActivityTemplates)
			r.Post("/vacancies/{id}/functional-activities", s.createFunctionalActivityTemplate)
			r.Patch("/functional-activity-templates/{tid}", s.patchFunctionalActivityTemplate)
			r.Delete("/functional-activity-templates/{tid}", s.deleteFunctionalActivityTemplate)
			r.Post("/applications/{id}/functional/activities/{aid}/complete", s.completeFunctionalActivity)

			r.Post("/induction/org-modules/{mid}/media", s.uploadInductionMedia)
			r.Delete("/induction/org-media/{mediaID}", s.deleteInductionMedia)

			r.Patch("/applications/{id}/ips-result/upload", s.uploadIPSResultFile)

			r.Route("/admin/outbox", func(r chi.Router) {
				r.Get("/", s.listOutbox)
				r.Post("/{id}/retry", s.retryOutbox)
			})
			r.Get("/applications/overdue", s.listOverdueApplications)
			r.Get("/reports/pipeline-time.csv", s.exportPipelineTimeCSV)
			r.Get("/reports/ips-monthly.csv", s.exportIPSMonthlyCSV)
			r.Get("/reports/onboarding-completed.csv", s.exportOnboardingCompletedCSV)

			r.Post("/applications/{id}/endowment-delivery", s.recordEndowmentDelivery)
			r.Get("/applications/{id}/endowment-deliveries", s.listEndowmentDeliveries)
		})
	})

	return r
}

func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(openapi.Spec)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	var id uuid.UUID
	var hash, role string
	err := s.Pool.QueryRow(r.Context(),
		`SELECT id, password_hash, role FROM users WHERE email=$1 AND active`,
		strings.TrimSpace(strings.ToLower(body.Email)),
	).Scan(&id, &hash, &role)
	if err != nil || !auth.CheckPassword(hash, body.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "credenciales invalidas"})
		return
	}
	token, err := auth.Sign(s.Cfg.JWTSecret, id, body.Email, role, s.Cfg.JWTExpiration)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_in_hours": s.Cfg.JWTExpiration})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	cl := ClaimsFromCtx(r)
	if cl == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "auth"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_id": cl.UserID, "email": cl.Email, "role": cl.Role})
}

func (s *Server) audit(ctx context.Context, actor *uuid.UUID, entityType string, entityID *uuid.UUID, action string, payload any) {
	var pid any
	if actor != nil {
		pid = *actor
	}
	var eid any
	if entityID != nil {
		eid = *entityID
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, entity_type, entity_id, action, payload)
		VALUES ($1,$2,$3,$4, NULL)`,
		pid, entityType, eid, action)
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "fid")
	uid, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	s.streamFileByID(w, r, uid, true)
}

// streamFileByID resuelve un archivo por su id y lo sirve usando http.ServeContent
// para que se respete `Range` (necesario para streaming de video) y caching.
// Si attachment=true, fuerza Content-Disposition: attachment para descarga directa.
func (s *Server) streamFileByID(w http.ResponseWriter, r *http.Request, fileID uuid.UUID, attachment bool) {
	var key, name, mime string
	err := s.Pool.QueryRow(r.Context(),
		`SELECT storage_key, original_name, mime_type FROM files WHERE id=$1`, fileID,
	).Scan(&key, &name, &mime)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(s.Cfg.StorageDir, key)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "missing file", http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "stat", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime)
	if attachment {
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	} else {
		w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, name, stat.ModTime(), f)
}

func (s *Server) downloadOccupationalPDF(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	appID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var fn, ln, vac string
	err = s.Pool.QueryRow(r.Context(), `
		SELECT a.first_name, a.last_name, v.title
		FROM applications a JOIN vacancies v ON v.id=a.vacancy_id WHERE a.id=$1`, appID).Scan(&fn, &ln, &vac)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	b, err := pdf.OccupationalExamPDF(fn+" "+ln, appID.String(), vac)
	if err != nil {
		http.Error(w, "pdf", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="examen-ocupacional.pdf"`)
	_, _ = w.Write(b)
}
