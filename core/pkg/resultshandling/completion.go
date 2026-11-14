package resultshandling

import "github.com/kubescape/kubescape/v4/core/cautils"

// IsPartial is a reporting guard, not an inference from handler presence,
// coverage, the error string, or a provisional numeric score.
func (rh *ResultsHandler) IsPartial() bool {
	return rh != nil && rh.ScanData != nil && rh.ScanData.ScanCompletion != nil && rh.ScanData.ScanCompletion.Status == cautils.ScanPartial
}
