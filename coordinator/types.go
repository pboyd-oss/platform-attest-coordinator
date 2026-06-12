package coordinator

import "time"

type BuildEventPayload struct {
	JobPath             string    `json:"jobPath"`
	BuildNumber         int       `json:"buildNumber"`
	Result              string    `json:"result"`
	AuditID             string    `json:"auditId"`
	UpstreamJob         string    `json:"upstreamJob"`
	UpstreamBuild       int       `json:"upstreamBuild"`
	GitCommit           string    `json:"gitCommit"`
	GitURL              string    `json:"gitUrl"`
	Branch              string    `json:"branch"`
	JUnitTotal          int       `json:"junitTotal"`
	JUnitFailed         int       `json:"junitFailed"`
	LineCoverage        float64   `json:"lineCoverage"` // -1 if not recorded
	CovThreshold        int       `json:"covThreshold"`
	HasArtifacts        bool      `json:"hasArtifacts"`
	SCMTriggered        bool      `json:"scmTriggered"`
	ImageRef            string    `json:"imageRef"`
	Stages              []Stage   `json:"stages"`
	Libraries           []Library `json:"libraries"`
	LibrarySteps        []string  `json:"librarySteps"`
	CustomStepCount     int       `json:"customStepCount"`
	StrictPipeline      bool      `json:"strictPipeline"`
	JenkinsfileApproved bool      `json:"jenkinsfileApproved"`
}

type Stage struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Library struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

type BuildRecord struct {
	JobPath             string
	BuildNumber         int
	AuditID             string
	GitCommit           string
	GitURL              string
	Branch              string
	JUnitTotal          int
	JUnitFailed         int
	LineCoverage        float64
	CovThreshold        int
	HasArtifacts        bool
	SCMTriggered        bool
	ImageRef            string
	Stages              []Stage
	Libraries           []Library
	LibrarySteps        []string
	CustomStepCount     int
	StrictPipeline      bool
	JenkinsfileApproved bool

	ServiceType string // "team" or "platform-service"
	TeamSlug    string
	ServiceSlug string

	// set when scan callbacks arrive
	ImageScanJob    string
	ImageScanBuild  int
	ImageScanAgeMs  int64
	ImageScanAt     time.Time
	SourceScanJob   string
	SourceScanBuild int
	SourceScanAt    time.Time

	Attested   bool
	ReceivedAt time.Time
}

type AuditSummary struct {
	Digest                 string `json:"digest"`
	AnomalyCount           int64  `json:"anomaly_count"`
	UnexpectedNetworkCount int64  `json:"unexpected_network_count"`
	// ExecsObserved=total Tetragon execs correlated (0 with a valid auditId = witness dark).
	// SandboxViolations=GROOVY_DENY count from PlatformGroovyInterceptor.
	ExecsObserved     int64 `json:"total_execs"`
	SandboxViolations int64 `json:"sandbox_violation_count"`
	// GroovyRuntimeCalls=GROOVY_RUNTIME events from PlatformGroovyRuntimeTracer; 0 with a
	// real build = the tracer was off/bypassed/shadowed.
	GroovyRuntimeCalls int64 `json:"total_groovy_runtime_calls"`
	// Classloader-provenance attribution computed independently by the audit-service.
	// CalledLibrarySteps = unique "lib::step" for steps proven to come from a trusted
	// library; CustomStepCount = leaf steps from team code; CustomSyscallSiteCount =
	// team-origin Groovy call sites reaching a syscall gateway. The Cedar gate keys on
	// these (not the Jenkins shim's self-reported tallies).
	CalledLibrarySteps     []string `json:"called_library_steps"`
	CustomStepCount        int64    `json:"custom_step_count"`
	CustomSyscallSiteCount int64    `json:"custom_syscall_site_count"`
	// LibraryDigests = library name -> SHA-256 of the code that actually loaded on disk
	// (independent of the version the build declares). Compared against the platform's
	// expected-digest registry to detect trusted-library drift/tampering.
	LibraryDigests map[string]string `json:"library_digests"`
}
