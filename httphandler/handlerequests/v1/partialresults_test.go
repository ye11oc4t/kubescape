package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling"
	"github.com/stretchr/testify/require"
)

type partialHTTPPrinter struct {
	noOpPrinter
	closes int
	err    error
}

func (*partialHTTPPrinter) SupportsPartialScan() bool { return true }
func (p *partialHTTPPrinter) CloseWriter() error      { p.closes++; return nil }
func (p *partialHTTPPrinter) ActionPrint(context.Context, *cautils.OPASessionObj, []cautils.ImageScanData) error {
	return p.err
}

func partialHTTPResults(p *partialHTTPPrinter) *resultshandling.ResultsHandler {
	h := resultshandling.NewResultsHandler(nil, nil, p)
	h.SetData(cautils.NewOPASessionObjMock())
	h.ScanData.ScanCompletion = &cautils.ScanCompletionMetadata{Status: cautils.ScanPartial, FailedStage: cautils.StageEvaluation, ReasonCode: "SCAN_INTERRUPTED", Scores: "provisional", Coverage: "provisional"}
	return h
}

func TestHTTPPartialScanSyncAndPoll(t *testing.T) {
	withTempOutputDirs(t)
	oldScan, oldRun := scanImpl, runKubescapeScan
	t.Cleanup(func() { scanImpl, runKubescapeScan = oldScan, oldRun })
	scanImpl = scan
	p := &partialHTTPPrinter{}
	runKubescapeScan = func(context.Context, *cautils.ScanInfo, []cautils.PolicyIdentifier) (*resultshandling.ResultsHandler, error) {
		return partialHTTPResults(p), context.DeadlineExceeded
	}
	h := NewHTTPHandler(false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, h.Shutdown(ctx, time.Second))
	})
	w := httptest.NewRecorder()
	h.Scan(w, httptest.NewRequest(http.MethodPost, "/scan?wait=true&keep=true", bytes.NewBufferString(`{"format":"json"}`)))
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	var response struct {
		ID       string              `json:"id"`
		Type     string              `json:"type"`
		Response partialScanResponse `json:"response"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "partialScan", response.Type)
	require.Equal(t, cautils.ScanPartial, response.Response.Completion.Status)
	require.NotEmpty(t, response.Response.Artifact)
	require.Equal(t, 1, p.closes)
	w = httptest.NewRecorder()
	h.GetResults(w, httptest.NewRequest(http.MethodGet, "/results?id="+response.ID+"&keep=true", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"partial"`)
	require.Contains(t, w.Body.String(), `"artifact"`)
}

func TestHTTPPartialDeliveryFailurePreservesScanError(t *testing.T) {
	out := withTempOutputDirs(t)
	primary, renderErr := context.Canceled, errors.New("render failed")
	p := &partialHTTPPrinter{err: renderErr}
	id := uuid.NewString()
	err := preservePartialScan(context.Background(), partialHTTPResults(p), &cautils.ScanInfo{}, id, primary)
	require.ErrorIs(t, err, primary)
	require.ErrorIs(t, err, renderErr)
	var partial *partialScanError
	require.ErrorAs(t, err, &partial)
	require.Equal(t, "failed", partial.response().DeliveryStatus)
	require.Equal(t, id, partial.response().ArtifactID)
	data, readErr := readResultsFile(id)
	require.NoError(t, readErr)
	require.True(t, json.Valid(data))
	response, ok := partialResponseFromArtifact(data, id)
	require.True(t, ok)
	require.Equal(t, "unknown", response.DeliveryStatus)
	response = readPartialDelivery(response)
	require.Equal(t, "failed", response.DeliveryStatus)
	require.FileExists(t, filepath.Join(out, id))
}
