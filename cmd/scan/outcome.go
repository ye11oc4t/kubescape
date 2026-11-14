package scan

import (
	"context"
	"errors"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/meta"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling"
)

// scanPostureResults consumes the optional outcome API without expanding
// IKubescape. Partial artifacts are rendered once, but no handler is returned
// to gates/fleet aggregation, which require a completed measurement.
func scanPostureResults(ctx context.Context, info *cautils.ScanInfo, ks meta.IKubescape, policies []cautils.PolicyIdentifier) (*resultshandling.ResultsHandler, error) {
	scanner, ok := ks.(resultshandling.OutcomeScanner)
	if !ok || info.DryRun {
		return ks.ScanContext(ctx, info, policies)
	}
	outcome := scanner.ScanWithOutcome(ctx, info, policies)
	switch outcome.Completion {
	case cautils.ScanComplete:
		return outcome.Results, outcome.Err
	case cautils.ScanPartial:
		// Finalization is finished. Local rendering must not be suppressed by
		// the cancellation that interrupted production of the scan.
		deliveryErr := outcome.Results.HandleResults(context.WithoutCancel(ctx), info)
		return nil, errors.Join(outcome.Err, deliveryErr)
	default:
		return nil, outcome.Err
	}
}
