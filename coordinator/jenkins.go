package coordinator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPJenkinsClient struct {
	baseURL string
	user    string
	token   string
	client  *http.Client
}

func NewJenkinsClient(baseURL, user, token string) *HTTPJenkinsClient {
	return &HTTPJenkinsClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		user:    user,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (j *HTTPJenkinsClient) TriggerBuild(jobPath string, params map[string]string) error {
	endpoint := j.baseURL + "/job/" + strings.Join(strings.Split(jobPath, "/"), "/job/") + "/buildWithParameters"

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(j.user, j.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Jenkins returned HTTP %d for %s", resp.StatusCode, endpoint)
	}
	return nil
}

// buildAttestParams constructs the parameter map for the attest job.
func buildAttestParams(rec *BuildRecord, summary *AuditSummary) map[string]string {
	stagesJSON, _ := json.Marshal(rec.Stages)
	libsJSON, _ := json.Marshal(rec.Libraries)

	scanRef := ""
	if rec.ImageScanJob != "" {
		scanRef = fmt.Sprintf("%s#%d", rec.ImageScanJob, rec.ImageScanBuild)
	}

	cov := rec.LineCoverage
	if cov < 0 {
		cov = 0
	}

	return map[string]string{
		"UPSTREAM_JOB":              rec.JobPath,
		"UPSTREAM_BUILD":            fmt.Sprintf("%d", rec.BuildNumber),
		"PLATFORM_AUDIT_ID":         rec.AuditID,
		"PLATFORM_AUDIT_LOG_REF":    fmt.Sprintf("%s#%d/artifact/audit-log.json", rec.JobPath, rec.BuildNumber),
		"PLATFORM_AUDIT_LOG_DIGEST": summary.Digest,
		"PLATFORM_TESTS_COUNT":      fmt.Sprintf("%d", rec.JUnitTotal),
		"PLATFORM_TESTS_FAILURES":   fmt.Sprintf("%d", rec.JUnitFailed),
		"PLATFORM_COVERAGE_PCT":     fmt.Sprintf("%.2f", cov),
		"PLATFORM_COVERAGE_THRESH":  fmt.Sprintf("%d", rec.CovThreshold),
		"PLATFORM_SCAN_JOB_REF":     scanRef,
		"PLATFORM_STAGES_JSON":      string(stagesJSON),
		"PLATFORM_LIBRARIES_JSON":   string(libsJSON),
		"PLATFORM_GIT_COMMIT":       rec.GitCommit,
		"PLATFORM_GIT_URL":          rec.GitURL,
	}
}
