package opaprocessor

import (
	"context"
	"errors"
	"slices"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/opa-utils/reporthandling/apis"
)

// markInterrupted conservatively marks control-wide coverage incomplete even
// when some resource verdicts are available. It never turns missing work into
// a passing control. Call only after evaluation workers have joined.
func (opap *OPAProcessor) markInterrupted() {
	if opap.ErroredControls == nil {
		opap.ErroredControls = make(map[string]string)
	}
	if opap.AllPolicies != nil {
		for id := range opap.AllPolicies.Controls {
			if _, exists := opap.ErroredControls[id]; !exists {
				opap.ErroredControls[id] = "scan interrupted before all required work completed"
			}
		}
	}
}

// FinalizePartial is called by the owner after producer shutdown and worker
// completion. Failure leaves no public completion marker and preserves both
// errors. A pre-evaluation failure never manufactures a partial report.
func (opap *OPAProcessor) FinalizePartial(ctx context.Context, primary error) error {
	primary = cautils.AtScanStage(cautils.StageEvaluation, primary)
	if primary == nil || opap.completedEvaluations == 0 || len(opap.ResourcesResult) == 0 {
		return primary
	}
	if !opap.finalized {
		opap.markInterrupted()
	}
	if err := opap.finalizeResults(context.WithoutCancel(ctx)); err != nil {
		return errors.Join(primary, err)
	}
	if opap.scoreErr != nil {
		return errors.Join(primary, cautils.AtScanStage(cautils.StageFinalization, opap.scoreErr))
	}
	trustworthy := false
	for _, result := range opap.ResourcesResult {
		for _, control := range result.AssociatedControls {
			for _, rule := range control.ResourceAssociatedRules {
				if rule.Status == apis.StatusPassed || rule.Status == apis.StatusFailed {
					trustworthy = true
				}
			}
		}
	}
	if !trustworthy {
		return primary
	}
	scopes := slices.Clone(opap.completedScopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	reason := "SCAN_INTERRUPTED"
	if errors.Is(primary, context.Canceled) {
		reason = "SCAN_CANCELED"
	}
	if errors.Is(primary, context.DeadlineExceeded) {
		reason = "SCAN_DEADLINE_EXCEEDED"
	}
	opap.ScanCompletion = &cautils.ScanCompletionMetadata{
		Status: cautils.ScanPartial, FailedStage: cautils.FailedScanStage(primary, cautils.StageEvaluation),
		ReasonCode: reason, CompletedScopes: scopes, Scores: "provisional", Coverage: "provisional",
		SkippedScanStages: []cautils.ScanStage{cautils.StageEnrichment},
	}
	return primary
}
