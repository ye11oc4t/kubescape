package cautils

import "errors"

// ScanCompletion describes production of a posture result, independently of
// rendering, persistence, submission, and CI gates. Empty means a legacy producer.
type ScanCompletion string

const (
	ScanComplete ScanCompletion = "complete"
	ScanPartial  ScanCompletion = "partial"
	ScanFailed   ScanCompletion = "failed"
)

// ScanStageError preserves the stage at the boundary where an error originates.
// Errors remain unwrap-compatible; later callers must not relabel a producer
// failure as an evaluation failure merely because evaluation was in progress.
type ScanStageError struct {
	Stage ScanStage
	Err   error
}

func (e *ScanStageError) Error() string { return e.Err.Error() }
func (e *ScanStageError) Unwrap() error { return e.Err }

func AtScanStage(stage ScanStage, err error) error {
	if err == nil {
		return nil
	}
	var located *ScanStageError
	if errors.As(err, &located) {
		return err
	}
	return &ScanStageError{Stage: stage, Err: err}
}

func FailedScanStage(err error, fallback ScanStage) ScanStage {
	var located *ScanStageError
	if errors.As(err, &located) {
		return located.Stage
	}
	return fallback
}

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
