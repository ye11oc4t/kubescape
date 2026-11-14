package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling"
	utilsapisv1 "github.com/kubescape/opa-utils/httpserver/apis/v1"
)

const partialScanResponseType utilsapisv1.ScanResponseType = "partialScan"

type partialScanResponse struct {
	Completion     *cautils.ScanCompletionMetadata `json:"scanCompletion"`
	ArtifactID     string                          `json:"artifactID,omitempty"`
	Artifact       json.RawMessage                 `json:"artifact,omitempty"`
	DeliveryStatus string                          `json:"deliveryStatus"`
	DeliveryCode   string                          `json:"deliveryCode,omitempty"`
}

type partialScanError struct {
	id                   string
	completion           *cautils.ScanCompletionMetadata
	scanErr, deliveryErr error
	persisted            bool
}

func (e *partialScanError) Error() string {
	return "posture scan interrupted; partial results are available only when artifactID is present"
}
func (e *partialScanError) Unwrap() []error { return []error{e.scanErr, e.deliveryErr} }
func (e *partialScanError) response() partialScanResponse {
	r := partialScanResponse{Completion: e.completion, DeliveryStatus: "succeeded"}
	if e.persisted {
		r.ArtifactID = e.id
	}
	if e.deliveryErr != nil {
		r.DeliveryStatus, r.DeliveryCode = "failed", "ARTIFACT_DELIVERY_FAILED"
	}
	return r
}

// Partial artifacts are local per-scan files. They never enter the operator
// storage/overwrite/sweep path or the complete-report submission protocol.
func preservePartialScan(ctx context.Context, result *resultshandling.ResultsHandler, info *cautils.ScanInfo, id string, primary error) error {
	deliveryErr := result.HandleResults(context.WithoutCancel(ctx), info)
	persistErr := persistPartialCanonicalResult(result, id)
	return &partialScanError{id: id, completion: result.ScanData.ScanCompletion, scanErr: primary, deliveryErr: errors.Join(deliveryErr, persistErr), persisted: persistErr == nil}
}

func persistPartialCanonicalResult(result *resultshandling.ResultsHandler, id string) (err error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid scan ID: %w", err)
	}
	f, err := os.CreateTemp(OutputDir, ".partial-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer func() { _ = os.Remove(temporary) }()
	writeErr := result.WriteJson(f)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	err = errors.Join(writeErr, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(OutputDir, parsed.String()))
}

func partialResponseFromArtifact(data json.RawMessage, id string) (partialScanResponse, bool) {
	var header struct {
		Completion *cautils.ScanCompletionMetadata `json:"scanCompletion"`
	}
	if json.Unmarshal(data, &header) != nil || header.Completion == nil || header.Completion.Status != cautils.ScanPartial {
		return partialScanResponse{}, false
	}
	return partialScanResponse{Completion: header.Completion, ArtifactID: id, Artifact: data, DeliveryStatus: "succeeded"}, true
}
