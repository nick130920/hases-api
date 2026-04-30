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
