package resultshandling

import (
	"context"
	"github.com/kubescape/kubescape/v4/core/cautils"
)

// ScanOutcome owns Results, including their output writers. Consumers must
// render or Close the handler. Failed outcomes never expose a handler.
type ScanOutcome struct {
	Results     *ResultsHandler
	Completion  cautils.ScanCompletion
	FailedStage cautils.ScanStage
	Err         error
}

// OutcomeScanner is additive: existing IKubescape implementations are not
// required to implement the new API.
type OutcomeScanner interface {
	ScanWithOutcome(context.Context, *cautils.ScanInfo, []cautils.PolicyIdentifier) ScanOutcome
}
