package coordinator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var pinnedSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

type HTTPCedarClient struct {
	url    string
	client *http.Client
	// expectedLibDigests = platform-controlled registry of library name -> approved SHA-256
	// of its loaded code. The source of truth for the library-integrity gate; a build cannot
	// influence it. Empty = gate off (opt-in, like strict).
	expectedLibDigests map[string]string
}

func NewCedarClient(url, libraryDigestsPath string) *HTTPCedarClient {
	return &HTTPCedarClient{
		url:                strings.TrimRight(url, "/"),
		client:             &http.Client{Timeout: 5 * time.Second},
		expectedLibDigests: loadLibraryDigests(libraryDigestsPath),
	}
}

// loadLibraryDigests reads a JSON map {libraryName: expectedSha256} from a
// platform-controlled file (e.g. a GitOps ConfigMap). Returns empty (gate disabled) if the
// path is unset or unreadable — opt-in, so a missing mount does not brick all attestation.
func loadLibraryDigests(path string) map[string]string {
	if path == "" {
		return map[string]string{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func (c *HTTPCedarClient) Authorize(rec *BuildRecord, summary *AuditSummary) (bool, string, error) {
	completedStages := make([]string, 0)
	for _, s := range rec.Stages {
		if s.Status == "SUCCESS" {
			completedStages = append(completedStages, s.Name)
		}
	}

	hasUnpinned := false
	for _, lib := range rec.Libraries {
		if !pinnedSHARe.MatchString(lib.SHA) {
			hasUnpinned = true
			break
		}
	}

	cov := rec.LineCoverage
	if cov < 0 {
		cov = 0
	}
	scanAgeSeconds := int64(0)
	if rec.ImageScanAgeMs > 0 {
		scanAgeSeconds = rec.ImageScanAgeMs / 1000
	}

	// Gate-critical attribution comes from the audit-service (independent classloader
	// witness), NOT rec.* (the Jenkins shim's self-reported payload). A nil set marshals to
	// JSON null and would error attest-gate's calledLibrarySteps.contains(...) evaluation;
	// coalesce to an empty set so the gate fails CLOSED (no proven library steps => deny)
	// when the witness did not compute them.
	calledLibrarySteps := summary.CalledLibrarySteps
	if calledLibrarySteps == nil {
		calledLibrarySteps = []string{}
	}

	// Library-integrity: for every loaded library we hold an EXPECTED digest for, the digest
	// the audit-service observed actually loading (a hash of the on-disk code — not the
	// build's self-reported version) must be present AND match. A miss (drift, tampering, or
	// no observed digest) increments the mismatch count. Libraries with no expected entry are
	// not checked, so you pin only the ones that matter (e.g. jenkins-library).
	libraryDigestMismatch := int64(0)
	if len(c.expectedLibDigests) > 0 {
		for _, lib := range rec.Libraries {
			want, ok := c.expectedLibDigests[lib.Name]
			if !ok {
				continue
			}
			if got := summary.LibraryDigests[lib.Name]; got == "" || !strings.EqualFold(got, want) {
				libraryDigestMismatch++
			}
		}
	}

	ctx := map[string]any{
		"testsRun":                    int64(rec.JUnitTotal),
		"testsFailed":                 int64(rec.JUnitFailed),
		"lineCoveragePct":             int64(cov),
		"coverageThreshold":           int64(rec.CovThreshold),
		"hasArtifactsJson":            rec.HasArtifacts,
		"hasScanAttestation":          rec.ImageScanJob != "",
		"scanAgeSeconds":              scanAgeSeconds,
		"completedStages":             completedStages,
		"calledLibrarySteps":          calledLibrarySteps,
		"auditAnomalyCount":           summary.AnomalyCount,
		"auditUnexpectedNetworkCount": summary.UnexpectedNetworkCount,
		"hasUnpinnedLibraries":        hasUnpinned,
		"customStepCount":             summary.CustomStepCount,
		"customSyscallSiteCount":      summary.CustomSyscallSiteCount,
		"libraryDigestMismatchCount":  libraryDigestMismatch,
		"jenkinsfileApproved":         rec.JenkinsfileApproved,
		"sandboxViolationCount":       summary.SandboxViolations,
		"tetragonExecsObserved":       summary.ExecsObserved,
		"groovyRuntimeCalls":          summary.GroovyRuntimeCalls,
	}

	entities := buildEntities(rec)

	imageRef := rec.ImageRef
	if imageRef == "" {
		imageRef = "unknown"
	}

	body, err := json.Marshal(map[string]any{
		"principal": fmt.Sprintf(`TuxGrid::Pipeline::"%s"`, rec.JobPath),
		"action":    `TuxGrid::Action::"Attest"`,
		"resource":  fmt.Sprintf(`TuxGrid::Image::"%s"`, imageRef),
		"entities":  entities,
		"context":   ctx,
	})
	if err != nil {
		return false, "", err
	}

	resp, err := c.client.Post(c.url+"/authorize", "application/json", bytes.NewReader(body))
	if err != nil {
		return false, "", fmt.Errorf("Cedar unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Decision string   `json:"decision"`
		Reasons  []string `json:"reasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "", fmt.Errorf("Cedar response unparseable: %w", err)
	}

	if result.Decision != "ALLOW" {
		return false, strings.Join(result.Reasons, "; "), nil
	}
	return true, "", nil
}

func buildEntities(rec *BuildRecord) []map[string]any {
	declaredBuild, declaredTest := false, false
	for _, s := range rec.Stages {
		switch s.Name {
		case "Build":
			declaredBuild = true
		case "Test":
			declaredTest = true
		}
	}

	pipelineAttrs := map[string]any{
		"jobPath":        rec.JobPath,
		"branch":         rec.Branch,
		"triggeredBySCM": rec.SCMTriggered,
		"hasAuditId":     rec.AuditID != "",
		"declaredBuild":  declaredBuild,
		"declaredTest":   declaredTest,
		"strictPipeline": rec.StrictPipeline,
	}

	if rec.ServiceType == "platform-service" {
		return []map[string]any{
			{
				"uid":     map[string]any{"type": "TuxGrid::Namespace", "id": "platform"},
				"attrs":   map[string]any{"tier": "platform"},
				"parents": []any{},
			},
			{
				"uid":     map[string]any{"type": "TuxGrid::Pipeline", "id": rec.JobPath},
				"attrs":   pipelineAttrs,
				"parents": []any{map[string]any{"type": "TuxGrid::Namespace", "id": "platform"}},
			},
		}
	}

	return []map[string]any{
		{
			"uid":     map[string]any{"type": "TuxGrid::Namespace", "id": "development"},
			"attrs":   map[string]any{"tier": "development"},
			"parents": []any{},
		},
		{
			"uid": map[string]any{"type": "TuxGrid::Team", "id": rec.TeamSlug},
			"attrs": map[string]any{
				"slug":              rec.TeamSlug,
				"coverageThreshold": int64(rec.CovThreshold),
			},
			"parents": []any{map[string]any{"type": "TuxGrid::Namespace", "id": "development"}},
		},
		{
			"uid":     map[string]any{"type": "TuxGrid::Pipeline", "id": rec.JobPath},
			"attrs":   pipelineAttrs,
			"parents": []any{map[string]any{"type": "TuxGrid::Team", "id": rec.TeamSlug}},
		},
	}
}
