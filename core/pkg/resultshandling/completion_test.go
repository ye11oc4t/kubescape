package resultshandling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling/printer"
	printerv2 "github.com/kubescape/kubescape/v4/core/pkg/resultshandling/printer/v2"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type completionSpy struct {
	SpyPrinter
	closes int
}

func (*completionSpy) SupportsPartialScan() bool { return true }
func (p *completionSpy) CloseWriter() error      { p.closes++; return nil }

type submissionSpy struct {
	DummyReporter
	calls int
}

func (r *submissionSpy) Submit(context.Context, *cautils.OPASessionObj) error { r.calls++; return nil }

func TestPartialReportDelivery(t *testing.T) {
	for _, status := range []cautils.ScanCompletion{cautils.ScanComplete, cautils.ScanPartial} {
		for _, format := range []string{"json", "yaml"} {
			t.Run(string(status)+"/"+format, func(t *testing.T) {
				session := cautils.NewOPASessionObjMock()
				session.ScanCompletion = &cautils.ScanCompletionMetadata{Status: status, Scores: "provisional", Coverage: "provisional"}
				var output printer.IPrinter = printerv2.NewJsonPrinter()
				if format == "yaml" {
					output = printerv2.NewYamlPrinter()
				}
				path := filepath.Join(t.TempDir(), "report."+format)
				require.NoError(t, output.SetWriter(context.Background(), path))
				ui, submitter := &completionSpy{}, &submissionSpy{}
				h := NewResultsHandler(submitter, []printer.IPrinter{output}, ui)
				h.SetData(session)
				info := &cautils.ScanInfo{}
				info.Submit.SetBool(true)
				require.NoError(t, h.HandleResults(context.Background(), info))
				require.NoError(t, h.Close())
				require.Equal(t, 1, ui.closes)
				if status == cautils.ScanPartial {
					require.Zero(t, submitter.calls)
					require.Zero(t, ui.ScoreCalls)
				} else {
					require.Equal(t, 1, submitter.calls)
				}
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				if format == "yaml" {
					data, err = yaml.YAMLToJSON(data)
					require.NoError(t, err)
				}
				var report struct {
					Completion cautils.ScanCompletionMetadata `json:"scanCompletion"`
				}
				require.NoError(t, json.Unmarshal(data, &report))
				require.Equal(t, status, report.Completion.Status)
				canonical, err := h.ToJson()
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(canonical, &report))
				require.Equal(t, status, report.Completion.Status)
			})
		}
	}
}

func TestPartialReportRejectsUnsupportedPrinter(t *testing.T) {
	h := NewResultsHandler(nil, nil, &SpyPrinter{})
	h.SetData(cautils.NewOPASessionObjMock())
	h.ScanData.ScanCompletion = &cautils.ScanCompletionMetadata{Status: cautils.ScanPartial}
	require.ErrorContains(t, h.HandleResults(context.Background(), &cautils.ScanInfo{}), "cannot identify partial")
}

func TestPartialJSONWithSilentUI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.json")
	output := printerv2.NewJsonPrinter()
	require.NoError(t, output.SetWriter(context.Background(), path))
	h := NewResultsHandler(nil, []printer.IPrinter{output}, &printerv2.SilentPrinter{})
	h.SetData(cautils.NewOPASessionObjMock())
	h.ScanData.ScanCompletion = &cautils.ScanCompletionMetadata{Status: cautils.ScanPartial, Scores: "provisional", Coverage: "provisional"}
	require.NoError(t, h.HandleResults(context.Background(), &cautils.ScanInfo{}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"status":"partial"`)
}
