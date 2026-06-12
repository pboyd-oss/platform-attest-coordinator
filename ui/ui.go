package ui

import (
	_ "embed"

	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pboyd-oss/platform-attest-coordinator/coordinator"
)

type coordinatorReader interface {
	ActiveBuilds() []coordinator.BuildRecord
	RecentDecisions() []coordinator.DecisionRecord
}

type Handler struct {
	coord coordinatorReader
}

func NewHandler(coord coordinatorReader) *Handler {
	return &Handler{coord: coord}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/" || r.URL.Path == "/ui":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(spaHTML))
	case r.URL.Path == "/suite.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(suiteCSS)
	case r.URL.Path == "/api/builds" && r.Method == http.MethodGet:
		h.apiBuilds(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/builds/") && r.Method == http.MethodGet:
		h.apiBuildDetail(w, r)
	default:
		http.NotFound(w, r)
	}
}

type apiBuildsResponse struct {
	Active    []buildJSON    `json:"active"`
	Decisions []decisionJSON `json:"decisions"`
}

type buildJSON struct {
	Key             string                `json:"key"`
	JobPath         string                `json:"jobPath"`
	BuildNumber     int                   `json:"buildNumber"`
	Branch          string                `json:"branch"`
	GitCommit       string                `json:"gitCommit"`
	AuditID         string                `json:"auditId"`
	ImageRef        string                `json:"imageRef"`
	JUnitTotal      int                   `json:"junitTotal"`
	JUnitFailed     int                   `json:"junitFailed"`
	LineCoverage    float64               `json:"lineCoverage"`
	CovThreshold    int                   `json:"covThreshold"`
	Stages          []coordinator.Stage   `json:"stages"`
	Libraries       []coordinator.Library `json:"libraries"`
	LibrarySteps    []string              `json:"librarySteps"`
	ImageScanJob    string                `json:"imageScanJob"`
	ImageScanBuild  int                   `json:"imageScanBuild"`
	SourceScanJob   string                `json:"sourceScanJob"`
	SourceScanBuild int                   `json:"sourceScanBuild"`
	ImageScanAt     *time.Time            `json:"imageScanAt"`
	SourceScanAt    *time.Time            `json:"sourceScanAt"`
	ReceivedAt      time.Time             `json:"receivedAt"`
}

type decisionJSON struct {
	Key          string     `json:"key"`
	JobPath      string     `json:"jobPath"`
	BuildNumber  int        `json:"buildNumber"`
	Branch       string     `json:"branch"`
	GitCommit    string     `json:"gitCommit"`
	ImageRef     string     `json:"imageRef"`
	AuditID      string     `json:"auditId"`
	Outcome      string     `json:"outcome"`
	Reason       string     `json:"reason"`
	ReceivedAt   time.Time  `json:"receivedAt"`
	DecidedAt    time.Time  `json:"decidedAt"`
	ImageScanAt  *time.Time `json:"imageScanAt"`
	SourceScanAt *time.Time `json:"sourceScanAt"`
}

func (h *Handler) apiBuilds(w http.ResponseWriter, r *http.Request) {
	active := h.coord.ActiveBuilds()
	decisions := h.coord.RecentDecisions()

	resp := apiBuildsResponse{
		Active:    make([]buildJSON, len(active)),
		Decisions: make([]decisionJSON, len(decisions)),
	}
	for i, b := range active {
		resp.Active[i] = toBuildJSON(b)
	}
	for i, d := range decisions {
		resp.Decisions[i] = toDecisionJSON(d)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type apiBuildDetailResponse struct {
	Active   *buildJSON    `json:"active"`
	Decision *decisionJSON `json:"decision"`
}

func (h *Handler) apiBuildDetail(w http.ResponseWriter, r *http.Request) {
	rawKey := strings.TrimPrefix(r.URL.Path, "/api/builds/")
	key, err := url.PathUnescape(rawKey)
	if err != nil {
		key = rawKey
	}

	decisions := h.coord.RecentDecisions()
	active := h.coord.ActiveBuilds()

	resp := apiBuildDetailResponse{}
	for _, d := range decisions {
		if d.Key == key {
			j := toDecisionJSON(d)
			resp.Decision = &j
			break
		}
	}
	if resp.Decision == nil {
		for _, b := range active {
			if coordinator.BuildKey(b.JobPath, b.BuildNumber) == key {
				j := toBuildJSON(b)
				resp.Active = &j
				break
			}
		}
	}

	if resp.Decision == nil && resp.Active == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func toBuildJSON(b coordinator.BuildRecord) buildJSON {
	return buildJSON{
		Key:             coordinator.BuildKey(b.JobPath, b.BuildNumber),
		JobPath:         b.JobPath,
		BuildNumber:     b.BuildNumber,
		Branch:          b.Branch,
		GitCommit:       b.GitCommit,
		AuditID:         b.AuditID,
		ImageRef:        b.ImageRef,
		JUnitTotal:      b.JUnitTotal,
		JUnitFailed:     b.JUnitFailed,
		LineCoverage:    b.LineCoverage,
		CovThreshold:    b.CovThreshold,
		Stages:          b.Stages,
		Libraries:       b.Libraries,
		LibrarySteps:    b.LibrarySteps,
		ImageScanJob:    b.ImageScanJob,
		ImageScanBuild:  b.ImageScanBuild,
		SourceScanJob:   b.SourceScanJob,
		SourceScanBuild: b.SourceScanBuild,
		ImageScanAt:     optTime(b.ImageScanAt),
		SourceScanAt:    optTime(b.SourceScanAt),
		ReceivedAt:      b.ReceivedAt,
	}
}

func toDecisionJSON(d coordinator.DecisionRecord) decisionJSON {
	return decisionJSON{
		Key:          d.Key,
		JobPath:      d.JobPath,
		BuildNumber:  d.BuildNumber,
		Branch:       d.Branch,
		GitCommit:    d.GitCommit,
		ImageRef:     d.ImageRef,
		AuditID:      d.AuditID,
		Outcome:      string(d.Outcome),
		Reason:       d.Reason,
		ReceivedAt:   d.ReceivedAt,
		DecidedAt:    d.DecidedAt,
		ImageScanAt:  optTime(d.ImageScanAt),
		SourceScanAt: optTime(d.SourceScanAt),
	}
}

//go:embed index.html
var spaHTML []byte

//go:embed suite.css
var suiteCSS []byte
