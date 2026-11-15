package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kubescape/kubescape/v4/core/cautils"
	"github.com/kubescape/opa-utils/reporthandling"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/types"
	"github.com/stretchr/testify/require"
)

var outcomePrivacyCancels sync.Map

// Register the fixture builtin before any tests or concurrent OPA evaluations.
// A deferred whole-cluster control invokes it after normal scope evaluation,
// giving the real public scan API a deterministic cancellation without sleeps,
// replacing the scanner, or adding a production test hook.
func init() {
	rego.RegisterBuiltin1(&rego.Function{
		Name:             "test.cancel_partial_privacy_scan",
		Decl:             types.NewFunction(types.Args(types.S), types.B),
		Nondeterministic: true,
	}, func(_ rego.BuiltinContext, token *ast.Term) (*ast.Term, error) {
		key, ok := token.Value.(ast.String)
		if !ok {
			return nil, fmt.Errorf("privacy fixture requires a string token")
		}
		cancel, ok := outcomePrivacyCancels.Load(string(key))
		if !ok {
			return nil, fmt.Errorf("privacy fixture has no active scan")
		}
		cancel.(context.CancelFunc)()
		return nil, context.Canceled
	})
}

func TestScanWithOutcomePartialPrivacy(t *testing.T) {
	t.Setenv("LARGE_CLUSTER_SIZE", "1")
	t.Setenv("KUBESCAPE_MASTER_KEY", "partial-privacy-test-master-secret-32-bytes")
	t.Setenv("KUBESCAPE_MASTER_KEY_HEX", "")
	for _, streaming := range []bool{false, true} {
		for _, privacy := range []string{"plain", "hide", "encrypt", "encrypt-unconfigured"} {
			t.Run(fmt.Sprintf("streaming=%t/%s", streaming, privacy), func(t *testing.T) {
				info, policies := partialPrivacyScanInfo(t)
				info.EnableStreaming = streaming
				info.Hide = privacy == "hide"
				info.EncryptionEnabled = privacy == "encrypt" || privacy == "encrypt-unconfigured"
				if privacy == "encrypt-unconfigured" {
					t.Setenv("KUBESCAPE_MASTER_KEY", "")
				}
				info.Output = filepath.Join(t.TempDir(), "partial.json")
				require.NoError(t, os.WriteFile(info.Output, []byte("previous report"), 0o600))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				outcomePrivacyCancels.Store(t.Name(), context.CancelFunc(cancel))
				defer outcomePrivacyCancels.Delete(t.Name())

				outcome := NewKubescape(context.Background()).ScanWithOutcome(ctx, info, policies)
				require.ErrorIs(t, outcome.Err, context.Canceled)
				if privacy == "encrypt-unconfigured" {
					require.Equal(t, cautils.ScanFailed, outcome.Completion)
					require.Nil(t, outcome.Results)
					require.ErrorContains(t, outcome.Err, "encryption requires")
					before, err := os.ReadFile(info.Output)
					require.NoError(t, err)
					require.Equal(t, "previous report", string(before))
					return
				}
				require.Equal(t, cautils.ScanPartial, outcome.Completion)
				require.Equal(t, cautils.StageEvaluation, outcome.FailedStage)
				require.NotNil(t, outcome.Results)
				t.Cleanup(func() { require.NoError(t, outcome.Results.Close()) })
				session := outcome.Results.GetData()
				require.NotEmpty(t, session.ResourcesResult)
				require.Equal(t, "provisional", session.ScanCompletion.Scores)
				require.Equal(t, "SCAN_CANCELED", session.ScanCompletion.ReasonCode)
				if privacy == "plain" {
					require.NotEmpty(t, session.ScanCompletion.CompletedScopes)
					if !streaming {
						require.Contains(t, session.ScanCompletion.CompletedScopes, "private-team-one")
						require.Contains(t, session.ScanCompletion.CompletedScopes, "private-team-two")
					}
				} else {
					require.Empty(t, session.ScanCompletion.CompletedScopes)
				}
				if privacy == "encrypt" {
					require.NotNil(t, session.Metadata.EncryptionMetadata)
				}
				before, err := os.ReadFile(info.Output)
				require.NoError(t, err)
				require.Equal(t, "previous report", string(before))
				canonical, err := outcome.Results.ToJson()
				require.NoError(t, err)
				require.NoError(t, outcome.Results.HandleResults(context.WithoutCancel(ctx), info))
				rendered, err := os.ReadFile(info.Output)
				require.NoError(t, err)
				for _, artifact := range [][]byte{canonical, rendered} {
					require.True(t, json.Valid(artifact))
					require.Contains(t, string(artifact), `"status":"partial"`)
					if privacy == "plain" {
						require.Contains(t, string(artifact), "private-pod-one")
					} else {
						for _, secret := range []string{"private-team-one", "private-team-two", "private-pod-one", "private-pod-two"} {
							require.NotContains(t, string(artifact), secret)
						}
						require.NotContains(t, string(artifact), `"completedScopes"`)
					}
				}
			})
		}
	}
}

func partialPrivacyScanInfo(t *testing.T) (*cautils.ScanInfo, []cautils.PolicyIdentifier) {
	t.Helper()
	info, policies, good := buildBrokenRuleScanInfo(t)
	info.Format, info.FormatVersion, info.NoClusterAccess = "json", "v2", true
	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: private-team-one
---
apiVersion: v1
kind: Namespace
metadata:
  name: private-team-two
---
apiVersion: v1
kind: Pod
metadata:
  name: private-pod-one
  namespace: private-team-one
---
apiVersion: v1
kind: Pod
metadata:
  name: private-pod-two
  namespace: private-team-two
`
	require.NoError(t, os.WriteFile(info.InputPatterns[0], []byte(manifest), 0o600))
	good.ControlID, good.Name = "C-PRIVACY-GOOD", "accepted pod findings"
	good.Rules[0].Match[0].Resources = []string{"Pod", "Namespace"}
	good.Rules[0].Name = "accepted-pods"
	good.Rules[0].Rule = `package armo_builtins
deny[msg] {
  pod := input[_]
  pod.kind == "Pod"
  msg := {"alertMessage": "pod found", "packagename": "armo_builtins",
    "alertScore": 1, "fixPaths": [], "reviewPaths": [], "failedPaths": [],
    "alertObject": {"k8sApiObjects": [pod]}}
}`
	interrupt := good
	interrupt.ControlID, interrupt.Name = "C-PRIVACY-INTERRUPT", "interrupt after accepted findings"
	interrupt.Attributes = map[string]interface{}{"requiresWholeClusterInput": true}
	interrupt.Rules = append([]reporthandling.PolicyRule(nil), good.Rules...)
	interrupt.Rules[0].Name = "cancel-after-scope-evaluation"
	interrupt.Rules[0].Rule = fmt.Sprintf(`package armo_builtins
deny[msg] {
  test.cancel_partial_privacy_scan(%q)
  pod := input[_]
  msg := {"alertObject": {"k8sApiObjects": [pod]}}
}`, t.Name())
	framework := reporthandling.Framework{Controls: []reporthandling.Control{good, interrupt}}
	framework.Name = policies[0].Identifier
	data, err := json.Marshal(framework)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(info.UseFrom[0], data, 0o600))
	return info, policies
}
