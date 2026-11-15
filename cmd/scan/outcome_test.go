package scan

import (
	"context"
	"errors"
	"testing"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/meta"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling/printer"
	"github.com/stretchr/testify/require"
)

type outcomeScannerStub struct {
	meta.IKubescape
	outcome resultshandling.ScanOutcome
}

func (s *outcomeScannerStub) ScanWithOutcome(context.Context, *cautils.ScanInfo, []cautils.PolicyIdentifier) resultshandling.ScanOutcome {
	return s.outcome
}

type outcomePrinter struct {
	printer.IPrinter
	prints, closes int
	printErr       error
}

func (*outcomePrinter) SupportsPartialScan() bool { return true }
func (p *outcomePrinter) ActionPrint(context.Context, *cautils.OPASessionObj, []cautils.ImageScanData) error {
	p.prints++
	return p.printErr
}
func (*outcomePrinter) PrintNextSteps()      {}
func (*outcomePrinter) Score(float32)        {}
func (p *outcomePrinter) CloseWriter() error { p.closes++; return nil }

func TestAllPostureCommandsPreservePartialFailure(t *testing.T) {
	for name, run := range map[string]fleetRunner{"scan": runSecurityScan, "framework": runFrameworkScan, "control": runControlScan, "workload": runWorkloadScan} {
		for _, renderFails := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{false: "render", true: "render-error"}[renderFails], func(t *testing.T) {
				productionErr, deliveryErr := context.DeadlineExceeded, errors.New("disk full")
				p := &outcomePrinter{}
				if renderFails {
					p.printErr = deliveryErr
				}
				h := resultshandling.NewResultsHandler(nil, nil, p)
				h.SetData(cautils.NewOPASessionObjMock())
				h.ScanData.ScanCompletion = &cautils.ScanCompletionMetadata{Status: cautils.ScanPartial}
				ks := &outcomeScannerStub{outcome: resultshandling.ScanOutcome{Results: h, Completion: cautils.ScanPartial, Err: productionErr}}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				results, err := run(ctx, &cautils.ScanInfo{}, ks, nil)
				require.ErrorIs(t, err, productionErr)
				if renderFails {
					require.ErrorIs(t, err, deliveryErr)
				}
				require.Nil(t, results, "fleet must not classify partial data as a scanned cluster")
				require.Equal(t, 1, p.prints)
				require.Equal(t, 1, p.closes)
			})
		}
	}
}

func TestFailedOutcomeDoesNotRender(t *testing.T) {
	ks := &outcomeScannerStub{outcome: resultshandling.ScanOutcome{Completion: cautils.ScanFailed, Err: context.Canceled}}
	results, err := scanPostureResults(context.Background(), &cautils.ScanInfo{}, ks, nil)
	require.Nil(t, results)
	require.ErrorIs(t, err, context.Canceled)
}

type legacyOutcomeScanner struct {
	outcomeScannerStub
	legacyCalled bool
}

func (s *legacyOutcomeScanner) ScanContext(context.Context, *cautils.ScanInfo, []cautils.PolicyIdentifier) (*resultshandling.ResultsHandler, error) {
	s.legacyCalled = true
	return nil, context.Canceled
}

func TestLegacyJSONUsesLegacyScanContext(t *testing.T) {
	s := &legacyOutcomeScanner{}
	_, err := scanPostureResults(context.Background(), &cautils.ScanInfo{Format: "json", FormatVersion: "v1"}, s, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, s.legacyCalled)
}
