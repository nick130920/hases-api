package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hases/hases-api/internal/app/mailer"
	"github.com/hases/hases-api/internal/auth"
	"github.com/hases/hases-api/internal/domain"
)

func mustUUID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	return id, err == nil
}

func (s *Server) listVacancies(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, title, description, requirements, status, public_slug, published_at::text,
		       checklist_template_id, role_manual_body, role_manual_file_id, created_at::text
		FROM vacancies ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, ct uuid.UUID
		var title, desc, req, st, slug, pub, manual, created string
		var manualFid pgtype.UUID
		_ = rows.Scan(&id, &title, &desc, &req, &st, &slug, &pub, &ct, &manual, &manualFid, &created)
		row := map[string]any{
			"id": id.String(), "title": title, "description": desc, "requirements": req,
			"status": st, "public_slug": slug, "published_at": pub,
			"checklist_template_id": ct.String(),
			"role_manual_body":      manual,
			"created_at":            created,
		}
		if manualFid.Valid {
			row["role_manual_file_id"] = uuid.UUID(manualFid.Bytes).String()
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createVacancy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		Requirements string `json:"requirements"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}
	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	var tid uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO checklist_templates (name) VALUES ($1) RETURNING id`,
		"Plantilla "+body.Title).Scan(&tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err = seedDefaultChecklist(ctx, tx, tid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(body.Title), " ", "-")) + "-" + uuid.NewString()[:8]
	var vid uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO vacancies (title, description, requirements, status, public_slug, checklist_template_id)
		VALUES ($1,$2,$3,'draft',$4,$5) RETURNING id`,
		body.Title, body.Description, body.Requirements, slug, tid).Scan(&vid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var uid *uuid.UUID
	if cl != nil {
		uid = &cl.UserID
	}
	s.audit(ctx, uid, "vacancy", &vid, "create", nil)
	_ = tx.Commit(ctx)
	writeJSON(w, http.StatusCreated, map[string]any{"id": vid.String(), "public_slug": slug, "checklist_template_id": tid.String()})
}

func seedDefaultChecklist(ctx context.Context, tx pgx.Tx, templateID uuid.UUID) error {
	items := []struct {
		Key    string
		Label  string
		Req    bool
		Veh    bool
		Ord    int
	}{
		{"hv", "Hoja de vida con foto (formato)", true, false, 1},
		{"cedula", "Fotocopia cedula 150%", true, false, 2},
		{"proc", "Antecedentes Procuraduria", true, false, 3},
		{"cura", "Antecedentes Curaduria", true, false, 4},
		{"pol", "Antecedentes Policia Nacional", true, false, 5},
		{"lic_cond", "Licencia de conduccion", false, true, 6},
		{"lic_trans", "Licencia de transito", false, true, 7},
		{"banco", "Certificado bancario (max 30 dias)", true, false, 8},
		{"eps", "Certificado afiliacion EPS", true, false, 9},
		{"pension", "Certificado fondo pensional", true, false, 10},
		{"estudio", "Certificado de estudios (opcional)", false, false, 11},
		{"laboral", "Certificados laborales", true, false, 12},
	}
	for _, it := range items {
		_, err := tx.Exec(ctx, `
			INSERT INTO checklist_items (template_id, sort_order, item_key, label, required, requires_vehicle)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			templateID, it.Ord, it.Key, it.Label, it.Req, it.Veh)
		if err != nil {
			return err
		}
	}
	return nil
}

// pgx.Tx interface - use pgxpool pool.Begin returns pgx.Tx - actually BeginTx returns pgx.Tx from jackc/pgx/v5

func (s *Server) getVacancy(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	row := s.Pool.QueryRow(r.Context(), `
		SELECT id, title, description, requirements, status, public_slug, published_at::text,
		       checklist_template_id, role_manual_body, role_manual_file_id
		FROM vacancies WHERE id=$1`, id)
	var vid uuid.UUID
	var title, desc, req, st, slug, manual string
	var pub pgtype.Text
	var ct uuid.UUID
	var manualFid pgtype.UUID
	if err := row.Scan(&vid, &title, &desc, &req, &st, &slug, &pub, &ct, &manual, &manualFid); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	out := map[string]any{
		"id": vid.String(), "title": title, "description": desc, "requirements": req,
		"status": st, "public_slug": slug, "published_at": pub.String, "checklist_template_id": ct.String(),
		"role_manual_body": manual,
	}
	if manualFid.Valid {
		out["role_manual_file_id"] = uuid.UUID(manualFid.Bytes).String()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) patchVacancy(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	title, _ := body["title"].(string)
	desc, _ := body["description"].(string)
	req, _ := body["requirements"].(string)
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE vacancies SET title=COALESCE(NULLIF($1,''), title),
			description=COALESCE(NULLIF($2,''), description),
			requirements=COALESCE(NULLIF($3,''), requirements),
			updated_at=now()
		WHERE id=$4`, title, desc, req, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) publishVacancy(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE vacancies SET status='published', published_at=now(), updated_at=now() WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (s *Server) createChecklistTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name"})
		return
	}
	var tid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO checklist_templates (name) VALUES ($1) RETURNING id`, body.Name).Scan(&tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": tid.String()})
}

func (s *Server) addChecklistItem(w http.ResponseWriter, r *http.Request) {
	tid, ok := mustUUID(chi.URLParam(r, "tid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tid"})
		return
	}
	var body struct {
		ItemKey         string `json:"item_key"`
		Label           string `json:"label"`
		Required        bool   `json:"required"`
		RequiresVehicle bool   `json:"requires_vehicle"`
		SortOrder       int    `json:"sort_order"`
	}
	if err := readJSON(r, &body); err != nil || body.ItemKey == "" || body.Label == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fields"})
		return
	}
	var iid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO checklist_items (template_id, sort_order, item_key, label, required, requires_vehicle)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		tid, body.SortOrder, body.ItemKey, body.Label, body.Required, body.RequiresVehicle).Scan(&iid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": iid.String()})
}

func (s *Server) publicVacancyBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	row := s.Pool.QueryRow(r.Context(), `
		SELECT id, title, description, requirements, status, public_slug FROM vacancies WHERE public_slug=$1 AND status='published'`, slug)
	var id uuid.UUID
	var title, desc, req, st, sl string
	if err := row.Scan(&id, &title, &desc, &req, &st, &sl); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id.String(), "title": title, "description": desc, "requirements": req, "public_slug": sl,
	})
}

func (s *Server) publicCreateApplication(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VacancyID       string `json:"vacancy_id"`
		FirstName       string `json:"first_name"`
		LastName        string `json:"last_name"`
		Email           string `json:"email"`
		Phone           string `json:"phone"`
		Channel         string `json:"channel"`
		CVReference     string `json:"cv_reference"`
		RequiresVehicle bool   `json:"requires_vehicle"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	vid, ok := mustUUID(body.VacancyID)
	if !ok || strings.TrimSpace(body.FirstName) == "" || strings.TrimSpace(body.Email) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "vacancy_id first_name email"})
		return
	}
	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	var checklist uuid.UUID
	err = tx.QueryRow(ctx, `SELECT checklist_template_id FROM vacancies WHERE id=$1 AND status='published'`, vid).Scan(&checklist)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "vacancy not publishable"})
		return
	}

	var aid uuid.UUID
	st := domain.InitialApplicationStatus()
	err = tx.QueryRow(ctx, `
		INSERT INTO applications (vacancy_id, status, first_name, last_name, email, phone, channel, cv_reference, requires_vehicle)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		vid, st, body.FirstName, body.LastName, strings.ToLower(strings.TrimSpace(body.Email)),
		body.Phone, body.Channel, body.CVReference, body.RequiresVehicle).Scan(&aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rows, err := tx.Query(ctx, `
		SELECT id FROM checklist_items WHERE template_id=$1 AND (requires_vehicle = false OR $2 = true) ORDER BY sort_order`,
		checklist, body.RequiresVehicle)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ci uuid.UUID
		if err := rows.Scan(&ci); err != nil {
			continue
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO application_documents (application_id, checklist_item_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			aid, ci)
	}
	_ = rows.Err()
	_ = tx.Commit(ctx)
	writeJSON(w, http.StatusCreated, map[string]string{"application_id": aid.String(), "status": st})
}

func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	vac := r.URL.Query().Get("vacancy_id")
	status := r.URL.Query().Get("status")
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	q := `SELECT id, vacancy_id, status, first_name, last_name, email, phone, channel, created_at::text FROM applications WHERE 1=1`
	args := []any{}
	n := 1
	if vac != "" {
		if id, err := uuid.Parse(vac); err == nil {
			q += ` AND vacancy_id=$` + itoa(n)
			args = append(args, id)
			n++
		}
	}
	if status != "" {
		q += ` AND status=$` + itoa(n)
		args = append(args, status)
		n++
	}
	if search != "" {
		q += ` AND (first_name ILIKE $` + itoa(n) + ` OR last_name ILIKE $` + itoa(n) + ` OR email ILIKE $` + itoa(n) + `)`
		args = append(args, "%"+search+"%")
		n++
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.Pool.Query(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, vid uuid.UUID
		var st, fn, ln, em, ph, ch, ca string
		_ = rows.Scan(&id, &vid, &st, &fn, &ln, &em, &ph, &ch, &ca)
		out = append(out, map[string]any{
			"id": id.String(), "vacancy_id": vid.String(), "status": st,
			"first_name": fn, "last_name": ln, "email": em, "phone": ph, "channel": ch, "created_at": ca,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func itoa(i int) string {
	b := []byte{}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if len(b) == 0 {
		return "1"
	}
	return string(b)
}

func (s *Server) getApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	s.writeApplicationDetail(w, r, id)
}

// writeApplicationDetail produce el JSON del expediente completo de una postulación.
// Reutilizado por staff y por el portal del trabajador (/me/application).
func (s *Server) writeApplicationDetail(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	row := s.Pool.QueryRow(r.Context(), `
		SELECT id, vacancy_id, status, first_name, last_name, email, phone, channel, cv_reference, requires_vehicle, discarded_reason, notes, created_at::text
		FROM applications WHERE id=$1`, id)
	var aid, vid uuid.UUID
	var st, fn, ln, em, ph, ch, cv, notes string
	var rv bool
	var dr pgtype.Text
	var ca string
	if err := row.Scan(&aid, &vid, &st, &fn, &ln, &em, &ph, &ch, &cv, &rv, &dr, &notes, &ca); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	docs, _ := s.Pool.Query(r.Context(), `
		SELECT d.id, d.checklist_item_id, d.file_id, d.review_status, d.reviewer_notes,
		       d.issued_at::text,
		       i.item_key, i.label, i.required,
		       dt.max_age_days, COALESCE(dt.requires_template, FALSE),
		       COALESCE(dt.requires_issued_at, FALSE)
		FROM application_documents d
		JOIN checklist_items i ON i.id=d.checklist_item_id
		LEFT JOIN document_types dt ON dt.item_key = i.item_key
		WHERE d.application_id=$1
		ORDER BY i.sort_order`, id)
	defer docs.Close()
	var doclist []map[string]any
	for docs.Next() {
		var did, ciid uuid.UUID
		var fid pgtype.UUID
		var rs, rnotes, ik, lb string
		var required, requiresTemplate, requiresIssuedAt bool
		var issuedAt pgtype.Text
		var maxAge pgtype.Int4
		_ = docs.Scan(&did, &ciid, &fid, &rs, &rnotes, &issuedAt, &ik, &lb, &required, &maxAge, &requiresTemplate, &requiresIssuedAt)
		m := map[string]any{
			"id": did.String(), "checklist_item_id": ciid.String(), "review_status": rs,
			"reviewer_notes": rnotes, "item_key": ik, "label": lb, "required": required,
			"requires_template":  requiresTemplate,
			"requires_issued_at": requiresIssuedAt,
		}
		if fid.Valid {
			m["file_id"] = uuid.UUID(fid.Bytes).String()
		}
		if issuedAt.Valid {
			m["issued_at"] = issuedAt.String
		}
		if maxAge.Valid {
			m["max_age_days"] = int(maxAge.Int32)
		}
		doclist = append(doclist, m)
	}
	completeness, _ := s.computeCompleteness(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": aid.String(), "vacancy_id": vid.String(), "status": st,
		"first_name": fn, "last_name": ln, "email": em, "phone": ph, "channel": ch,
		"cv_reference": cv, "requires_vehicle": rv, "discarded_reason": dr.String,
		"notes": notes, "created_at": ca,
		"documents":    doclist,
		"completeness": completeness,
	})
}

func (s *Server) patchApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		FirstName       *string `json:"first_name"`
		LastName        *string `json:"last_name"`
		Email           *string `json:"email"`
		Phone           *string `json:"phone"`
		Channel         *string `json:"channel"`
		CVReference     *string `json:"cv_reference"`
		RequiresVehicle *bool   `json:"requires_vehicle"`
		Notes           *string `json:"notes"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	var emailLower *string
	if body.Email != nil {
		v := strings.ToLower(strings.TrimSpace(*body.Email))
		emailLower = &v
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE applications SET
			first_name = COALESCE($1, first_name),
			last_name = COALESCE($2, last_name),
			email = COALESCE($3, email),
			phone = COALESCE($4, phone),
			channel = COALESCE($5, channel),
			cv_reference = COALESCE($6, cv_reference),
			requires_vehicle = COALESCE($7, requires_vehicle),
			notes = COALESCE($8, notes),
			updated_at = now()
		WHERE id=$9`,
		body.FirstName, body.LastName, emailLower, body.Phone, body.Channel,
		body.CVReference, body.RequiresVehicle, body.Notes, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) transitionApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil || body.Status == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE applications SET status=$2, discarded_reason=$3, updated_at=now() WHERE id=$1`,
		id, body.Status, nullIfEmpty(body.Reason))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var uid *uuid.UUID
	if cl != nil {
		uid = &cl.UserID
	}
	s.audit(r.Context(), uid, "application", &id, "transition:"+body.Status, nil)

	if s.Mailer != nil && s.Mailer.Enabled() {
		var em, fn, ln string
		if err := s.Pool.QueryRow(r.Context(),
			`SELECT email, first_name, last_name FROM applications WHERE id=$1`, id,
		).Scan(&em, &fn, &ln); err == nil && em != "" {
			tpl := mailer.RenderStatusUpdate(mailer.StatusUpdateData{
				FullName:    strings.TrimSpace(fn + " " + ln),
				StatusLabel: domain.StatusLabel(body.Status),
				Status:      body.Status,
				Message:     statusUpdateCopy(body.Status),
			})
			_ = s.Mailer.SendHTML(em, "Actualización de tu postulación HASES", tpl.HTML, tpl.Text)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// statusUpdateCopy retorna un copy contextual para el correo de cambio de
// estado segun la etapa del pipeline. Si el estado no esta mapeado se devuelve
// "" y la plantilla cae al texto generico.
func statusUpdateCopy(status string) string {
	switch status {
	case domain.StatusDocsPending:
		return "Pasamos a la etapa de carga documental. Por favor revisa el listado de documentos requeridos y súbelos cuando los tengas listos."
	case domain.StatusDocsIncomplete:
		return "Detectamos que algunos documentos están incompletos o pendientes de corrección. Por favor revísalos y vuelve a cargarlos para que podamos continuar tu proceso."
	case domain.StatusDocsReview:
		return "Recibimos tus documentos y los estamos revisando. En breve te notificaremos el resultado de la verificación documental."
	case domain.StatusDocsApproved:
		return "Tus documentos fueron aprobados. Continuaremos con la etapa de entrevista, te contactaremos pronto para coordinarla."
	case domain.StatusInterviewPending:
		return "Tu proceso avanza a la etapa de entrevista. El equipo de selección se pondrá en contacto contigo para agendar el encuentro."
	case domain.StatusInterviewDone:
		return "La entrevista quedó registrada. Estamos consolidando la evaluación antes de dar el siguiente paso."
	case domain.StatusOccPending:
		return "Programaremos tu examen ocupacional con la IPS. Te llegará la información de la cita una vez se confirme con el proveedor."
	case domain.StatusOccSent:
		return "Enviamos tu formato de examen ocupacional a la IPS. Cuando recibamos los resultados te avisaremos para continuar."
	case domain.StatusOccResult:
		return "Recibimos tus resultados ocupacionales. Pasarán a revisión interna para tomar la decisión de contratación."
	case domain.StatusHiringPending:
		return "Tu expediente quedó listo para decisión final. El equipo de gestión humana lo está revisando para emitir respuesta en los próximos días."
	case domain.StatusInductionOrg:
		return "Inicia tu inducción organizacional. Desde el portal del trabajador podrás acceder a los módulos y firmar las constancias."
	case domain.StatusInductionTheory:
		return "Pasamos a la fase teórica de la inducción. Revisa los módulos audiovisuales asignados desde el portal del trabajador."
	case domain.StatusInductionEppPending:
		return "Programaremos la entrega de tus elementos de protección personal y la dotación correspondiente al cargo."
	case domain.StatusInductionPractice:
		return "Inicia la fase práctica de tu inducción. Recuerda dejar evidencia de las actividades en el portal del trabajador."
	case domain.StatusOnboardingComplete:
		return "Felicitaciones, completaste tu proceso de onboarding. Bienvenido formalmente al equipo HASES."
	}
	return ""
}

func (s *Server) uploadDocument(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	itemID, ok := mustUUID(chi.URLParam(r, "itemID"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "itemID"})
		return
	}
	s.handleDocumentUpload(w, r, aid, itemID)
}

// handleDocumentUpload contiene la lógica común usada tanto por el endpoint
// de RR.HH. como por el del portal del trabajador (/me/...). Aplica reglas
// de antigüedad (max_age_days) y permite capturar issued_at desde el form.
func (s *Server) handleDocumentUpload(w http.ResponseWriter, r *http.Request, aid, itemID uuid.UUID) {
	if err := r.ParseMultipartForm(s.Cfg.UploadMaxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart"})
		return
	}

	// Resolver metadatos del tipo de documento por item_key del checklist_item.
	var itemKey string
	var maxAge pgtype.Int4
	var requiresIssuedAt bool
	err := s.Pool.QueryRow(r.Context(), `
		SELECT ci.item_key, dt.max_age_days, COALESCE(dt.requires_issued_at, FALSE)
		FROM checklist_items ci
		LEFT JOIN document_types dt ON dt.item_key = ci.item_key
		WHERE ci.id=$1`, itemID).Scan(&itemKey, &maxAge, &requiresIssuedAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "checklist item not found"})
		return
	}

	var issuedAt *time.Time
	if raw := strings.TrimSpace(r.FormValue("issued_at")); raw != "" {
		t, perr := parseFlexibleDate(raw)
		if perr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "issued_at format (use YYYY-MM-DD)"})
			return
		}
		issuedAt = &t
	}
	if maxAge.Valid && requiresIssuedAt && issuedAt == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "issued_at_required"})
		return
	}
	if maxAge.Valid && issuedAt != nil {
		if ok, msg := validateIssuedAt(*issuedAt, int(maxAge.Int32)); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
	}

	fid, status, err := s.persistFormFile(r, "file")
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var iss any
	if issuedAt != nil {
		iss = *issuedAt
	}
	_, err = s.Pool.Exec(r.Context(), `
		UPDATE application_documents
		SET file_id=$3, issued_at=$4,
		    review_status='pending', reviewer_notes='', reviewed_by=NULL, reviewed_at=NULL
		WHERE application_id=$1 AND checklist_item_id=$2`,
		aid, itemID, fid, iss)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cl := ClaimsFromCtx(r)
	var actor *uuid.UUID
	if cl != nil {
		actor = &cl.UserID
	}
	s.audit(r.Context(), actor, "application_document", &itemID, "upload", nil)
	writeJSON(w, http.StatusOK, map[string]string{"file_id": fid.String()})
}

// parseFlexibleDate acepta YYYY-MM-DD o RFC3339.
func parseFlexibleDate(raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func (s *Server) createInterviewTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VacancyID *string `json:"vacancy_id"`
		Title     string  `json:"title"`
	}
	if err := readJSON(r, &body); err != nil || body.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title"})
		return
	}
	var vid pgtype.UUID
	if body.VacancyID != nil {
		if u, err := uuid.Parse(*body.VacancyID); err == nil {
			vid.Bytes = [16]byte(u)
			vid.Valid = true
		}
	}
	var tid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO interview_templates (vacancy_id, title) VALUES ($1,$2) RETURNING id`,
		vid, body.Title).Scan(&tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": tid.String()})
}

func (s *Server) addInterviewQuestion(w http.ResponseWriter, r *http.Request) {
	tid, ok := mustUUID(chi.URLParam(r, "tid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tid"})
		return
	}
	var body struct {
		Section      string `json:"section_name"`
		SortOrder    int    `json:"sort_order"`
		QuestionText string `json:"question_text"`
		AnswerType   string `json:"answer_type"`
	}
	if err := readJSON(r, &body); err != nil || body.QuestionText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "question"})
		return
	}
	at := body.AnswerType
	if at == "" {
		at = "text"
	}
	var qid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO interview_questions (template_id, section_name, sort_order, question_text, answer_type)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		tid, body.Section, body.SortOrder, body.QuestionText, at).Scan(&qid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": qid.String()})
}

func (s *Server) createInterviewSession(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		TemplateID string `json:"template_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	tid, ok := mustUUID(body.TemplateID)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id"})
		return
	}
	var sid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO interview_sessions (application_id, template_id) VALUES ($1,$2) RETURNING id`,
		aid, tid).Scan(&sid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"session_id": sid.String()})
}

func (s *Server) putInterviewResponses(w http.ResponseWriter, r *http.Request) {
	sid, ok := mustUUID(chi.URLParam(r, "sid"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sid"})
		return
	}
	var body struct {
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	for qidStr, ans := range body.Responses {
		qid, err := uuid.Parse(qidStr)
		if err != nil {
			continue
		}
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO interview_responses (session_id, question_id, answer_json) VALUES ($1,$2,$3)
			ON CONFLICT (session_id, question_id) DO UPDATE SET answer_json=EXCLUDED.answer_json`,
			sid, qid, ans)
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) recordOccupationalSend(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		EmailTo string `json:"email_to"`
	}
	_ = readJSON(r, &body)
	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO occupational_orders (application_id, sent_at, email_to) VALUES ($1, now(), $2)`,
		aid, body.EmailTo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`, aid, domain.StatusOccSent)
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (s *Server) recordIPSResult(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		Outcome         string `json:"outcome"`
		Recommendations string `json:"recommendations"`
	}
	if err := readJSON(r, &body); err != nil || body.Outcome == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "outcome"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO ips_results (application_id, outcome, recommendations) VALUES ($1,$2,$3)`,
		aid, body.Outcome, body.Recommendations)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`, aid, domain.StatusOccResult)
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (s *Server) listInductionModules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT id, title, body, sort_order FROM induction_org_modules ORDER BY sort_order`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var title, body string
		var so int
		_ = rows.Scan(&id, &title, &body, &so)
		out = append(out, map[string]any{"id": id.String(), "title": title, "body": body, "sort_order": so})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createInductionModule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		SortOrder int   `json:"sort_order"`
	}
	if err := readJSON(r, &body); err != nil || body.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title"})
		return
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO induction_org_modules (title, body, sort_order) VALUES ($1,$2,$3) RETURNING id`,
		body.Title, body.Body, body.SortOrder).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (s *Server) upsertInductionProgress(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
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

func (s *Server) addInductionSignature(w http.ResponseWriter, r *http.Request) {
	s.addInductionSignatureMultipart(w, r)
}

func (s *Server) completeInductionOrg(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`, aid, domain.StatusInductionTheory)
	writeJSON(w, http.StatusOK, map[string]string{"status": domain.StatusInductionTheory})
}

func (s *Server) ensureFunctionalPlan(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var body struct {
		ManualSummary string `json:"manual_summary"`
	}
	_ = readJSON(r, &body)
	manual := strings.TrimSpace(body.ManualSummary)
	if manual == "" {
		// Snapshot desde la vacante: si la vacante tiene role_manual_body se usa
		// como punto de partida del plan funcional.
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT v.role_manual_body
			FROM applications a JOIN vacancies v ON v.id = a.vacancy_id
			WHERE a.id=$1`, aid).Scan(&manual)
	}
	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO functional_plans (application_id, manual_summary) VALUES ($1,$2)
		ON CONFLICT (application_id) DO UPDATE SET manual_summary=EXCLUDED.manual_summary`,
		aid, manual)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "manual_summary": manual})
}

func (s *Server) completeTheory(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	// Si hay cronograma teórico definido para la vacante, exigir 100%.
	if ok, err := s.allActivitiesCompleted(r.Context(), aid, "theory"); err == nil && !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "theory_activities_pending"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE functional_plans SET theory_completed_at=now() WHERE application_id=$1`, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`, aid, domain.StatusInductionEppPending)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) recordEPPDelivery(w http.ResponseWriter, r *http.Request) {
	s.recordEPPDeliveryUnified(w, r)
}

func (s *Server) startPractice(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	var sigID pgtype.UUID
	err := s.Pool.QueryRow(r.Context(),
		`SELECT signature_file_id FROM epp_deliveries WHERE application_id=$1`, aid,
	).Scan(&sigID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "epp_delivery_required"})
		return
	}
	if !sigID.Valid {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "epp_signature_required"})
		return
	}
	_, err = s.Pool.Exec(r.Context(), `UPDATE functional_plans SET practice_started_at=now() WHERE application_id=$1`, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`, aid, domain.StatusInductionPractice)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) addFunctionalEvidence(w http.ResponseWriter, r *http.Request) {
	s.addFunctionalEvidenceUnified(w, r)
}

func (s *Server) completeFunctional(w http.ResponseWriter, r *http.Request) {
	aid, ok := mustUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	if ok, err := s.allActivitiesCompleted(r.Context(), aid, "practice"); err == nil && !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "practice_activities_pending"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE functional_plans SET practice_completed_at=now(), onboarding_completed_at=now() WHERE application_id=$1`, aid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE applications SET status=$2, updated_at=now() WHERE id=$1`, aid, domain.StatusOnboardingComplete)
	writeJSON(w, http.StatusOK, map[string]string{"status": domain.StatusOnboardingComplete})
}

func (s *Server) getCompanySettings(w http.ResponseWriter, r *http.Request) {
	row := s.Pool.QueryRow(r.Context(), `SELECT legal_name, tax_id, default_sender_email, updated_at::text FROM company_settings WHERE id=1`)
	var ln, tid, em, ua string
	if err := row.Scan(&ln, &tid, &em, &ua); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"legal_name": ln, "tax_id": tid, "default_sender_email": em, "updated_at": ua})
}

func (s *Server) patchCompanySettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE company_settings SET
			legal_name=COALESCE($1, legal_name),
			tax_id=COALESCE($2, tax_id),
			default_sender_email=COALESCE($3, default_sender_email),
			updated_at=now()
		WHERE id=1`,
		body["legal_name"], body["tax_id"], body["default_sender_email"])
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, actor_user_id, entity_type, entity_id, action, created_at::text FROM audit_logs ORDER BY id DESC LIMIT 200`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var actor pgtype.UUID
		var et, act, ca string
		var eid pgtype.UUID
		_ = rows.Scan(&id, &actor, &et, &eid, &act, &ca)
		m := map[string]any{"id": id, "entity_type": et, "action": act, "created_at": ca}
		if actor.Valid {
			m["actor_user_id"] = uuid.UUID(actor.Bytes).String()
		}
		if eid.Valid {
			m["entity_id"] = uuid.UUID(eid.Bytes).String()
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT id, email, full_name, role, active, created_at::text FROM users ORDER BY created_at`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var em, fn, role, ca string
		var active bool
		_ = rows.Scan(&id, &em, &fn, &role, &active, &ca)
		out = append(out, map[string]any{"id": id.String(), "email": em, "full_name": fn, "role": role, "active": active, "created_at": ca})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if !RequireRoles(r, "admin") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &body); err != nil || body.Email == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fields"})
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hash"})
		return
	}
	emailLower := strings.ToLower(strings.TrimSpace(body.Email))
	var id uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO users (email, password_hash, full_name, role) VALUES ($1,$2,$3,$4) RETURNING id`,
		emailLower, hash, body.FullName, body.Role).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.sendStaffWelcome(r.Context(), staffWelcomeArgs{
		To:        emailLower,
		FullName:  strings.TrimSpace(body.FullName),
		Password:  body.Password,
		Role:      body.Role,
		CreatedBy: requesterDisplayName(r),
	})

	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// staffWelcomeArgs agrupa los datos minimos para enviar el correo de bienvenida
// a un usuario del backoffice recien creado.
type staffWelcomeArgs struct {
	To        string
	FullName  string
	Password  string
	Role      string
	CreatedBy string
}

func (s *Server) sendStaffWelcome(ctx context.Context, args staffWelcomeArgs) {
	if s.Mailer == nil || !s.Mailer.Enabled() {
		return
	}
	to := strings.TrimSpace(args.To)
	if to == "" {
		return
	}
	loginURL := strings.TrimRight(s.Cfg.PortalBaseURL, "/") + "/login"
	tpl := mailer.RenderStaffWelcome(mailer.StaffWelcomeData{
		FullName:  args.FullName,
		Email:     to,
		Password:  args.Password,
		Role:      args.Role,
		RoleLabel: domain.RoleLabel(args.Role),
		LoginURL:  loginURL,
		CreatedBy: args.CreatedBy,
	})
	// Enviamos en goroutine para no bloquear la respuesta del POST. Si falla
	// el envio dejamos rastro en logs (Mailer ya formatea sus errores).
	go func() {
		if err := s.Mailer.SendHTML(to, "Bienvenido al sistema HASES", tpl.HTML, tpl.Text); err != nil {
			// Aprovechamos el contexto solo para evitar advertencias del compilador;
			// el envio en si no se cancela porque corre fuera del request.
			_ = ctx
		}
	}()
}

// requesterDisplayName intenta deducir el nombre o email del usuario actual
// para el correo de bienvenida. Cae a "" si no se puede resolver.
func requesterDisplayName(r *http.Request) string {
	cl := ClaimsFromCtx(r)
	if cl == nil {
		return ""
	}
	if e := strings.TrimSpace(cl.Email); e != "" {
		return e
	}
	return ""
}
