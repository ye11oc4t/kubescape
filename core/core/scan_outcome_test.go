package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling"
	"github.com/kubescape/opa-utils/reporthandling/apis"
	"github.com/stretchr/testify/require"
)

func TestScanWithOutcomeCompletePreservesFileUntilRendering(t *testing.T) {
	info, policies, control := buildBrokenRuleScanInfo(t)
	info.Format, info.FormatVersion = "json", "v2"
	info.Output = filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(info.Output, []byte("previous report"), 0o600))
	outcome := NewKubescape(context.Background()).ScanWithOutcome(context.Background(), info, policies)
	require.NoError(t, outcome.Err)
	require.Equal(t, cautils.ScanComplete, outcome.Completion)
	requireBrokenControlNotEvaluated(t, outcome.Results, control)
	before, err := os.ReadFile(info.Output)
	require.NoError(t, err)
	require.Equal(t, "previous report", string(before))
	require.NoError(t, outcome.Results.HandleResults(context.Background(), info))
	after, err := os.ReadFile(info.Output)
	require.NoError(t, err)
	require.Contains(t, string(after), `"status":"complete"`)
	require.NoError(t, outcome.Results.Close())
}

func TestScanWithOutcomePreResultFailureKeepsExistingOutput(t *testing.T) {
	info, policies, _ := buildBrokenRuleScanInfo(t)
	info.Format, info.FormatVersion = "json", "v2"
	info.Output = filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(info.Output, []byte("previous report"), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := NewKubescape(context.Background()).ScanWithOutcome(ctx, info, policies)
	require.ErrorIs(t, outcome.Err, context.Canceled)
	require.Equal(t, cautils.ScanFailed, outcome.Completion)
	require.Nil(t, outcome.Results)
	data, err := os.ReadFile(info.Output)
	require.NoError(t, err)
	require.Equal(t, "previous report", string(data))
}

func TestScanWithOutcomeRejectsLegacyJSONBeforeOpeningOutput(t *testing.T) {
	info, policies, _ := buildBrokenRuleScanInfo(t)
	info.Format, info.FormatVersion = "json", "v1"
	info.Output = filepath.Join(t.TempDir(), "legacy.json")
	require.NoError(t, os.WriteFile(info.Output, []byte("existing legacy report"), 0o600))
	ks := NewKubescape(context.Background())
	outcome := ks.ScanWithOutcome(context.Background(), info, policies)
	require.ErrorIs(t, outcome.Err, resultshandling.ErrLegacyScanOutput)
	require.Equal(t, cautils.ScanFailed, outcome.Completion)
	require.Nil(t, outcome.Results)
	before, err := os.ReadFile(info.Output)
	require.NoError(t, err)
	require.Equal(t, "existing legacy report", string(before))
	results, err := ks.ScanContext(context.Background(), info, policies)
	require.NoError(t, err)
	require.NoError(t, results.HandleResults(context.Background(), info))
	legacy, err := os.ReadFile(info.Output)
	require.NoError(t, err)
	require.NotContains(t, string(legacy), "scanCompletion")
}

type joinedStreamingMock struct {
	streamingCancelMock
	done chan struct{}
}

func (m *joinedStreamingMock) StreamResourcesBatches(ctx context.Context, session *cautils.OPASessionObj, _ *cautils.ScanInfo) (<-chan *cautils.ResourceBatch, <-chan error, int, error) {
	session.InfoMap = make(map[string]apis.StatusInfo)
	batches, failures := make(chan *cautils.ResourceBatch, 1), make(chan error, 1)
	batches <- cautils.NewResourceBatch("not-resident")
	go func() {
		defer close(failures)
		defer close(batches)
		defer close(m.done)
		<-ctx.Done()
		session.InfoMap["producer-exited"] = apis.StatusInfo{}
	}()
	return batches, failures, 1, nil
}

func TestStreamingFailureJoinsProducerBeforeReturning(t *testing.T) {
	m := &joinedStreamingMock{done: make(chan struct{})}
	session := cautils.NewOPASessionObjMock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := collectAndProcessResourcesWithStreaming(ctx, m, session, &cautils.ScanInfo{}, "test", "", "", false, 0, 10)
	require.ErrorContains(t, err, "first batch must be resident")
	require.Equal(t, cautils.StageCollection, cautils.FailedScanStage(err, cautils.StageInitialization))
	select {
	case <-m.done:
	default:
		t.Fatal("producer still running after scan returned")
	}
	_, ok := session.InfoMap["producer-exited"]
	require.True(t, ok)
}
