package ui

import (
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
	Key             string              `json:"key"`
	JobPath         string              `json:"jobPath"`
	BuildNumber     int                 `json:"buildNumber"`
	Branch          string              `json:"branch"`
	GitCommit       string              `json:"gitCommit"`
	AuditID         string              `json:"auditId"`
	ImageRef        string              `json:"imageRef"`
	JUnitTotal      int                 `json:"junitTotal"`
	JUnitFailed     int                 `json:"junitFailed"`
	LineCoverage    float64             `json:"lineCoverage"`
	CovThreshold    int                 `json:"covThreshold"`
	Stages          []coordinator.Stage   `json:"stages"`
	Libraries       []coordinator.Library `json:"libraries"`
	LibrarySteps    []string            `json:"librarySteps"`
	ImageScanJob    string              `json:"imageScanJob"`
	ImageScanBuild  int                 `json:"imageScanBuild"`
	SourceScanJob   string              `json:"sourceScanJob"`
	SourceScanBuild int                 `json:"sourceScanBuild"`
	ImageScanAt     *time.Time          `json:"imageScanAt"`
	SourceScanAt    *time.Time          `json:"sourceScanAt"`
	ReceivedAt      time.Time           `json:"receivedAt"`
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

const spaHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Attest Coordinator</title>
<style>
:root {
  --bg:     #0d1117;
  --bg2:    #161b22;
  --bg3:    #21262d;
  --border: #30363d;
  --text:   #c9d1d9;
  --muted:  #8b949e;
  --green:  #3fb950;
  --red:    #f85149;
  --yellow: #d29922;
  --blue:   #58a6ff;
  --font:   'SF Mono','Fira Code','Cascadia Code',monospace;
}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:var(--font);font-size:13px;line-height:1.5}
a{color:var(--blue);text-decoration:none}a:hover{text-decoration:underline}
header{background:var(--bg2);border-bottom:1px solid var(--border);padding:10px 24px;display:flex;align-items:center;gap:16px}
header h1{font-size:13px;color:var(--blue);font-weight:normal}
#back{cursor:pointer;color:var(--muted);font-size:12px;display:none}
#back:hover{color:var(--text)}
#breadcrumb{color:var(--muted);font-size:12px}
main{padding:20px 24px;max-width:1400px}
.tbl{width:100%;border-collapse:collapse}
.tbl th{text-align:left;color:var(--muted);padding:6px 10px;border-bottom:1px solid var(--border);font-weight:normal;font-size:11px;text-transform:uppercase;letter-spacing:.5px;white-space:nowrap}
.tbl td{padding:6px 10px;border-bottom:1px solid var(--border);white-space:nowrap}
.tbl tr.clickable:hover td{background:var(--bg2);cursor:pointer}
.jpath{color:var(--muted)}
.bnum{font-weight:bold}
.badge{display:inline-block;padding:1px 5px;border-radius:3px;font-size:11px}
.br{background:rgba(248,81,73,.15);color:var(--red)}
.bg{background:rgba(63,185,80,.15);color:var(--green)}
.by{background:rgba(210,153,34,.15);color:var(--yellow)}
.bm{background:var(--bg3);color:var(--muted)}
.stats{display:flex;gap:28px;padding:14px 16px;background:var(--bg2);border:1px solid var(--border);border-radius:6px;margin-bottom:20px}
.stat-label{color:var(--muted);font-size:10px;text-transform:uppercase;letter-spacing:.5px}
.stat-val{font-size:22px;margin-top:2px}
.stat-val.red{color:var(--red)}
.stat-val.green{color:var(--green)}
.sec{margin-bottom:24px}
.sec-title{color:var(--muted);font-size:10px;text-transform:uppercase;letter-spacing:.5px;padding-bottom:8px;border-bottom:1px solid var(--border);margin-bottom:10px}
.evtbl{border-collapse:collapse;width:100%;max-width:700px;font-size:12px}
.evtbl td{padding:5px 10px;border-bottom:1px solid var(--border)}
.evtbl td:first-child{color:var(--muted);width:140px}
.eok{color:var(--green)}
.epend{color:var(--blue)}
.edim{color:var(--muted)}
.infotbl{border-collapse:collapse;width:100%;max-width:700px;font-size:12px}
.infotbl td{padding:5px 10px;border-bottom:1px solid var(--border)}
.infotbl td:first-child{color:var(--muted);width:140px}
.rbox{background:#1a0808;border:1px solid #3d1010;border-radius:6px;padding:12px 16px;color:var(--red);font-size:12px;max-width:700px;white-space:pre-wrap;margin-top:8px}
.outcome{display:inline-block;padding:3px 10px;border-radius:4px;font-size:12px;font-weight:600}
.outcome-ATTESTED{background:rgba(63,185,80,.15);color:var(--green)}
.outcome-REFUSED{background:rgba(210,153,34,.15);color:var(--yellow)}
.outcome-CEDAR_DENY{background:rgba(248,81,73,.15);color:var(--red)}
.outcome-SCAN_FAILED{background:rgba(248,81,73,.15);color:var(--red)}
.outcome-pending{background:rgba(88,166,255,.1);color:var(--blue)}
.dtbl{width:100%;max-width:700px;border-collapse:collapse;font-size:12px}
.dtbl th{text-align:left;color:var(--muted);padding:5px 8px;border-bottom:1px solid var(--border);font-weight:normal;font-size:10px;text-transform:uppercase;letter-spacing:.5px}
.dtbl td{padding:4px 8px;border-bottom:1px solid var(--border)}
.dtbl tr:hover td{background:var(--bg2)}
.stok{color:var(--green)}.stfail{color:var(--red)}
.sha-ok{color:var(--muted);font-size:11px}
.sha-warn{color:var(--yellow);font-size:11px}
.msg{color:var(--muted);padding:20px 0;text-align:center;font-size:12px}
.err{color:var(--red);padding:20px 0;font-size:12px}
</style>
</head>
<body>
<header>
  <h1>platform-attest-coordinator</h1>
  <span id="back">&#8592; builds</span>
  <span id="breadcrumb"></span>
</header>
<main id="app"><div class="msg">loading&#8230;</div></main>
<script>
'use strict';
var _refreshTimer=null;
function route(){var p=new URLSearchParams(location.search).get('build');if(p){clearRefresh();showBuild(p);}else{showList();}}
function clearRefresh(){if(_refreshTimer){clearInterval(_refreshTimer);_refreshTimer=null;}}
document.getElementById('back').addEventListener('click',function(){history.pushState(null,'','/');route();});
window.addEventListener('popstate',function(){route();});
route();
async function showList(){document.getElementById('back').style.display='none';document.getElementById('breadcrumb').textContent='';await renderList();clearRefresh();_refreshTimer=setInterval(renderList,10000);}
async function renderList(){var app=document.getElementById('app');try{var d=await fetch('/api/builds').then(function(r){return r.json();});var h='';h+='<div class="sec"><div class="sec-title">active ('+d.active.length+')</div>';if(!d.active.length){h+='<div class="msg" style="text-align:left;padding:12px 0">no active builds</div>';}else{h+='<table class="tbl"><thead><tr><th>build</th><th>branch</th><th>evidence</th><th>junit</th><th>age</th></tr></thead><tbody>';for(var i=0;i<d.active.length;i++){var b=d.active[i];var ev=evidenceSummary(b);var jb=b.junitFailed>0?badge('br',b.junitTotal+' / '+b.junitFailed+' failed'):badge('bg',b.junitTotal+' passed');h+='<tr class="clickable" onclick="nav('+JSON.stringify(b.key)+')">'+'<td><span class="jpath">'+jobFolder(b.jobPath)+'</span><span class="bnum">#'+b.buildNumber+'</span></td>'+'<td class="edim">'+esc(b.branch)+'</td>'+'<td class="epend">'+esc(ev)+'</td>'+'<td>'+jb+'</td>'+'<td class="edim">'+age(b.receivedAt)+'</td></tr>';}h+='</tbody></table>';}h+='</div>';h+='<div class="sec"><div class="sec-title">recent decisions ('+d.decisions.length+')</div>';if(!d.decisions.length){h+='<div class="msg" style="text-align:left;padding:12px 0">no decisions yet</div>';}else{h+='<table class="tbl"><thead><tr><th>outcome</th><th>build</th><th>branch</th><th>reason</th><th>decided</th></tr></thead><tbody>';for(var i=0;i<d.decisions.length;i++){var dec=d.decisions[i];var ob=outcomeBadge(dec.outcome);var reason=dec.reason?'<span style="color:var(--red)">'+esc(dec.reason.substring(0,90))+'</span>':'<span class="edim">—</span>';h+='<tr class="clickable" onclick="nav('+JSON.stringify(dec.key)+')">'+'<td>'+ob+'</td>'+'<td><span class="jpath">'+jobFolder(dec.jobPath)+'</span><span class="bnum">#'+dec.buildNumber+'</span></td>'+'<td class="edim">'+esc(dec.branch)+'</td>'+'<td>'+reason+'</td>'+'<td class="edim">'+age(dec.decidedAt)+'</td></tr>';}h+='</tbody></table>';}h+='</div>';app.innerHTML=h;}catch(e){app.innerHTML='<div class="err">failed to load: '+esc(e.message)+'</div>';}}
function nav(key){clearRefresh();history.pushState(null,'','/?build='+encodeURIComponent(key));showBuild(key);}
async function showBuild(key){document.getElementById('back').style.display='';document.getElementById('breadcrumb').textContent=key;var app=document.getElementById('app');app.innerHTML='<div class="msg">loading…</div>';try{var d=await fetch('/api/builds/'+encodeURIComponent(key)).then(function(r){if(!r.ok)throw new Error(r.status===404?'build not found':r.statusText);return r.json();});if(d.decision){app.innerHTML=renderDecisionDetail(d.decision);}else if(d.active){app.innerHTML=renderActiveDetail(d.active);}}catch(e){app.innerHTML='<div class="err">'+esc(e.message)+'</div>';}}
function renderDecisionDetail(d){var cov=d.lineCoverage>=0?d.lineCoverage.toFixed(1)+'%':'—';var covCls=d.lineCoverage<0?'':(d.lineCoverage<d.covThreshold?' red':' green');var h='';h+='<div class="stats">'+statRaw('junit',d.junitTotal+' tests'+(d.junitFailed>0?', <span class="stat-val red">'+d.junitFailed+' failed</span>':''))+statVal('coverage',cov,covCls)+statVal('stages',d.stages?d.stages.length:0,'')+statVal('libraries',d.libraries?d.libraries.length:0,'')+'</div>';h+='<div class="sec"><div class="sec-title">decision</div><span class="outcome outcome-'+esc(d.outcome)+'">'+esc(d.outcome)+'</span>';if(d.reason)h+='<div class="rbox">'+esc(d.reason)+'</div>';h+='</div>';h+='<div class="sec"><div class="sec-title">evidence timeline</div><table class="evtbl">'+evRow('build received',fmtTime(d.receivedAt),'eok')+evRow('image scan',d.imageScanAt?fmtTime(d.imageScanAt):null,'eok')+evRow('source scan',d.sourceScanAt?fmtTime(d.sourceScanAt):null,'eok')+evRow('decision',fmtTime(d.decidedAt),'eok')+'</table></div>';h+='<div class="sec"><div class="sec-title">build info</div><table class="infotbl">'+infoRow('job','<code>'+esc(d.jobPath)+'</code>')+infoRow('build','#'+d.buildNumber)+infoRow('branch',esc(d.branch))+infoRow('git commit','<code>'+esc(d.gitCommit)+'</code>')+infoRow('image ref','<code>'+esc(d.imageRef)+'</code>')+infoRow('audit id','<code>'+esc(d.auditId)+'</code>')+'</table></div>';h+=stagesSection(d.stages);h+=librariesSection(d.libraries);return h;}
function renderActiveDetail(b){var cov=b.lineCoverage>=0?b.lineCoverage.toFixed(1)+'%':'—';var covCls=b.lineCoverage<0?'':(b.lineCoverage<b.covThreshold?' red':' green');var h='';h+='<div class="stats">'+statRaw('junit',b.junitTotal+' tests'+(b.junitFailed>0?', <span class="stat-val red">'+b.junitFailed+' failed</span>':''))+statVal('coverage',cov,covCls)+statVal('stages',b.stages?b.stages.length:0,'')+statVal('libraries',b.libraries?b.libraries.length:0,'')+'</div>';h+='<div class="sec"><div class="sec-title">status</div><span class="outcome outcome-pending">● collecting evidence</span></div>';h+='<div class="sec"><div class="sec-title">evidence</div><table class="evtbl">'+evRow('build received',fmtTime(b.receivedAt),'eok');if(b.imageScanJob){h+=evRow('image scan',fmtTime(b.imageScanAt)+' — '+esc(b.imageScanJob)+' #'+b.imageScanBuild,'eok');}else{h+=evRow('image scan',null,'eok');}if(b.sourceScanJob){h+=evRow('source scan',fmtTime(b.sourceScanAt)+' — '+esc(b.sourceScanJob)+' #'+b.sourceScanBuild,'eok');}else{h+=evRow('source scan',null,'eok');}h+='</table></div>';h+='<div class="sec"><div class="sec-title">build info</div><table class="infotbl">'+infoRow('job','<code>'+esc(b.jobPath)+'</code>')+infoRow('build','#'+b.buildNumber)+infoRow('branch',esc(b.branch))+infoRow('git commit','<code>'+esc(b.gitCommit)+'</code>')+infoRow('image ref','<code>'+esc(b.imageRef)+'</code>')+infoRow('audit id','<code>'+esc(b.auditId)+'</code>')+'</table></div>';h+=stagesSection(b.stages);h+=librariesSection(b.libraries);return h;}
function stagesSection(stages){if(!stages||!stages.length)return'';var h='<div class="sec"><div class="sec-title">stages</div><table class="dtbl"><thead><tr><th>name</th><th>status</th></tr></thead><tbody>';for(var i=0;i<stages.length;i++){var s=stages[i];var cls=s.Status==='SUCCESS'?'stok':'stfail';h+='<tr><td>'+esc(s.Name)+'</td><td class="'+cls+'">'+esc(s.Status)+'</td></tr>';}return h+'</tbody></table></div>';}
function librariesSection(libs){if(!libs||!libs.length)return'';var h='<div class="sec"><div class="sec-title">libraries</div><table class="dtbl"><thead><tr><th>name</th><th>sha / ref</th></tr></thead><tbody>';for(var i=0;i<libs.length;i++){var l=libs[i];var isSHA=/^[0-9a-f]{40}$/i.test(l.SHA);var cls=isSHA?'sha-ok':'sha-warn';var val=isSHA?l.SHA.substring(0,12)+'…':l.SHA+(isSHA?'':' ⚠');h+='<tr><td>'+esc(l.Name)+'</td><td class="'+cls+'" title="'+esc(l.SHA)+'">'+val+'</td></tr>';}return h+'</tbody></table></div>';}
function evidenceSummary(b){var m=[];if(!b.imageScanJob)m.push('image scan');if(!b.sourceScanJob)m.push('source scan');return m.length?'waiting: '+m.join(', '):'all evidence collected';}
function outcomeBadge(o){var cls={ATTESTED:'bg',REFUSED:'by',CEDAR_DENY:'br',SCAN_FAILED:'br'}[o]||'bm';return '<span class="badge '+cls+'">'+esc(o)+'</span>';}
function jobFolder(path){var parts=path.split('/');return esc(parts.join('/'))+'/';}
function badge(cls,text){return '<span class="badge '+cls+'">'+esc(String(text))+'</span>';}
function statRaw(label,valHTML){return '<div><div class="stat-label">'+esc(label)+'</div><div class="stat-val">'+valHTML+'</div></div>';}
function statVal(label,val,cls){return '<div><div class="stat-label">'+esc(label)+'</div><div class="stat-val'+cls+'">'+val+'</div></div>';}
function evRow(label,val,cls){if(!val)return '<tr><td>'+esc(label)+'</td><td class="epend">waiting…</td></tr>';return '<tr><td>'+esc(label)+'</td><td class="'+cls+'">'+val+'</td></tr>';}
function infoRow(label,val){return '<tr><td>'+esc(label)+'</td><td>'+val+'</td></tr>';}
function age(ts){if(!ts)return '—';var d=(Date.now()-new Date(ts))/1000;if(d<60)return Math.floor(d)+'s ago';if(d<3600)return Math.floor(d/60)+'m'+Math.floor(d%60)+'s ago';return Math.floor(d/3600)+'h'+Math.floor((d%3600)/60)+'m ago';}
function fmtTime(ts){if(!ts)return '—';return new Date(ts).toISOString().replace('T',' ').replace('Z','').split('.')[0];}
function esc(s){return String(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');}
</script>
</body>
</html>`
