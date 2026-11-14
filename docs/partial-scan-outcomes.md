# Posture scan completion

The additive `ScanWithOutcome(ctx, scanInfo, policies)` API distinguishes a
complete scan, a finalized partial scan, and a failed scan without publishable
results. `Scan` and `ScanContext` retain their existing error behavior. Dry-run
continues to use `ScanContext` because it produces no posture artifact.

| Completion | Results | Scan error |
| --- | --- | --- |
| complete | finalized | nil |
| partial | finalized, provisional scores and coverage | original interruption |
| failed | nil | non-nil |

Consumers of the outcome API own the returned results and must render them or
call `Close`. Rendering, closing, persistence, submission and threshold errors
do not change the scan's completion. Preserve the scan error when combining it
with those errors, using `errors.Join` rather than replacing it.

JSON, YAML and pretty output support partial results. Machine output from the
new API explicitly carries `scanCompletion.status` for both complete and partial
scans. Legacy producers omit the block; absence means unspecified, not complete.
Partial artifacts contain stable reason codes, provisional score/coverage flags,
and known completed scope identities. Scope identities are omitted under `--hide`
and `--encrypt`. Original error strings are not copied into report metadata.

Other printers reject partial rendering until they can encode completion safely.
Partial reports are not submitted through the existing complete-report protocol,
nor exported as full-scan telemetry or completion notifications. A supported
local artifact can still be retained while scanning exits nonzero.

Rule-level evaluation failures retain the existing `Skipped / NotEvaluated`
behavior. They are not scan interruptions. Use `--fail-coverage-below 100` to
enforce complete control coverage. Cancellation, producer failures and collection
errors remain scan failures; only stable, finalized evaluated work can be partial.

The processor retains accepted resources before evaluating a streaming scope,
joins evaluation workers and waits for producer channel closure before partial
finalization. Interrupted control-wide coverage is conservative; numeric values
describe available work and must not be compared as final cluster scores.

Related design: https://github.com/kubescape/designs-and-proposals/pull/15
