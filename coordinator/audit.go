package coordinator

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPAuditClient struct {
	baseURL string
	client  *http.Client
}

func NewAuditClient(baseURL string) *HTTPAuditClient {
	return &HTTPAuditClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *HTTPAuditClient) GetSummary(auditID string) (*AuditSummary, error) {
	url := fmt.Sprintf("%s/builds/%s/summary", a.baseURL, auditID)

	resp, err := a.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("audit service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("audit summary not found for auditId=%s — build may not have completed", auditID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audit service returned HTTP %d for auditId=%s", resp.StatusCode, auditID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(body)
	digest := fmt.Sprintf("%x", hash)

	var report struct {
		TotalExecs            int64 `json:"total_execs"`
		SandboxViolationCount int64 `json:"sandbox_violation_count"`
		AnomalyCount          int64 `json:"anomaly_count"`
		CorrelatedExecs []struct {
			Anomaly   bool `json:"anomaly"`
			TetragonEvent struct {
				EventType string `json:"event_type"`
			} `json:"tetragon_event"`
		} `json:"correlated_execs"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("audit summary unparseable: %w", err)
	}

	var unexpectedNetwork int64
	for _, e := range report.CorrelatedExecs {
		if e.Anomaly && e.TetragonEvent.EventType == "network" {
			unexpectedNetwork++
		}
	}

	return &AuditSummary{
		Digest:                 digest,
		AnomalyCount:           report.AnomalyCount,
		UnexpectedNetworkCount: unexpectedNetwork,
		ExecsObserved:          report.TotalExecs,
		SandboxViolations:      report.SandboxViolationCount,
	}, nil
}
