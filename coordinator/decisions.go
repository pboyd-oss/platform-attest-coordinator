package coordinator

import "time"

type DecisionOutcome string

const (
	OutcomeRefused    DecisionOutcome = "REFUSED"
	OutcomeCedarDeny  DecisionOutcome = "CEDAR_DENY"
	OutcomeAttested   DecisionOutcome = "ATTESTED"
	OutcomeScanFailed DecisionOutcome = "SCAN_FAILED"
)

// DecisionRecord captures the final outcome of one build's attestation attempt.
type DecisionRecord struct {
	Key          string
	JobPath      string
	BuildNumber  int
	Branch       string
	GitCommit    string
	ImageRef     string
	AuditID      string
	Outcome      DecisionOutcome
	Reason       string
	ReceivedAt   time.Time
	DecidedAt    time.Time
	ImageScanAt  time.Time
	SourceScanAt time.Time
}

type decisionLog struct {
	entries []DecisionRecord
	max     int
}

func newDecisionLog(max int) *decisionLog {
	return &decisionLog{entries: make([]DecisionRecord, 0, max), max: max}
}

func (d *decisionLog) add(rec DecisionRecord) {
	d.entries = append(d.entries, rec)
	if len(d.entries) > d.max {
		d.entries = d.entries[len(d.entries)-d.max:]
	}
}

// snapshot returns entries newest first.
func (d *decisionLog) snapshot() []DecisionRecord {
	out := make([]DecisionRecord, len(d.entries))
	for i, e := range d.entries {
		out[len(d.entries)-1-i] = e
	}
	return out
}
