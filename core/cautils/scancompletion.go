package cautils

// ScanCompletion describes production of a posture result, independently of
// rendering, persistence, submission, and CI gates. Empty means a legacy producer.
type ScanCompletion string

const (
	ScanComplete ScanCompletion = "complete"
	ScanPartial  ScanCompletion = "partial"
	ScanFailed   ScanCompletion = "failed"
)

type ScanStage string

const (
	StageInitialization ScanStage = "initialization"
	StageCollection     ScanStage = "resource-collection"
	StageEvaluation     ScanStage = "policy-evaluation"
	StageFinalization   ScanStage = "finalization"
	StageEnrichment     ScanStage = "enrichment"
)

// ScanCompletionMetadata contains bounded, non-secret diagnostics. The original
// error belongs to the in-process outcome, never to the serialized report.
type ScanCompletionMetadata struct {
	Status            ScanCompletion `json:"status"`
	FailedStage       ScanStage      `json:"failedStage,omitempty"`
	ReasonCode        string         `json:"reasonCode,omitempty"`
	CompletedScopes   []string       `json:"completedScopes,omitempty"`
	Scores            string         `json:"scores"`
	Coverage          string         `json:"coverage"`
	SkippedScanStages []ScanStage    `json:"skippedScanStages,omitempty"`
}
