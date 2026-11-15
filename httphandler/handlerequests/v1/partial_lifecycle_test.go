package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubescape/k8s-interface/workloadinterface"
	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/kubescape/v4/core/pkg/opaprocessor"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling"
	"github.com/kubescape/kubescape/v4/core/pkg/resultshandling/printer"
	printerv2 "github.com/kubescape/kubescape/v4/core/pkg/resultshandling/printer/v2"
	"github.com/kubescape/kubescape/v4/httphandler/config"
	"github.com/kubescape/kubescape/v4/httphandler/storage"
	"github.com/kubescape/opa-utils/reporthandling"
	"github.com/kubescape/opa-utils/resources"
	"github.com/kubescape/storage/pkg/generated/clientset/versioned/fake"
	"github.com/stretchr/testify/require"
)

// This exercises real OPA evaluation, HTTP cancellation, rendering, artifact
// polling and callback delivery. Resource collection is a deterministic fixture;
// this is not a substitute for a deployed operator integration test.
func TestHTTPPartialCancellationLifecycle(t *testing.T) {
	withTempOutputDirs(t)
	oldScan, oldRun := scanImpl, runKubescapeScan
	oldStorage, oldAccount := storage.GetStorage(), config.GetAccount()
	t.Cleanup(func() {
		scanImpl, runKubescapeScan = oldScan, oldRun
		storage.SetStorage(oldStorage)
		config.SetAccount(oldAccount)
	})
	//nolint:staticcheck // The pinned storage client lacks the summary apply schema required by NewClientset.
	storageClient := fake.NewSimpleClientset()
	store, err := storage.NewAPIServerStorage("partial-test", "default", storageClient.SpdxV1beta1(), false)
	require.NoError(t, err)
	storage.SetStorage(store)
	config.SetAccount("")
	t.Setenv(sweepOrphanedPostureResultsEnv, "true")
	t.Setenv(callbackAllowlistEnv, "127.0.0.1/32")
	callbackURL, callbacks, deliveries := countingCallbackReceiver(t)
	ready := make(chan struct{})
	submitter := &partialLifecycleSubmitter{}
	scanImpl = scan
	runKubescapeScan = func(ctx context.Context, info *cautils.ScanInfo, _ []cautils.PolicyIdentifier) (*resultshandling.ResultsHandler, error) {
		session := cautils.NewOPASessionObjMock()
		rule := reporthandling.PolicyRule{
			RuleLanguage: reporthandling.RegoLanguage,
			Rule: `package armo_builtins
deny[msg] {
  pod := input[_]
  pod.kind == "Pod"
  msg := {"alertMessage": "pod found", "packagename": "armo_builtins",
    "alertScore": 1, "fixPaths": [], "reviewPaths": [], "failedPaths": [],
    "alertObject": {"k8sApiObjects": [pod]}}
}`,
			Match: []reporthandling.RuleMatchObjects{{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"Pod"}}},
		}
		rule.Name = "partial-lifecycle-rule"
		control := reporthandling.Control{ControlID: "C-PARTIAL", BaseScore: 5, Rules: []reporthandling.PolicyRule{rule}}
		control.Name = "partial-lifecycle-control"
		framework := reporthandling.Framework{Controls: []reporthandling.Control{control}}
		framework.Name = "partial-lifecycle-framework"
		session.Policies = []reporthandling.Framework{framework}
		session.ResourceToControlsMap = map[string][]string{"/v1/pods": {control.ControlID}}
		batches := make(chan *cautils.ResourceBatch, 3)
		batches <- cautils.NewResourceBatch(cautils.ClusterScope)
		for _, namespace := range []string{"completed", "interrupted"} {
			pod := workloadinterface.NewWorkloadObj(map[string]any{
				"apiVersion": "v1", "kind": "Pod",
				"metadata": map[string]any{"name": "test-pod", "namespace": namespace},
			})
			batch := cautils.NewResourceBatch(namespace)
			batch.K8SResources["/v1/pods"] = []string{pod.GetID()}
			batch.AllResources[pod.GetID()] = pod
			batches <- batch
		}
		close(batches)
		producerErrors := make(chan error)
		close(producerErrors)
		processor := opaprocessor.NewOPAProcessor(session, resources.NewRegoDependenciesDataMock(), "test", "", "", false, nil)
		progress := &interruptThirdScope{ctx: ctx, ready: ready}
		primary := processor.ProcessWithStreaming(ctx, batches, producerErrors, progress, 2)
		primary = processor.FinalizePartial(ctx, primary)
		if session.ScanCompletion == nil || session.ScanCompletion.Status != cautils.ScanPartial {
			return nil, fmt.Errorf("expected finalized partial evaluation: %w", primary)
		}
		output := printerv2.NewJsonPrinter()
		if err := output.SetWriter(context.WithoutCancel(ctx), info.Output); err != nil {
			return nil, err
		}
		info.Submit.SetBool(true)
		results := resultshandling.NewResultsHandler(submitter, []printer.IPrinter{output}, &printerv2.SilentPrinter{})
		results.SetData(session)
		return results, primary
	}
	h := NewHTTPHandler(false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, h.Shutdown(ctx, time.Second))
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /scan", h.Scan)
	mux.HandleFunc("DELETE /scan", h.CancelScan)
	mux.HandleFunc("GET /results", h.GetResults)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Post(server.URL+"/scan?keep=true&callback="+url.QueryEscape(callbackURL), "application/json", strings.NewReader(`{"format":"json"}`))
	require.NoError(t, err)
	var accepted struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&accepted))
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NotEmpty(t, accepted.ID)
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation did not reach its second namespace")
	}
	request, err := http.NewRequest(http.MethodDelete, server.URL+"/scan?id="+accepted.ID, nil)
	require.NoError(t, err)
	response, err = client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	select {
	case callback := <-callbacks:
		require.Equal(t, accepted.ID, callback.ID)
		require.Equal(t, "partial", callback.Status)
		require.NotNil(t, callback.Completion)
		require.Equal(t, cautils.StageEvaluation, callback.Completion.FailedStage)
		require.Equal(t, "SCAN_CANCELED", callback.Completion.ReasonCode)
		require.Equal(t, "succeeded", callback.DeliveryStatus)
		require.Equal(t, accepted.ID, callback.ArtifactID)
	case <-time.After(5 * time.Second):
		t.Fatal("partial callback was not delivered")
	}
	response, err = client.Get(server.URL + "/results?id=" + accepted.ID + "&keep=true")
	require.NoError(t, err)
	var polled struct {
		Type     string              `json:"type"`
		Response partialScanResponse `json:"response"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&polled))
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusInternalServerError, response.StatusCode)
	require.Equal(t, "partialScan", polled.Type)
	require.Equal(t, cautils.ScanPartial, polled.Response.Completion.Status)
	require.Contains(t, polled.Response.Completion.CompletedScopes, "completed")
	require.NotContains(t, polled.Response.Completion.CompletedScopes, "interrupted")
	require.Equal(t, "succeeded", polled.Response.DeliveryStatus)
	require.Contains(t, string(polled.Response.Artifact), `"status":"partial"`)
	require.Contains(t, string(polled.Response.Artifact), "test-pod")
	require.Zero(t, submitter.calls.Load(), "partial reports must not be submitted")
	require.Empty(t, storageClient.Actions(), "partial reports must not store or sweep operator records")
	require.EqualValues(t, 1, deliveries.Load())
}

type interruptThirdScope struct {
	ctx   context.Context
	ready chan struct{}
	steps int
}

func (*interruptThirdScope) Start(int) {}
func (*interruptThirdScope) Stop()     {}
func (p *interruptThirdScope) ProgressJob(int, string) {
	p.steps++
	if p.steps == 3 {
		close(p.ready)
		<-p.ctx.Done()
	}
}

type partialLifecycleSubmitter struct{ calls atomic.Int32 }

func (s *partialLifecycleSubmitter) Submit(context.Context, *cautils.OPASessionObj) error {
	s.calls.Add(1)
	return nil
}
func (*partialLifecycleSubmitter) SetTenantConfig(cautils.ITenantConfig) {}
func (*partialLifecycleSubmitter) DisplayMessage()                       {}
