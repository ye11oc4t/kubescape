package opaprocessor

import (
	"context"
	"errors"
	"testing"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/opa-utils/reporthandling"
	"github.com/kubescape/opa-utils/resources"
	"github.com/stretchr/testify/require"
)

func TestPartialFinalizationAfterCompletedBatch(t *testing.T) {
	for _, primary := range []error{context.Canceled, context.DeadlineExceeded, errors.New("producer failed")} {
		t.Run(primary.Error(), func(t *testing.T) {
			session, _ := brokenRuleSession()
			session.Policies[0].Controls[0].Rules[0].Rule = failEveryPodRego
			p := NewOPAProcessor(session, resources.NewRegoDependenciesDataMock(), "test", "", "", false, nil)
			batch := cautils.NewResourceBatch(cautils.ClusterScope)
			batch.AllResources, batch.K8SResources = session.AllResources, session.K8SResources
			batches := make(chan *cautils.ResourceBatch, 1)
			batches <- batch
			producerErrors := make(chan error, 1)
			// Use the progress seam to interrupt only after resident evaluation.
			progress := &afterScopeProgress{done: func() { producerErrors <- primary }}
			err := p.ProcessWithStreaming(context.Background(), batches, producerErrors, progress, 0)
			require.ErrorIs(t, err, primary)
			require.ErrorIs(t, p.FinalizePartial(context.Background(), err), primary)
			require.NotNil(t, session.ScanCompletion)
			require.Equal(t, cautils.ScanPartial, session.ScanCompletion.Status)
			require.Equal(t, "provisional", session.ScanCompletion.Scores)
			require.NotEmpty(t, session.ResourcesResult)
		})
	}
}

// The error is queued as evaluation starts; ProcessWithStreaming cannot select
// it until the synchronous scope call and its workers have completed.
type afterScopeProgress struct{ done func() }

func (*afterScopeProgress) Start(int)                 {}
func (p *afterScopeProgress) ProgressJob(int, string) { p.done() }
func (*afterScopeProgress) Stop()                     {}

func TestPartialFinalizationRejectsUntrustedState(t *testing.T) {
	primary := context.Canceled
	session, _ := brokenRuleSession()
	p := NewOPAProcessor(session, resources.NewRegoDependenciesDataMock(), "test", "", "", false, nil)
	require.ErrorIs(t, p.FinalizePartial(context.Background(), primary), primary)
	require.Nil(t, session.ScanCompletion)
	p.completedEvaluations = 1
	p.AllPolicies = &cautils.Policies{Controls: map[string]reporthandling.Control{}}
	// A result that refers to a missing resource fails finalization.
	session.ResourcesResult["missing"] = session.ResourcesResult["missing"]
	err := p.FinalizePartial(context.Background(), primary)
	require.ErrorIs(t, err, primary)
	require.Contains(t, err.Error(), "accepted catalog")
	require.Nil(t, session.ScanCompletion)
}
