package coordinator

import (
	"fmt"
	"log"
	"os"
	"testing"
)

// --- fakes ------------------------------------------------------------------

type fakeJenkins struct {
	triggered []string
	failPaths map[string]bool
}

func (f *fakeJenkins) TriggerBuild(jobPath string, _ map[string]string) error {
	if f.failPaths[jobPath] {
		return fmt.Errorf("trigger failed")
	}
	f.triggered = append(f.triggered, jobPath)
	return nil
}

func (f *fakeJenkins) triggered_(path string) bool {
	for _, p := range f.triggered {
		if p == path {
			return true
		}
	}
	return false
}

type fakeCedar struct {
	allow  bool
	reason string
	err    error
}

func (f *fakeCedar) Authorize(_ *BuildRecord, _ *AuditSummary) (bool, string, error) {
	return f.allow, f.reason, f.err
}

type fakeAudit struct {
	summary *AuditSummary
	err     error
}

func (f *fakeAudit) GetSummary(_ string) (*AuditSummary, error) {
	return f.summary, f.err
}

func newCoordinator(j JenkinsClient, c CedarClient, a AuditClient) *Coordinator {
	return New(j, c, a, log.New(os.Stdout, "", 0))
}

func goodSummary() *AuditSummary {
	return &AuditSummary{Digest: "abc123", AnomalyCount: 0, UnexpectedNetworkCount: 0}
}

func goodBuildPayload() BuildEventPayload {
	return BuildEventPayload{
		JobPath:      "teams/myteam/myrepo/build",
		BuildNumber:  42,
		Result:       "SUCCESS",
		AuditID:      "audit-teams-myteam-myrepo-build-42-deadbeef",
		GitCommit:    "aabbccddee1122334455667788990011aabbccdd",
		GitURL:       "https://gitea.tuxgrid.com/myteam/myrepo.git",
		Branch:       "main",
		JUnitTotal:   20,
		JUnitFailed:  0,
		LineCoverage: 85.0,
		CovThreshold: 70,
		HasArtifacts: true,
		SCMTriggered: true,
		ImageRef:     "harbor.tuxgrid.com/teams/myteam/myrepo@sha256:abc",
		Stages:       []Stage{{Name: "Build", Status: "SUCCESS"}, {Name: "Test", Status: "SUCCESS"}},
		Libraries:    []Library{{Name: "jenkins-library", SHA: "aabbccddee1122334455667788990011aabbccdd"}},
		LibrarySteps: []string{"jenkins-library::microservicePipeline", "jenkins-library::runTests", "jenkins-library::buildApp"},
	}
}

// --- ClassifyJob ------------------------------------------------------------

func TestClassifyJob(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"teams/myteam/myrepo/build", "team-build"},
		{"platform/services/audit-service/build", "platform-service-build"},
		{"platform/myteam/scan", "image-scan"},
		{"platform/myteam/myrepo/source-scan", "source-scan"},
		{"platform/services/audit-service/scan", "platform-service-scan"},
		{"seed/master-seed", ""},
		{"teams/myteam/myrepo/deploy", ""},
	}
	for _, tc := range cases {
		if got := ClassifyJob(tc.path); got != tc.want {
			t.Errorf("ClassifyJob(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// --- refusalReason ----------------------------------------------------------

func TestRefusalReason_NoAuditID(t *testing.T) {
	p := goodBuildPayload()
	p.AuditID = ""
	rec := recordFromPayload(p)
	if rec.refusalReason() == "" {
		t.Fatal("expected refusal for missing audit ID")
	}
}

func TestRefusalReason_ManualTrigger(t *testing.T) {
	p := goodBuildPayload()
	p.SCMTriggered = false
	rec := recordFromPayload(p)
	if rec.refusalReason() == "" {
		t.Fatal("expected refusal for manual trigger")
	}
}

func TestRefusalReason_NoGitCommit(t *testing.T) {
	p := goodBuildPayload()
	p.GitCommit = ""
	rec := recordFromPayload(p)
	if rec.refusalReason() == "" {
		t.Fatal("expected refusal for missing git commit")
	}
}

func TestRefusalReason_NoArtifacts(t *testing.T) {
	p := goodBuildPayload()
	p.HasArtifacts = false
	rec := recordFromPayload(p)
	if rec.refusalReason() == "" {
		t.Fatal("expected refusal for no artifacts.json")
	}
}

func TestRefusalReason_NoJUnit(t *testing.T) {
	p := goodBuildPayload()
	p.JUnitTotal = 0
	rec := recordFromPayload(p)
	if rec.refusalReason() == "" {
		t.Fatal("expected refusal for no JUnit results")
	}
}

func TestRefusalReason_FailingTests(t *testing.T) {
	p := goodBuildPayload()
	p.JUnitFailed = 3
	rec := recordFromPayload(p)
	if rec.refusalReason() == "" {
		t.Fatal("expected refusal for failing tests")
	}
}

func TestRefusalReason_GoodBuild(t *testing.T) {
	rec := recordFromPayload(goodBuildPayload())
	if reason := rec.refusalReason(); reason != "" {
		t.Fatalf("expected no refusal, got: %s", reason)
	}
}

// --- OnBuildComplete --------------------------------------------------------

func TestOnBuildComplete_FailedBuild_NotRegistered(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})
	p := goodBuildPayload()
	p.Result = "FAILURE"
	c.OnBuildComplete(p)
	if len(j.triggered) != 0 {
		t.Fatal("failed build should not trigger any scans")
	}
}

func TestOnBuildComplete_RefusedBuild_NoScansTriggered(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})
	p := goodBuildPayload()
	p.AuditID = ""
	c.OnBuildComplete(p)
	if len(j.triggered) != 0 {
		t.Fatal("refused build should not trigger scans")
	}
}

func TestOnBuildComplete_GoodBuild_TriggersScans(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})
	c.OnBuildComplete(goodBuildPayload())
	if !j.triggered_("platform/myteam/scan") {
		t.Error("expected image scan to be triggered at platform/myteam/scan")
	}
	if !j.triggered_("platform/myteam/myrepo/source-scan") {
		t.Error("expected source scan to be triggered")
	}
}

func TestOnBuildComplete_PlatformService_CorrectScanPaths(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})
	p := goodBuildPayload()
	p.JobPath = "platform/services/audit-service/build"
	c.OnBuildComplete(p)
	if !j.triggered_("platform/services/audit-service/scan") {
		t.Error("expected platform service image scan at platform/services/audit-service/scan")
	}
	if !j.triggered_("platform/services/audit-service/source-scan") {
		t.Error("expected platform service source scan")
	}
}

// --- full attestation flow --------------------------------------------------

func TestFullFlow_AttestsWhenBothScansArrive(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})

	c.OnBuildComplete(goodBuildPayload())

	c.OnScanComplete(BuildEventPayload{
		JobPath:       "platform/myteam/scan",
		BuildNumber:   1,
		Result:        "SUCCESS",
		UpstreamJob:   "teams/myteam/myrepo/build",
		UpstreamBuild: 42,
	})
	if j.triggered_("platform/myteam/attest") {
		t.Fatal("should not attest before source scan arrives")
	}

	c.OnSourceScanComplete(BuildEventPayload{
		JobPath:       "platform/myteam/myrepo/source-scan",
		BuildNumber:   1,
		Result:        "SUCCESS",
		UpstreamJob:   "teams/myteam/myrepo/build",
		UpstreamBuild: 42,
	})
	if !j.triggered_("platform/myteam/attest") {
		t.Fatal("expected attest job to be triggered after both scans")
	}
}

func TestFullFlow_CedarDeny_NoAttest(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: false, reason: "runTests not called"}, &fakeAudit{summary: goodSummary()})

	c.OnBuildComplete(goodBuildPayload())
	c.OnScanComplete(BuildEventPayload{JobPath: "platform/myteam/scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42})
	c.OnSourceScanComplete(BuildEventPayload{JobPath: "platform/myteam/myrepo/source-scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42})

	if j.triggered_("platform/myteam/attest") {
		t.Fatal("Cedar denied — attest should not be triggered")
	}
}

func TestFullFlow_AuditServiceDown_NoAttest(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{err: fmt.Errorf("connection refused")})

	c.OnBuildComplete(goodBuildPayload())
	c.OnScanComplete(BuildEventPayload{JobPath: "platform/myteam/scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42})
	c.OnSourceScanComplete(BuildEventPayload{JobPath: "platform/myteam/myrepo/source-scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42})

	if j.triggered_("platform/myteam/attest") {
		t.Fatal("audit service down — attest should be blocked")
	}
}

func TestFullFlow_FailedScan_NoAttest(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})

	c.OnBuildComplete(goodBuildPayload())
	c.OnScanComplete(BuildEventPayload{JobPath: "platform/myteam/scan", BuildNumber: 1, Result: "FAILURE", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42})
	c.OnSourceScanComplete(BuildEventPayload{JobPath: "platform/myteam/myrepo/source-scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42})

	if j.triggered_("platform/myteam/attest") {
		t.Fatal("failed image scan — attest should not be triggered")
	}
}

func TestFullFlow_NoDoubleAttest(t *testing.T) {
	j := &fakeJenkins{}
	c := newCoordinator(j, &fakeCedar{allow: true}, &fakeAudit{summary: goodSummary()})

	c.OnBuildComplete(goodBuildPayload())
	scanEvent := BuildEventPayload{JobPath: "platform/myteam/scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42}
	sourceScanEvent := BuildEventPayload{JobPath: "platform/myteam/myrepo/source-scan", BuildNumber: 1, Result: "SUCCESS", UpstreamJob: "teams/myteam/myrepo/build", UpstreamBuild: 42}

	c.OnScanComplete(scanEvent)
	c.OnSourceScanComplete(sourceScanEvent)
	// second delivery of same events
	c.OnScanComplete(scanEvent)
	c.OnSourceScanComplete(sourceScanEvent)

	count := 0
	for _, p := range j.triggered {
		if p == "platform/myteam/attest" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected attest triggered exactly once, got %d", count)
	}
}

// --- scan job path derivation -----------------------------------------------

func TestScanJobPath_Team(t *testing.T) {
	rec := recordFromPayload(goodBuildPayload())
	if got := scanJobPath(rec); got != "platform/myteam/scan" {
		t.Errorf("got %q", got)
	}
}

func TestScanJobPath_PlatformService(t *testing.T) {
	p := goodBuildPayload()
	p.JobPath = "platform/services/audit-service/build"
	rec := recordFromPayload(p)
	if got := scanJobPath(rec); got != "platform/services/audit-service/scan" {
		t.Errorf("got %q", got)
	}
}

func TestAttestJobPath_Team(t *testing.T) {
	rec := recordFromPayload(goodBuildPayload())
	if got := attestJobPath(rec); got != "platform/myteam/attest" {
		t.Errorf("got %q", got)
	}
}
