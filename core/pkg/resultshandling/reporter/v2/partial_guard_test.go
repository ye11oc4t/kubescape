package reporter

import (
	"context"
	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReporterRejectsPartialBeforeAnyBackendCall(t *testing.T) {
	report := &ReportEventReceiver{}
	data := &cautils.OPASessionObj{ScanCompletion: &cautils.ScanCompletionMetadata{Status: cautils.ScanPartial}}
	require.ErrorContains(t, report.Submit(context.Background(), data), "cannot submit a partial scan")
}
