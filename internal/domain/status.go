package domain

// Pipeline states for applications (single source of truth strings).
const (
	StatusApplied                = "applied"
	StatusDocsPending            = "docs_pending"
	StatusDocsIncomplete         = "docs_incomplete"
	StatusDocsReview             = "docs_review"
	StatusDocsApproved           = "docs_approved"
	StatusInterviewPending       = "interview_pending"
	StatusInterviewDone          = "interview_done"
	StatusOccPending             = "occ_pending"
	StatusOccSent                = "occ_sent"
	StatusOccResult              = "occ_result_received"
	StatusHiringPending          = "hiring_pending"
	StatusHired                  = "hired"
	StatusRejected               = "rejected"
	StatusInductionOrg           = "induction_org"
	StatusInductionOrgDone       = "induction_org_done"
	StatusInductionTheory        = "induction_theory"
	StatusInductionEppPending    = "induction_epp_pending"
	StatusInductionPractice      = "induction_practice"
	StatusOnboardingComplete     = "onboarding_complete"
)

func InitialApplicationStatus() string { return StatusApplied }

// StatusLabel devuelve la etiqueta en espanol para mostrar en correos y
// reportes. Mantiene paridad con PIPELINE_STATUSES del frontend (web/types.ts).
// Si el estado no esta mapeado retorna el codigo tal cual para no romper nada.
func StatusLabel(status string) string {
	if v, ok := statusLabels[status]; ok {
		return v
	}
	return status
}

var statusLabels = map[string]string{
	StatusApplied:             "Postulación recibida",
	StatusDocsPending:         "Documentos pendientes",
	StatusDocsIncomplete:      "Documentos incompletos",
	StatusDocsReview:          "Documentos en revisión",
	StatusDocsApproved:        "Documentos aprobados",
	StatusInterviewPending:    "Entrevista pendiente",
	StatusInterviewDone:       "Entrevista realizada",
	StatusOccPending:          "Examen ocupacional pendiente",
	StatusOccSent:             "Examen enviado a la IPS",
	StatusOccResult:           "Resultado IPS recibido",
	StatusHiringPending:       "Decisión de contratación pendiente",
	StatusHired:               "Contratado",
	StatusRejected:            "Proceso descartado",
	StatusInductionOrg:        "Inducción organizacional",
	StatusInductionOrgDone:    "Inducción organizacional cerrada",
	StatusInductionTheory:     "Inducción teórica",
	StatusInductionEppPending: "Entrega de EPP pendiente",
	StatusInductionPractice:   "Inducción práctica",
	StatusOnboardingComplete:  "Onboarding completado",
}
