package resultshandling

import (
	"errors"

	"github.com/kubescape/kubescape/v4/core/cautils"
)

var ErrLegacyScanOutput = errors.New("v1 JSON has a legacy output contract; use ScanContext or select --format-version=v2")

// UsesLegacyJSONOutput keeps v1 reports on the legacy scan API. Adding a
// completion envelope to v1 would change its single-framework/array shape.
func UsesLegacyJSONOutput(info *cautils.ScanInfo) bool {
	if info == nil || info.FormatVersion != "v1" {
		return false
	}
	for _, format := range info.Formats() {
		if format == "json" {
			return true
		}
	}
	return false
}
