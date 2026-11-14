package opaprocessor

import (
	"context"
	"testing"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/opa-utils/resources"
	"github.com/stretchr/testify/require"
)

func TestStreamingNilProducerDiagnosticDoesNotEndEvaluation(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		session, _ := brokenRuleSession()
		session.Policies[0].Controls[0].Rules[0].Rule = failEveryPodRego
		batch := cautils.NewResourceBatch(cautils.ClusterScope)
		batch.AllResources, batch.K8SResources = session.AllResources, session.K8SResources
		batches := make(chan *cautils.ResourceBatch, 1)
		batches <- batch
		close(batches)
		failures := make(chan error, 1)
		failures <- nil
		close(failures)
		p := NewOPAProcessor(session, resources.NewRegoDependenciesDataMock(), "test", "", "", false, nil)
		require.NoError(t, p.ProcessWithStreaming(context.Background(), batches, failures, nil, 0))
		require.True(t, p.finalized)
		require.NotEmpty(t, session.ResourcesResult)
	}
}
