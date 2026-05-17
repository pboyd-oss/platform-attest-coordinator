package ui

import (
	"fmt"
	"html/template"
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
	tmpl  *template.Template
}

func NewHandler(coord coordinatorReader) *Handler {
	fm := template.FuncMap{
		"age":      fmtAge,
		"missing":  missingEvidence,
		"buildhref": func(jobPath string, buildNum int) template.URL {
			return template.URL("/builds?key=" + url.QueryEscape(fmt.Sprintf("%s#%d", jobPath, buildNum)))
		},
		"keyhref": func(key string) template.URL {
			return template.URL("/builds?key=" + url.QueryEscape(key))
		},
		"css":    outcomeCSS,
		"label":  outcomeLabel,
		"ts":     fmtTS,
		"commit": shortCommit,
	}
	tmpl := template.Must(template.New("root").Funcs(fm).Parse(tmplDashboard + tmplDetail))
	return &Handler{coord: coord, tmpl: tmpl}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		h.dashboard(w, r)
	case "/builds":
		h.detail(w, r)
	default:
		http.NotFound(w, r)
	}
}

type dashboardData struct {
	Now       time.Time
	Active    []coordinator.BuildRecord
	Decisions []coordinator.DecisionRecord
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	data := dashboardData{
		Now:       time.Now().UTC(),
		Active:    h.coord.ActiveBuilds(),
		Decisions: h.coord.RecentDecisions(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "dashboard", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type detailData struct {
	Now      time.Time
	Key      string
	Decision *coordinator.DecisionRecord
	Active   *coordinator.BuildRecord
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	decisions := h.coord.RecentDecisions()
	active := h.coord.ActiveBuilds()

	data := detailData{Now: time.Now().UTC(), Key: key}
	for i := range decisions {
		if decisions[i].Key == key {
			data.Decision = &decisions[i]
			break
		}
	}
	if data.Decision == nil {
		for i := range active {
			if coordinator.BuildKey(active[i].JobPath, active[i].BuildNumber) == key {
				data.Active = &active[i]
				break
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "detail", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- template helpers ---

func fmtAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds ago", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm ago", int(d.Hours()), int(d.Minutes())%60)
}

func fmtTS(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("15:04:05")
}

func shortCommit(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func missingEvidence(rec coordinator.BuildRecord) string {
	var m []string
	if rec.ImageScanJob == "" {
		m = append(m, "image scan")
	}
	if rec.SourceScanJob == "" {
		m = append(m, "source scan")
	}
	if len(m) == 0 {
		return "all evidence collected"
	}
	return "waiting for: " + strings.Join(m, ", ")
}

func outcomeCSS(o coordinator.DecisionOutcome) string {
	switch o {
	case coordinator.OutcomeAttested:
		return "outcome-attested"
	case coordinator.OutcomeRefused:
		return "outcome-refused"
	case coordinator.OutcomeCedarDeny:
		return "outcome-cedar-deny"
	case coordinator.OutcomeScanFailed:
		return "outcome-scan-failed"
	default:
		return "outcome-unknown"
	}
}

func outcomeLabel(o coordinator.DecisionOutcome) string {
	switch o {
	case coordinator.OutcomeAttested:
		return "ATTESTED"
	case coordinator.OutcomeRefused:
		return "REFUSED"
	case coordinator.OutcomeCedarDeny:
		return "CEDAR DENY"
	case coordinator.OutcomeScanFailed:
		return "SCAN FAILED"
	default:
		return string(o)
	}
}

// --- templates ---

const tmplDashboard = `{{define "dashboard"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="refresh" content="10">
<title>Attest Coordinator</title>
<style>
*{box-sizing:border-box}
body{font-family:system-ui,-apple-system,sans-serif;background:#0d1117;color:#c9d1d9;margin:0;padding:24px;font-size:14px}
h1{font-size:14px;color:#8b949e;font-weight:400;margin:0 0 28px}
h2{font-size:11px;text-transform:uppercase;letter-spacing:.1em;color:#6e7681;margin:32px 0 12px;border-bottom:1px solid #21262d;padding-bottom:8px}
table{border-collapse:collapse;width:100%}
th{text-align:left;color:#6e7681;font-weight:400;padding:6px 12px;font-size:11px;text-transform:uppercase;letter-spacing:.05em}
td{padding:9px 12px;border-bottom:1px solid #161b22;vertical-align:top}
a{color:#58a6ff;text-decoration:none}
a:hover{text-decoration:underline}
code{font-family:ui-monospace,monospace;font-size:12px;color:#8b949e}
.badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600;letter-spacing:.03em}
.outcome-attested{background:#0d2d12;color:#3fb950}
.outcome-refused{background:#2d1f05;color:#d29922}
.outcome-cedar-deny{background:#2d0e0e;color:#f85149}
.outcome-scan-failed{background:#2d0e0e;color:#f85149}
.outcome-unknown{background:#21262d;color:#8b949e}
.pending{color:#58a6ff}
.muted{color:#6e7681}
.reason{color:#f85149}
.empty{color:#484f58;font-style:italic;padding:16px 12px}
.job{font-family:ui-monospace,monospace;font-size:12px}
</style>
</head>
<body>
<h1>platform-attest-coordinator &mdash; {{.Now.Format "2006-01-02 15:04:05 UTC"}} (auto-refresh 10s)</h1>

<h2>Active ({{len .Active}})</h2>
{{if .Active}}
<table>
<tr><th>Build</th><th>Branch</th><th>Evidence</th><th>Age</th></tr>
{{range .Active}}
<tr>
<td class="job"><a href="{{buildhref .JobPath .BuildNumber}}">{{.JobPath}} #{{.BuildNumber}}</a></td>
<td class="muted">{{.Branch}}</td>
<td class="pending">{{missing .}}</td>
<td class="muted">{{age .ReceivedAt}}</td>
</tr>
{{end}}
</table>
{{else}}<p class="empty">No active builds</p>{{end}}

<h2>Recent Decisions ({{len .Decisions}})</h2>
{{if .Decisions}}
<table>
<tr><th>Outcome</th><th>Build</th><th>Branch</th><th>Reason</th><th>Decided</th></tr>
{{range .Decisions}}
<tr>
<td><span class="badge {{css .Outcome}}">{{label .Outcome}}</span></td>
<td class="job"><a href="{{keyhref .Key}}">{{.JobPath}} #{{.BuildNumber}}</a></td>
<td class="muted">{{.Branch}}</td>
<td{{if ne .Outcome "ATTESTED"}} class="reason"{{end}}>{{.Reason}}</td>
<td class="muted">{{age .DecidedAt}}</td>
</tr>
{{end}}
</table>
{{else}}<p class="empty">No decisions recorded yet — waiting for builds</p>{{end}}

</body>
</html>{{end}}`

const tmplDetail = `{{define "detail"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Build Detail &mdash; Attest Coordinator</title>
<style>
*{box-sizing:border-box}
body{font-family:system-ui,-apple-system,sans-serif;background:#0d1117;color:#c9d1d9;margin:0;padding:24px;font-size:14px}
h1{font-size:16px;font-weight:600;margin:0 0 4px;font-family:ui-monospace,monospace}
h2{font-size:11px;text-transform:uppercase;letter-spacing:.1em;color:#6e7681;margin:32px 0 12px;border-bottom:1px solid #21262d;padding-bottom:8px}
.sub{font-size:12px;color:#8b949e;margin:0 0 28px}
table{border-collapse:collapse;width:100%;max-width:700px}
td{padding:8px 12px;border-bottom:1px solid #161b22}
td:first-child{color:#8b949e;width:160px;font-size:12px}
a{color:#58a6ff;text-decoration:none}
a:hover{text-decoration:underline}
code{font-family:ui-monospace,monospace;font-size:12px}
.badge{display:inline-block;padding:3px 10px;border-radius:4px;font-size:12px;font-weight:600}
.outcome-attested{background:#0d2d12;color:#3fb950}
.outcome-refused{background:#2d1f05;color:#d29922}
.outcome-cedar-deny{background:#2d0e0e;color:#f85149}
.outcome-scan-failed{background:#2d0e0e;color:#f85149}
.outcome-unknown{background:#21262d;color:#8b949e}
.pending{color:#58a6ff}
.reason-box{background:#1a0808;border:1px solid #3d1010;border-radius:6px;padding:12px 16px;color:#f85149;font-family:ui-monospace,monospace;font-size:13px;max-width:700px;white-space:pre-wrap;margin-top:8px}
.back{font-size:12px;color:#6e7681;margin-bottom:20px;display:block}
.muted{color:#6e7681}
.ok{color:#3fb950}
</style>
</head>
<body>
<a class="back" href="/">← back to dashboard</a>

{{if .Decision}}
<h1>{{.Decision.JobPath}} #{{.Decision.BuildNumber}}</h1>
<p class="sub">{{.Decision.Branch}} &mdash; <code>{{commit .Decision.GitCommit}}</code> &mdash; decided {{age .Decision.DecidedAt}}</p>

<h2>Decision</h2>
<span class="badge {{css .Decision.Outcome}}">{{label .Decision.Outcome}}</span>
{{if .Decision.Reason}}<div class="reason-box">{{.Decision.Reason}}</div>{{end}}

<h2>Build Info</h2>
<table>
<tr><td>Job</td><td><code>{{.Decision.JobPath}}</code></td></tr>
<tr><td>Build</td><td>#{{.Decision.BuildNumber}}</td></tr>
<tr><td>Branch</td><td>{{.Decision.Branch}}</td></tr>
<tr><td>Git Commit</td><td><code>{{.Decision.GitCommit}}</code></td></tr>
<tr><td>Image Ref</td><td><code>{{.Decision.ImageRef}}</code></td></tr>
<tr><td>Audit ID</td><td><code>{{.Decision.AuditID}}</code></td></tr>
</table>

<h2>Evidence Timeline</h2>
<table>
<tr><td>Build received</td><td>{{ts .Decision.ReceivedAt}}</td></tr>
<tr><td>Image scan</td><td>{{if .Decision.ImageScanAt.IsZero}}<span class="muted">—</span>{{else}}<span class="ok">{{ts .Decision.ImageScanAt}}</span>{{end}}</td></tr>
<tr><td>Source scan</td><td>{{if .Decision.SourceScanAt.IsZero}}<span class="muted">—</span>{{else}}<span class="ok">{{ts .Decision.SourceScanAt}}</span>{{end}}</td></tr>
<tr><td>Decision</td><td>{{ts .Decision.DecidedAt}}</td></tr>
</table>

{{else if .Active}}
<h1>{{.Active.JobPath}} #{{.Active.BuildNumber}}</h1>
<p class="sub">{{.Active.Branch}} &mdash; <code>{{commit .Active.GitCommit}}</code> &mdash; received {{age .Active.ReceivedAt}}</p>

<h2>Status</h2>
<span class="pending">&#x25CF; Collecting evidence</span>

<h2>Evidence</h2>
<table>
<tr><td>Build received</td><td><span class="ok">{{ts .Active.ReceivedAt}}</span></td></tr>
<tr><td>Image scan</td><td>{{if .Active.ImageScanJob}}<span class="ok">{{ts .Active.ImageScanAt}} ({{.Active.ImageScanJob}} #{{.Active.ImageScanBuild}})</span>{{else}}<span class="pending">waiting…</span>{{end}}</td></tr>
<tr><td>Source scan</td><td>{{if .Active.SourceScanJob}}<span class="ok">{{ts .Active.SourceScanAt}} ({{.Active.SourceScanJob}} #{{.Active.SourceScanBuild}})</span>{{else}}<span class="pending">waiting…</span>{{end}}</td></tr>
</table>

<h2>Build Info</h2>
<table>
<tr><td>Branch</td><td>{{.Active.Branch}}</td></tr>
<tr><td>Git Commit</td><td><code>{{.Active.GitCommit}}</code></td></tr>
<tr><td>Image Ref</td><td><code>{{.Active.ImageRef}}</code></td></tr>
<tr><td>Audit ID</td><td><code>{{.Active.AuditID}}</code></td></tr>
<tr><td>JUnit</td><td>{{.Active.JUnitTotal}} tests, {{.Active.JUnitFailed}} failed</td></tr>
</table>

{{else}}
<h1>Build not found</h1>
<p class="muted">Key: <code>{{.Key}}</code> — not in active builds or recent decision log.</p>
{{end}}

</body>
</html>{{end}}`
