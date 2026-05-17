# platform-attest-coordinator

External state machine that gates image signing. It replaces the decision logic that previously lived in `02-attest-build-listener.groovy` inside Jenkins.

## End-to-end container flow

```
Developer pushes code
  └─ SCM trigger → Jenkins build (microservicePipeline, runTests, buildApp)
       └─ build completes
            └─ 03-attest-coordinator-shim fires (RunListener — developer cannot bypass)
                 └─ POST /webhook/build-event → platform-attest-coordinator
                      ├─ checks base standards (JUnit, SCM trigger, auditId, artifacts)
                      ├─ triggers platform/{team}/scan
                      ├─ triggers platform/{team}/{repo}/source-scan
                      └─ waits for both callbacks...
                           ├─ image scan done → POST /webhook/build-event
                           └─ source scan done → POST /webhook/build-event
                                └─ coordinator has all evidence
                                     ├─ GET /builds/{auditId}/summary (platform-audit-service)
                                     ├─ POST /authorize (Cedar — Attest action)
                                     └─ ALLOW → schedules platform/{team}/attest
                                          └─ attest job: cosign signs image
                                               └─ OCI registry: pipeline/v1 + scan/v1 attestations

                                                    [later — team triggers release]

                                                    PlatformReleasePipeline → POST /token (platform-token-service)
                                                         ├─ gate: scan/v1 attestation on image_ref must exist
                                                         ├─ gate: Cedar IssueCredentials ALLOW
                                                         └─ returns STS creds → terraform apply
```

The coordinator and the token service do not call each other. Their connection is the OCI registry: the coordinator is the only path to getting an image signed, and the token service refuses to issue AWS credentials for an unsigned image. A developer cannot deploy without passing through the coordinator's gates.

## Why this exists

The original Groovy `RunListener` was untestable — Jenkins API dependencies made unit testing impossible. The security requirement is that developers must call platform library steps (`microservicePipeline`, `runTests`, `buildApp`, etc.) and cannot self-attest. All attestation logic now lives in Go (testable with `go test`) and Cedar (policy-as-code), with Groovy reduced to a dumb data-extraction shim.

## Architecture

```
Jenkins RunListener (03-attest-coordinator-shim.groovy)
    → POST /webhook/build-event   (all tracked job completions)
          ↓
  platform-attest-coordinator    (this service)
    ├── OnBuildComplete: validate base standards, trigger image-scan + source-scan
    ├── OnScanComplete: record image scan result
    ├── OnSourceScanComplete: record source scan result
    └── tryAttest (runs after every event):
          ├── fetch audit summary from platform-audit-service
          ├── call Cedar /authorize (platform-cedar-sidecar)
          └── if ALLOW → POST to Jenkins attest job via jenkins-operator credentials
```

The coordinator is the only thing that triggers scans and schedules the attest job. Developers have no path to influence it.

## Job path routing

The webhook routes by job path pattern:

| Pattern | Handler |
|---|---|
| `teams/{team}/{repo}/build` | `OnBuildComplete` |
| `platform/services/{slug}/build` | `OnBuildComplete` |
| `platform/{team}/scan` | `OnScanComplete` |
| `platform/services/{slug}/scan` | `OnScanComplete` |
| `platform/{team}/{repo}/source-scan` | `OnSourceScanComplete` |

## What the Groovy shim sends

`03-attest-coordinator-shim.groovy` extracts everything the coordinator needs from Jenkins internals and POSTs it as JSON. Key fields:

- `jobPath`, `buildNumber`, `result`, `auditId`
- `upstreamJob`, `upstreamBuild` — present on scan jobs, used to look up the build record
- `junitTotal`, `junitFailed`, `lineCoverage`, `covThreshold`
- `hasArtifacts`, `scmTriggered`, `gitCommit`, `gitUrl`, `branch`
- `imageRef` — parsed from `artifacts.json`
- `stages` — from `DepthFirstScanner` over the pipeline graph
- `libraries` — from `LibrariesAction` (name + pinned SHA)
- `librarySteps` — from `audit-log.json` artifact, lists steps attributed to `jenkins-library`

## Cedar policy enforcement

All business rules live in `platform-cedar/policies/attest-gate.cedar`. The coordinator constructs a Cedar request from the build record and audit summary. Key rules currently enforced:

- `microservicePipeline` from `jenkins-library` must have been called
- `runTests` must have been called if a Test stage ran
- A build/image step (`buildApp` or `buildAndPushImage`) must have been called if a Build stage ran
- No audit anomalies (undeclared `exec()` outside pipeline steps)
- No unexpected network connections
- Libraries must be pinned to a full SHA, not a branch name
- SCM trigger required — manual builds refused

## Running tests

```bash
go test ./coordinator/
```

All state machine logic is covered. To add a new attestation rule, add a Cedar policy in `platform-cedar/policies/attest-gate.cedar` and a test in `coordinator/coordinator_test.go` that verifies the coordinator reaches Cedar with the right context.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `JENKINS_URL` | `http://jenkins-operator-http-jenkins.jenkins.svc.cluster.local:8080` | Jenkins base URL |
| `JENKINS_USER` | — (required) | Jenkins API user (jenkins-operator) |
| `JENKINS_TOKEN` | — (required) | Jenkins API token |
| `AUDIT_SERVICE_URL` | `http://platform-audit-service.platform.svc.cluster.local:8080` | Audit service for correlation reports |
| `CEDAR_SERVICE_URL` | `http://platform-cedar-sidecar.platform.svc.cluster.local:8080` | Cedar policy sidecar |
| `WEBHOOK_SECRET` | — (optional) | Bearer token checked on incoming webhook POST |
| `LISTEN_ADDR` | `:8080` | Listen address |

All cluster URLs are overridable for local dev:
```bash
JENKINS_USER=jenkins-operator JENKINS_TOKEN=... \
AUDIT_SERVICE_URL=http://localhost:9001 \
CEDAR_SERVICE_URL=http://localhost:9002 \
go run .
```

## Secrets

- `jenkins-operator-credentials-jenkins` (jenkins namespace) — provides `JENKINS_USER` and `JENKINS_TOKEN`
- `attest-webhook-secret` (platform namespace) — shared with Jenkins via `attest-webhook-secret` (jenkins namespace)

Both the platform and jenkins namespace SealedSecrets use the same plaintext token value. Seal with `kubeseal` — both have `REPLACE_ME` placeholders.

## Relationship to 02-attest-build-listener.groovy

`02` is the original self-contained Groovy listener with all decision logic inline. It remains in place as a fallback. `03-attest-coordinator-shim.groovy` is the replacement — load only one at a time. The marker strings differ (`PlatformAttestBuildListener-v1` vs `PlatformAttestCoordinatorShim-v1`) so both can coexist in Script Console without conflicting, but they would race to attest the same builds.

## Target architecture (capability-based model)

This coordinator is an interim solution. It solves the testability problem (moving decision logic out of untestable Groovy) but the async callback model is inherently complex — scans are separate jobs, callbacks come in separately, and giving developers inline feedback requires fighting against the asynchronous flow.

The target model is capability-based: **build pods start with no credentials and cannot reach anything directly. The platform controls all capability grants.**

```
Build pod (no credentials, Cilium blocks direct Harbor/registry access)
  │
  ├─ wants to push image → POST platform-credential-service with OIDC token
  │     checks: did microservicePipeline + runTests run? (audit service)
  │     yes → issues short-lived Harbor push token
  │     no  → 403 with reason, build fails inline
  │
  ├─ wants to sign image → POST platform-credential-service with OIDC token + image ref
  │     checks: scan passed? tests passed? library steps called?
  │     yes → service performs cosign sign (holds key, never exposes it)
  │     no  → 403 with reason, build fails inline
  │
  ├─ wants internet access → must route through MITM proxy
  │     all outbound traffic is inspected and logged
  │     unexpected destinations flagged as audit anomalies
  │
  └─ wants AWS creds at deploy → token service
        checks: cosign terraform scan attestation exists on the git commit SHA
                checkov passed, tfsec passed — signed by platform, not self-reported
        no clean attestation → 403, no credentials issued
```

Every gate follows the same pattern: before issuing any capability, verify a platform-signed attestation exists for the content. Scan results, test results, policy checks — none are self-reported. The platform runs the scan, the platform signs the result, and the platform verifies its own signature before granting access. A developer cannot produce a valid attestation without going through the platform.

A developer can write whatever they want in their Jenkinsfile. They still cannot push an image, sign it, reach the internet unobserved, run terraform, or deploy to AWS without the platform's permission. The library steps are not enforced by trust or convention — they are enforced by the fact that nothing works without them.

This scales across thousands of teams and thousands of pipelines without requiring opinionated pipeline templates. Opinionated pipelines require you to anticipate every edge case upfront — unusual build toolchains, non-standard test frameworks, multi-stage deploys, generated code — and teams with edge cases end up blocked or hacking around the template. Capability gates don't care what the pipeline looks like. Teams call the platform steps because they have no other way to get anything done, not because a template forced them to. Cedar policy evolves as edge cases reveal themselves — a deny surfaces the gap, the team comes to you, you learn what they actually need.

In this model the coordinator is unnecessary — the gate moves to capability-grant time. Developers physically cannot push or sign without going through the platform, and the platform only grants those capabilities if the right steps ran. Cilium `NetworkPolicy` is the hard wall that prevents direct registry access. The audit service and Tetragon become a second layer for anomaly detection, not the primary enforcement mechanism.

The `platform-token-service` already implements this pattern for AWS credentials. The next step is extending it (or adding a companion service) to cover Harbor push tokens and cosign signing.

### Why Tetragon makes this unbypassable

A developer could try to hide malicious behaviour inside a Go binary, a compiled test helper, a script called from a Makefile — anything that runs as a process inside the build pod. Opinionated pipeline templates cannot catch this because they only control the pipeline definition, not what executes inside it.

Tetragon operates at the kernel syscall level. Every `exec()` call in every process in the build pod is captured, regardless of how it was invoked or what language it came from. The audit service correlates those execs to the build via `PLATFORM_AUDIT_ID`. Cedar receives `anomaly_count` as context. Any exec that isn't attributable to a known library step is an anomaly. `anomaly_count > 0` → Cedar denies the capability grant. The developer cannot push, sign, or deploy.

They can try anything they like. The kernel sees it.

### Known gaps in the current implementation

**In-memory state** — the coordinator holds all pending build records in memory. A restart loses everything in flight. Builds that were waiting for scan callbacks will never be attested. This is acceptable as an interim solution but must be addressed before the coordinator is relied upon at scale. SQLite or an append log would be sufficient.

**Non-container workloads** — the coordinator handles container image builds only. The capability gate model applies equally to terraform modules, library packages, and other artifact types but the implementation for those does not exist yet. The git commit SHA is the natural content identifier for non-image attestations (`cosign attest-blob`).

### Additional gates (not yet implemented)

**Package registry publishing** — builds that publish npm, Maven, PyPI or other packages must go through the same gate as container pushes. Platform issues the publish token only after scan attestations pass. Without this, a developer can publish a malicious package directly.

**Kubernetes access from build pods** — build pods must not have a mounted kubeconfig or any RBAC that allows `kubectl apply`. All Kubernetes deployments must go through the release pipeline. A build pod with cluster write access bypasses every other gate.

**Dependency pulls** — builds pulling packages from the internet go through the MITM proxy which logs them, but a compromised dependency can still execute. A platform-controlled package mirror (Nexus/Artifactory) proxying approved registries closes this. Highest-impact supply chain attack vector.

**External image pulls** — build pods should not be able to pull images directly from Docker Hub or other external registries. Cilium should block external registry pulls and force everything through the Harbor proxy cache. Without this a developer can pull and run an arbitrary image inside the build.

## Web UI

The coordinator serves a dashboard at `GET /` showing active builds (waiting for evidence) and recent decisions (allowed/denied) with Cedar deny reasons surfaced prominently. Individual build detail is at `GET /builds?key={jobPath}%23{buildNumber}` showing the evidence timeline and full denial reason.

This is the primary way developers find out why their build was not attested. The dashboard auto-refreshes every 10 seconds.

## Opinionated pipelines vs capability gates

These solve different problems. Opinionated pipeline templates solve **consistency and onboarding** — reducing the number of ways teams can structure their builds, making support easier, lowering the cognitive overhead for new teams. That is a valid goal.

Capability gates solve **security**. They do not require opinionated pipelines. A team can have any pipeline structure they like and the security invariants still hold — they cannot push, sign, or deploy without platform permission regardless of what their Jenkinsfile looks like.

Conflating the two leads to using opinionated pipelines as a security control, which they are not. A developer can call the right steps in the wrong order, pass trivial inputs, or find gaps in the template. Capability gates cannot be gamed the same way because they verify what actually happened (via Tetragon and signed attestations) rather than trusting the pipeline definition.

At thousands of teams, opinionated pipelines also hit a scaling wall — every team with an edge case is either blocked or hacking around the template. Capability gates accommodate any pipeline shape because they only care about the output, not the structure.
