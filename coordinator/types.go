package coordinator

import "time"

type BuildEventPayload struct {
	JobPath       string    `json:"jobPath"`
	BuildNumber   int       `json:"buildNumber"`
	Result        string    `json:"result"`
	AuditID       string    `json:"auditId"`
	UpstreamJob   string    `json:"upstreamJob"`
	UpstreamBuild int       `json:"upstreamBuild"`
	GitCommit     string    `json:"gitCommit"`
	GitURL        string    `json:"gitUrl"`
	Branch        string    `json:"branch"`
	JUnitTotal    int       `json:"junitTotal"`
	JUnitFailed   int       `json:"junitFailed"`
	LineCoverage  float64   `json:"lineCoverage"`  // -1 if not recorded
	CovThreshold  int       `json:"covThreshold"`
	HasArtifacts  bool      `json:"hasArtifacts"`
	SCMTriggered  bool      `json:"scmTriggered"`
	ImageRef      string    `json:"imageRef"`
	Stages        []Stage   `json:"stages"`
	Libraries     []Library `json:"libraries"`
	LibrarySteps  []string  `json:"librarySteps"`
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
	JobPath      string
	BuildNumber  int
	AuditID      string
	GitCommit    string
	GitURL       string
	Branch       string
	JUnitTotal   int
	JUnitFailed  int
	LineCoverage float64
	CovThreshold int
	HasArtifacts bool
	SCMTriggered bool
	ImageRef     string
	Stages       []Stage
	Libraries    []Library
	LibrarySteps []string

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
}
