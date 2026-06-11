package coordinator

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	teamBuildRe            = regexp.MustCompile(`^teams/[^/]+/[^/]+/build$`)
	platformServiceBuildRe = regexp.MustCompile(`^platform/services/[^/]+/build$`)
	imageScanRe            = regexp.MustCompile(`^platform/[^/]+/scan$`)
	sourceScanRe           = regexp.MustCompile(`^platform/[^/]+/[^/]+/source-scan$`)
	platformServiceScanRe  = regexp.MustCompile(`^platform/services/[^/]+/scan$`)
)

// JenkinsClient triggers Jenkins jobs and schedules the attest job.
type JenkinsClient interface {
	TriggerBuild(jobPath string, params map[string]string) error
}

// CedarClient calls the Cedar policy sidecar.
type CedarClient interface {
	Authorize(rec *BuildRecord, summary *AuditSummary) (allowed bool, reason string, err error)
}

// AuditClient fetches the correlated audit summary from the audit service.
type AuditClient interface {
	GetSummary(auditID string) (*AuditSummary, error)
}

type Coordinator struct {
	mu        sync.Mutex
	records   map[string]*BuildRecord
	decisions *decisionLog
	jenkins   JenkinsClient
	cedar     CedarClient
	audit     AuditClient
	log       *log.Logger
}

func New(jenkins JenkinsClient, cedar CedarClient, audit AuditClient, logger *log.Logger) *Coordinator {
	return &Coordinator{
		records:   make(map[string]*BuildRecord),
		decisions: newDecisionLog(200),
		jenkins:   jenkins,
		cedar:     cedar,
		audit:     audit,
		log:       logger,
	}
}

// ActiveBuilds returns a snapshot of builds currently waiting for evidence.
func (c *Coordinator) ActiveBuilds() []BuildRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]BuildRecord, 0, len(c.records))
	for _, rec := range c.records {
		if !rec.Attested {
			out = append(out, *rec)
		}
	}
	return out
}

// RecentDecisions returns recent outcomes, newest first.
func (c *Coordinator) RecentDecisions() []DecisionRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.decisions.snapshot()
}

func (c *Coordinator) recordDecision(rec *BuildRecord, outcome DecisionOutcome, reason string) {
	d := DecisionRecord{
		Key:          buildKey(rec.JobPath, rec.BuildNumber),
		JobPath:      rec.JobPath,
		BuildNumber:  rec.BuildNumber,
		Branch:       rec.Branch,
		GitCommit:    rec.GitCommit,
		ImageRef:     rec.ImageRef,
		AuditID:      rec.AuditID,
		Outcome:      outcome,
		Reason:       reason,
		ReceivedAt:   rec.ReceivedAt,
		DecidedAt:    time.Now(),
		ImageScanAt:  rec.ImageScanAt,
		SourceScanAt: rec.SourceScanAt,
	}
	c.mu.Lock()
	c.decisions.add(d)
	c.mu.Unlock()
}

func (c *Coordinator) recordRefusal(p BuildEventPayload, reason string) {
	d := DecisionRecord{
		Key:         buildKey(p.JobPath, p.BuildNumber),
		JobPath:     p.JobPath,
		BuildNumber: p.BuildNumber,
		Branch:      p.Branch,
		GitCommit:   p.GitCommit,
		ImageRef:    p.ImageRef,
		AuditID:     p.AuditID,
		Outcome:     OutcomeRefused,
		Reason:      reason,
		ReceivedAt:  time.Now(),
		DecidedAt:   time.Now(),
	}
	c.mu.Lock()
	c.decisions.add(d)
	c.mu.Unlock()
}

// ClassifyJob returns the event type for a job path, or "" if not tracked.
func ClassifyJob(jobPath string) string {
	switch {
	case teamBuildRe.MatchString(jobPath):
		return "team-build"
	case platformServiceBuildRe.MatchString(jobPath):
		return "platform-service-build"
	case platformServiceScanRe.MatchString(jobPath):
		return "platform-service-scan"
	case imageScanRe.MatchString(jobPath):
		return "image-scan"
	case sourceScanRe.MatchString(jobPath):
		return "source-scan"
	default:
		return ""
	}
}

func (c *Coordinator) OnBuildComplete(p BuildEventPayload) {
	if p.Result != "SUCCESS" {
		c.logf("build %s #%d result=%s — skipping", p.JobPath, p.BuildNumber, p.Result)
		return
	}

	rec := recordFromPayload(p)

	if reason := rec.refusalReason(); reason != "" {
		c.logf("attestation REFUSED for %s #%d: %s", p.JobPath, p.BuildNumber, reason)
		c.recordRefusal(p, reason)
		return
	}

	key := buildKey(p.JobPath, p.BuildNumber)
	c.mu.Lock()
	c.records[key] = rec
	c.mu.Unlock()

	c.logf("build registered: %s #%d auditId=%s", p.JobPath, p.BuildNumber, p.AuditID)
	c.triggerScans(rec)
	c.tryAttest(key)
}

func (c *Coordinator) OnScanComplete(p BuildEventPayload) {
	if p.Result != "SUCCESS" {
		c.logf("scan %s #%d result=%s — skipping", p.JobPath, p.BuildNumber, p.Result)
		if p.UpstreamJob != "" && p.UpstreamBuild != 0 {
			key := buildKey(p.UpstreamJob, p.UpstreamBuild)
			c.mu.Lock()
			rec, ok := c.records[key]
			if ok {
				rec.Attested = true
			}
			c.mu.Unlock()
			if ok {
				c.recordDecision(rec, OutcomeScanFailed, fmt.Sprintf("image scan %s #%d result=%s", p.JobPath, p.BuildNumber, p.Result))
				c.mu.Lock()
				delete(c.records, key)
				c.mu.Unlock()
			}
		}
		return
	}
	if p.UpstreamJob == "" || p.UpstreamBuild == 0 {
		c.logf("scan %s #%d missing upstream params — skipping", p.JobPath, p.BuildNumber)
		return
	}

	key := buildKey(p.UpstreamJob, p.UpstreamBuild)
	c.mu.Lock()
	rec, ok := c.records[key]
	if ok {
		rec.ImageScanJob = p.JobPath
		rec.ImageScanBuild = p.BuildNumber
		rec.ImageScanAgeMs = time.Since(rec.ReceivedAt).Milliseconds()
		rec.ImageScanAt = time.Now()
	}
	c.mu.Unlock()

	if !ok {
		c.logf("image scan complete for %s #%d but no build record found — build may have been refused", p.UpstreamJob, p.UpstreamBuild)
		return
	}
	c.logf("image scan recorded: %s #%d → build %s #%d", p.JobPath, p.BuildNumber, p.UpstreamJob, p.UpstreamBuild)
	c.tryAttest(key)
}

func (c *Coordinator) OnSourceScanComplete(p BuildEventPayload) {
	if p.Result != "SUCCESS" {
		c.logf("source-scan %s #%d result=%s — skipping", p.JobPath, p.BuildNumber, p.Result)
		return
	}
	if p.UpstreamJob == "" || p.UpstreamBuild == 0 {
		c.logf("source-scan %s #%d missing upstream params — skipping", p.JobPath, p.BuildNumber)
		return
	}

	key := buildKey(p.UpstreamJob, p.UpstreamBuild)
	c.mu.Lock()
	rec, ok := c.records[key]
	if ok {
		rec.SourceScanJob = p.JobPath
		rec.SourceScanBuild = p.BuildNumber
		rec.SourceScanAt = time.Now()
		rec.JenkinsfileApproved = p.JenkinsfileApproved
	}
	c.mu.Unlock()

	if !ok {
		c.logf("source scan complete for %s #%d but no build record found", p.UpstreamJob, p.UpstreamBuild)
		return
	}
	c.logf("source scan recorded: %s #%d → build %s #%d", p.JobPath, p.BuildNumber, p.UpstreamJob, p.UpstreamBuild)
	c.tryAttest(key)
}

func (c *Coordinator) triggerScans(rec *BuildRecord) {
	params := map[string]string{
		"UPSTREAM_JOB":   rec.JobPath,
		"UPSTREAM_BUILD": fmt.Sprintf("%d", rec.BuildNumber),
		"GIT_URL":        rec.GitURL,
		"GIT_COMMIT":     rec.GitCommit,
	}

	scanJob := scanJobPath(rec)
	if err := c.jenkins.TriggerBuild(scanJob, params); err != nil {
		c.logf("ERROR triggering image scan %s: %v", scanJob, err)
	} else {
		c.logf("image scan triggered: %s for build %s #%d", scanJob, rec.JobPath, rec.BuildNumber)
	}

	sourceJob := sourceScanJobPath(rec)
	if err := c.jenkins.TriggerBuild(sourceJob, params); err != nil {
		c.logf("ERROR triggering source scan %s: %v", sourceJob, err)
	} else {
		c.logf("source scan triggered: %s for build %s #%d", sourceJob, rec.JobPath, rec.BuildNumber)
	}
}

func (c *Coordinator) tryAttest(key string) {
	c.mu.Lock()
	rec, ok := c.records[key]
	if !ok || rec.Attested || !rec.hasAllEvidence() {
		c.mu.Unlock()
		return
	}
	rec.Attested = true
	snap := *rec // safe snapshot; all reads below use snap, not the shared pointer
	c.mu.Unlock()

	c.logf("all evidence collected for %s #%d — fetching audit summary", snap.JobPath, snap.BuildNumber)

	summary, err := c.audit.GetSummary(snap.AuditID)
	if err != nil {
		c.logf("ERROR fetching audit summary for auditId=%s: %v — attestation blocked", snap.AuditID, err)
		c.mu.Lock()
		if mr, ok := c.records[key]; ok {
			mr.Attested = false
		}
		c.mu.Unlock()
		return
	}

	allowed, reason, err := c.cedar.Authorize(&snap, summary)
	if err != nil {
		c.logf("ERROR calling Cedar for %s #%d: %v — attestation blocked", snap.JobPath, snap.BuildNumber, err)
		c.mu.Lock()
		if mr, ok := c.records[key]; ok {
			mr.Attested = false
		}
		c.mu.Unlock()
		return
	}
	if !allowed {
		c.logf("Cedar DENIED attestation for %s #%d: %s", snap.JobPath, snap.BuildNumber, reason)
		c.recordDecision(&snap, OutcomeCedarDeny, reason)
		c.mu.Lock()
		delete(c.records, key)
		c.mu.Unlock()
		return
	}

	attestJob := attestJobPath(&snap)
	attestParams := buildAttestParams(&snap, summary)
	if err := c.jenkins.TriggerBuild(attestJob, attestParams); err != nil {
		c.logf("ERROR scheduling attest job %s for %s #%d: %v", attestJob, snap.JobPath, snap.BuildNumber, err)
		c.mu.Lock()
		if mr, ok := c.records[key]; ok {
			mr.Attested = false
		}
		c.mu.Unlock()
		return
	}

	c.recordDecision(&snap, OutcomeAttested, "")
	c.logf("attestation scheduled: %s for %s #%d | auditId=%s", attestJob, snap.JobPath, snap.BuildNumber, snap.AuditID)
	c.mu.Lock()
	delete(c.records, key)
	c.mu.Unlock()
}

// --- helpers ------------------------------------------------------------

func (r *BuildRecord) hasAllEvidence() bool {
	return r.ImageScanJob != "" && r.SourceScanJob != ""
}

func (r *BuildRecord) refusalReason() string {
	if r.AuditID == "" {
		return "no PLATFORM_AUDIT_ID — audit-graph-listener may not be loaded"
	}
	if !r.SCMTriggered {
		return "build was not triggered by SCM — manual builds are not eligible"
	}
	if r.GitCommit == "" {
		return "could not determine GIT_COMMIT from build data"
	}
	if !r.HasArtifacts {
		return "artifacts.json was not archived"
	}
	if r.JUnitTotal == 0 {
		return "no JUnit test results recorded"
	}
	if r.JUnitFailed > 0 {
		return fmt.Sprintf("%d test failure(s)", r.JUnitFailed)
	}
	return ""
}

func recordFromPayload(p BuildEventPayload) *BuildRecord {
	rec := &BuildRecord{
		JobPath:      p.JobPath,
		BuildNumber:  p.BuildNumber,
		AuditID:      p.AuditID,
		GitCommit:    p.GitCommit,
		GitURL:       p.GitURL,
		Branch:       p.Branch,
		JUnitTotal:   p.JUnitTotal,
		JUnitFailed:  p.JUnitFailed,
		LineCoverage: p.LineCoverage,
		CovThreshold: p.CovThreshold,
		HasArtifacts: p.HasArtifacts,
		SCMTriggered: p.SCMTriggered,
		ImageRef:     p.ImageRef,
		Stages:       p.Stages,
		Libraries:    p.Libraries,
		LibrarySteps: p.LibrarySteps,
		CustomStepCount: p.CustomStepCount,
		StrictPipeline:  p.StrictPipeline,
		ReceivedAt:   time.Now(),
	}
	parts := strings.Split(p.JobPath, "/")
	if strings.HasPrefix(p.JobPath, "teams/") && len(parts) >= 2 {
		rec.ServiceType = "team"
		rec.TeamSlug = parts[1]
	} else if strings.HasPrefix(p.JobPath, "platform/services/") && len(parts) >= 3 {
		rec.ServiceType = "platform-service"
		rec.ServiceSlug = parts[2]
	}
	return rec
}

func buildKey(jobPath string, buildNumber int) string {
	return fmt.Sprintf("%s#%d", jobPath, buildNumber)
}

// BuildKey returns the coordinator's canonical key for a build. Used by the UI.
func BuildKey(jobPath string, buildNumber int) string {
	return buildKey(jobPath, buildNumber)
}

func scanJobPath(rec *BuildRecord) string {
	if rec.ServiceType == "platform-service" {
		return fmt.Sprintf("platform/services/%s/scan", rec.ServiceSlug)
	}
	return fmt.Sprintf("platform/%s/scan", rec.TeamSlug)
}

func sourceScanJobPath(rec *BuildRecord) string {
	parts := strings.Split(rec.JobPath, "/")
	if rec.ServiceType == "platform-service" && len(parts) >= 3 {
		return fmt.Sprintf("platform/services/%s/source-scan", rec.ServiceSlug)
	}
	if len(parts) >= 3 {
		return fmt.Sprintf("platform/%s/%s/source-scan", parts[1], parts[2])
	}
	return ""
}

func attestJobPath(rec *BuildRecord) string {
	if rec.ServiceType == "platform-service" {
		return fmt.Sprintf("platform/services/%s/attest", rec.ServiceSlug)
	}
	return fmt.Sprintf("platform/%s/attest", rec.TeamSlug)
}

func (c *Coordinator) logf(format string, args ...any) {
	c.log.Printf("[coordinator] "+format, args...)
}
