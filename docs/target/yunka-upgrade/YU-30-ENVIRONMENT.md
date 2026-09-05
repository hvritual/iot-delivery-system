# YU-30 execution environment and evidence contract

The execution architecture separates repository I/O from process execution. A restricted assistant shell is not the authoritative test runner.

## Online execution

`.github/workflows/yu30-regression.yml` checks out the exact push SHA or PR head SHA and the reviewed Yunka gitlink, then installs Go 1.25.13, Node 22.16.0, protoc 21.12 and pinned Go plugins. The canonical full suite and independent Go, frontend and browser jobs run on GitHub-hosted Ubuntu 24.04. Each job retains its exact source SHA, command output and final worktree diff as an artifact.

A workflow run is not accepted merely because setup succeeded or a commit's legacy combined statuses array is empty. Read the Actions run and every required job at the exact tested SHA. A queued, cancelled, skipped or failed required job cannot certify completion.

The canonical source must remain clean. Generation is explicit and is followed by a read-only full check and a drift check on both passes. Go module resolution must agree with the committed go.mod/go.sum. The framework CLI explicitly selects third_party/yunka/go.work; consumer tests select GOWORK=off so the legacy comparison backend cannot alter their dependency graph.

## Restricted-shell source access

`.github/workflows/yu30-workspace.yml` exports committed consumer and framework histories as Git bundles, plus exact HEAD identifiers and SHA256SUMS. Download the artifact through the connected GitHub artifact action, verify checksums, clone from the local bundles and verify both HEADs. This materializes an authentic checkout without disabling TLS, exposing a GitHub token, changing DNS globally, or inventing a source snapshot from partial file excerpts.

Source export contains committed Git objects only. It must never archive the runner workspace, .git/config, credentials, environment variables, node_modules, session fixtures, databases or runtime secret manifests. Source artifacts are retained for one day and command evidence for seven days.

The assistant container's direct DNS/network restrictions were not removed by this solution. They no longer block certification because execution happens on the verified CI runner. Local source access was independently verified by cloning and hash-checking the offline bundles. A local Go 1.23 parser check is not a Go 1.25.13 compilation or test pass.

## Review and mutation

Normal regression workflows have contents:read and checkout persist-credentials:false. No reusable token is stored or uploaded. For this repair, branch-scoped, one-time workflows applied a pre-reviewed patch, checked branch HEAD equality and forbidden paths, committed with the local Git CLI and pushed only codex/yu-30-full-regression. Each writer removed its own workflow and patch in the repair commit. The final branch must contain no permanent automated writer.

Main is updated only after exact-SHA job readback, scope review and canonical parent verification. A non-force update is required; no deployment is performed. Changing TASKS.md to the next task does not constitute a test result.

## Governance evidence distinctions

Yunka ownership check covers changed handwritten Go files, and ownership inspect verifies generated files remain generator-owned. Frontend and CI files are outside that classifier and are covered by their own scopes and executable gates.

Yunka audit is evaluated from the JSON debt delta: exit zero alone is insufficient. Existing findings remain visible; no-new-debt does not mean no debt.

The ChangeSet test constructs contracts from all current canonical operations at the tested HEAD, reconciles them, adds a controlled out-of-scope file in an isolated Git worktree and requires an exact scope rejection, then restores the worktree and reconciles again. It tests real CLI behavior and semantic readback, not a claim that operation contracts authorize unrelated infrastructure or frontend edits.

## Next type-generation drift

Real build and browser runs at 04ffc65625b66be5f95aa50ee17554a57bb3faa6 regenerated web/next-env.d.ts differently: production referenced .next/types while development referenced .next/dev/types; both added root-params references. Next owns this mode-specific file. It is now untracked and ignored, remains in tsconfig include, and the strict typecheck command first executes `next typegen`. No generated declaration is hand-edited and no TypeScript check is disabled.

Official contract: https://nextjs.org/docs/app/api-reference/config/typescript#next-envdts and https://nextjs.org/docs/app/api-reference/cli/next#next-typegen-options .
