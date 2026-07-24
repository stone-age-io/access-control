package diag

import (
	"html/template"
)

// statusTemplate renders the /status page. It is fully self-contained — inline
// CSS and a few lines of inline vanilla JS, no external assets — so it works on a
// box with no network and never enters the accessd UI's Vite/embed pipeline. The
// inline script refreshes the content in place (fetch + swap the #doc subtree) so
// scroll position survives each update, and offers pause/refresh controls and a
// freshness indicator. No framework and no CDN: the offline constraint rules out a
// script src, and one refresh loop doesn't warrant inlining one.
var statusTemplate = template.Must(template.New("status").Funcs(template.FuncMap{
	"short": func(s string) string {
		if len(s) > 12 {
			return s[:12]
		}
		return s
	},
	// label prettifies a policy-count key for display. Only the camelCase aux
	// keys need help; everything else is already a plain word.
	"label": func(k string) string {
		switch k {
		case "auxInputs":
			return "aux inputs"
		case "auxOutputs":
			return "aux outputs"
		default:
			return k
		}
	},
}).Parse(statusHTML))

const statusHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>access-controller · {{.Identity.Controller}}</title>
<style>
  /* Self-contained + light/dark adaptive. The palette mirrors the accessd UI's
     theme (base-100/200/300, soft-badge tints) so a field screenshot reads as the
     same product, but nothing here is shared with the Vite build. */
  :root {
    color-scheme: light dark;
    --page: #f4f6fa; --card: #fff; --fg: #1b2330; --muted: #6b7280; --line: #e4e8ef;
    --zebra: rgba(27,35,48,.025);
    --good-bg: #e4f3e9; --good-fg: #146c34; --good-dot: #16a34a;
    --bad-bg:  #fbebeb; --bad-fg:  #b41c1c; --bad-dot:  #dc2626;
    --warn-bg: #fbf0de; --warn-fg: #97590a; --warn-dot: #d97706;
    --neut-bg: #edf0f5; --neut-fg: #47536a; --neut-dot: #64748b;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --page: #0d1017; --card: #12161d; --fg: #e7ebf2; --muted: #8a93a5; --line: #232a35;
      --zebra: rgba(231,235,242,.03);
      --good-bg: #14291d; --good-fg: #7fd69c; --good-dot: #34b26e;
      --bad-bg:  #2e1717; --bad-fg:  #f2a0a0; --bad-dot:  #e05656;
      --warn-bg: #2e2410; --warn-fg: #f4c264; --warn-dot: #e0a417;
      --neut-bg: #232b37; --neut-fg: #a8b3c6; --neut-dot: #7c8aa0;
    }
  }
  * { box-sizing: border-box; }
  body { font: 14px/1.45 system-ui, sans-serif; margin: 0 auto; max-width: 70rem; padding: 1.25rem 1.25rem 3rem; background: var(--page); color: var(--fg); }
  h1 { font-size: 1.35rem; margin: 0 0 .15rem; }
  h2 { font-size: .74rem; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); margin: 1.75rem 0 .5rem; }
  .sub { color: var(--muted); margin: 0 0 .25rem; }
  .band { background: var(--card); border: 1px solid var(--line); border-radius: .75rem; padding: 1rem 1.15rem; }
  .strip { display: flex; flex-wrap: wrap; gap: .5rem; margin: .85rem 0 0; }
  .badge { display: inline-flex; align-items: center; gap: .4rem; padding: .18rem .55rem; border-radius: .375rem; font-size: .76rem; font-weight: 600; line-height: 1; }
  .badge::before { content: ""; width: .4rem; height: .4rem; border-radius: 50%; background: currentColor; flex: none; }
  .good { background: var(--good-bg); color: var(--good-fg); }  .good::before { background: var(--good-dot); }
  .bad  { background: var(--bad-bg);  color: var(--bad-fg);  }  .bad::before  { background: var(--bad-dot); }
  .warn { background: var(--warn-bg); color: var(--warn-fg); }  .warn::before { background: var(--warn-dot); }
  .neutral { background: var(--neut-bg); color: var(--neut-fg); }  .neutral::before { background: var(--neut-dot); }
  .muted { color: var(--muted); }
  .scroll { overflow-x: auto; border: 1px solid var(--line); border-radius: .75rem; background: var(--card); }
  table { border-collapse: collapse; width: 100%; font-size: .84rem; }
  th, td { text-align: left; padding: .45rem .65rem; border-bottom: 1px solid var(--line); white-space: nowrap; }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr:nth-child(even) td { background: var(--zebra); }
  th { color: var(--muted); font-weight: 600; font-size: .66rem; text-transform: uppercase; letter-spacing: .04em; }
  code, .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .banner { background: var(--bad-bg); color: var(--bad-fg); border: 1px solid var(--line); padding: .75rem 1rem; border-radius: .75rem; font-weight: 600; }
  .row-warn td { background: var(--warn-bg); }
  .kv { display: grid; grid-template-columns: max-content 1fr; gap: .3rem 1.25rem; margin: 0; }
  .kv dt { color: var(--muted); }
  .kv dd { margin: 0; min-width: 0; overflow-wrap: anywhere; }
  /* Policy counts as a responsive grid of stat tiles: wraps at any width (no
     more nbsp-joined line forcing a horizontal scroll on mobile) and reads as a
     scannable summary of what actually synced to this box. */
  .stats { display: grid; grid-template-columns: repeat(auto-fill, minmax(6.5rem, 1fr)); gap: .55rem; margin: .5rem 0 0; }
  .stat { background: var(--card); border: 1px solid var(--line); border-radius: .6rem; padding: .55rem .7rem; }
  .stat .n { font-size: 1.45rem; font-weight: 700; line-height: 1.05; font-variant-numeric: tabular-nums; }
  .stat .k { font-size: .66rem; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); margin-top: .2rem; }
  .stat.zero .n { color: var(--muted); opacity: .55; } /* an empty count is present but de-emphasized */
  /* Sticky control bar for the in-place refresh: freshness indicator on the left,
     pause/refresh on the right. Bleeds to the page padding edges and carries the
     page background so content scrolls cleanly under it. */
  #bar { position: sticky; top: 0; z-index: 5; display: flex; align-items: center; gap: .55rem; padding: .5rem 1.25rem; margin: -1.25rem -1.25rem .7rem; background: var(--page); }
  #bar #fresh { margin-right: auto; font-size: .75rem; color: var(--muted); font-variant-numeric: tabular-nums; }
  #bar.stale #fresh { color: var(--bad-fg); font-weight: 600; }
  #bar button { font: inherit; font-size: .8rem; font-weight: 600; line-height: 1; cursor: pointer; padding: .35rem .75rem; border-radius: .45rem; border: 1px solid var(--line); background: var(--card); color: var(--fg); }
  #bar button:hover { border-color: var(--neut-dot); }
  #bar button:active { transform: translateY(1px); }
  .nojs { background: var(--warn-bg); color: var(--warn-fg); border-radius: .5rem; padding: .5rem .75rem; margin: 0 0 .7rem; font-size: .8rem; }
  .notice { background: var(--warn-bg); color: var(--warn-fg); border: 1px solid var(--line); padding: .7rem 1rem; border-radius: .75rem; font-weight: 600; margin: .5rem 0 0; }
  footer { margin-top: 2rem; color: var(--muted); font-size: .8rem; }
</style>
</head>
<body>

<div id="bar">
  <span id="fresh">live</span>
  <button id="pause" type="button" aria-pressed="false">Pause</button>
  <button id="refresh" type="button">Refresh</button>
</div>
<noscript><p class="nojs">Live updates need JavaScript — reload the page to refresh.</p></noscript>

<div id="doc">
<header class="band">
  <h1>{{or .Identity.Controller "(no controller code)"}}</h1>
  <p class="sub">location <code>{{or .Identity.Location "—"}}</code> · reader <code>{{.Identity.Reader}}</code> · driver <code>{{.Identity.Driver}}</code>{{if .Identity.Model}} · model <code>{{.Identity.Model}}</code>{{end}}</p>
  <div class="strip">
    {{if eq .Policy.State "synced"}}<span class="badge good">policy synced</span>{{else if eq .Policy.State "cached"}}<span class="badge warn">OFFLINE · cached config</span>{{else}}<span class="badge bad">DEFAULT-DENY · policy not loaded</span>{{end}}
    {{if .NATS.Connected}}<span class="badge good">NATS connected</span>{{else}}<span class="badge bad">NATS disconnected</span>{{end}}
  </div>
</header>

<h2>Identity &amp; connectivity</h2>
<div class="band">
<dl class="kv">
  <dt>controller</dt><dd class="mono">{{or .Identity.Controller "(unset)"}}</dd>
  <dt>location</dt><dd class="mono">{{or .Identity.Location "—"}}</dd>
  <dt>subjects.app</dt><dd class="mono">{{.Identity.SubjectsApp}}</dd>
  <dt>reader / driver</dt><dd class="mono">{{.Identity.Reader}} / {{.Identity.Driver}}{{if .Identity.Model}} ({{.Identity.Model}}){{end}}</dd>
  <dt>NATS</dt><dd class="mono">{{if .NATS.Connected}}{{.NATS.URL}}{{else}}disconnected{{end}} · {{.NATS.Reconnects}} reconnects</dd>
  <dt>uptime</dt><dd class="mono">{{.Identity.Uptime}}</dd>
</dl>
</div>

<h2>Policy</h2>
{{if eq .Policy.State "cached"}}
<p class="notice">Running on cached config from {{.Policy.SyncedAt.Format "2006-01-02 15:04:05 MST"}} — NATS unreachable. Deciding on last-known policy; any changes since then are not applied until the connection is restored.</p>
{{end}}
<div class="stats">
  {{range $k, $v := .Policy.Counts}}
  <div class="stat{{if eq $v 0}} zero{{end}}">
    <div class="n">{{$v}}</div>
    <div class="k">{{label $k}}</div>
  </div>
  {{end}}
</div>

<h2>Bound portals</h2>
{{if not .Portals}}
  <p class="banner">No portals bound to this controller. Check that <code>controller.code</code> ({{or .Identity.Controller "unset"}}) matches a controllers record and that portals are assigned to it in accessd.</p>
{{else}}
<div class="scroll">
<table>
  <thead><tr>
    <th>portal</th><th>type</th><th>state</th><th>posture</th><th>door</th><th>held</th><th>override</th>
    <th>relay</th><th>DPS</th><th>REX</th><th>DOTL</th><th>OSDP</th>
  </tr></thead>
  <tbody>
  {{range .Portals}}
    <tr{{if not .Armed}} class="row-warn"{{end}}>
      <td class="mono">{{.Code}}</td>
      <td>{{.Type}}</td>
      <td>{{if .Armed}}<span class="badge good">armed</span>{{else}}<span class="badge warn">bound, not armed</span>{{end}}</td>
      <td>{{if .Armed}}{{.Posture}} <span class="muted">({{.Source}})</span>{{else}}<span class="muted">—</span>{{end}}</td>
      <td>{{if .Armed}}{{if eq .Door "open"}}<span class="badge warn">open</span>{{else if eq .Door "closed"}}<span class="badge good">closed</span>{{else}}<span class="muted">unknown</span>{{end}}{{else}}<span class="muted">—</span>{{end}}{{if .AuthOpen}} <span class="muted">auth</span>{{end}}</td>
      <td>{{if .Held}}<span class="badge bad">HELD</span>{{else}}<span class="muted">—</span>{{end}}</td>
      <td class="mono">{{or .Override "—"}}</td>
      <td class="mono">{{.LockRelay}}{{if .Maglock}} <span class="muted">maglock</span>{{end}}</td>
      <td class="mono">{{.DpsInput}}{{if gt .DpsInput 0}} <span class="muted">{{if .DpsInvert}}N.O.{{else}}N.C.{{end}}</span>{{end}}</td>
      <td class="mono">{{.RexInput}}{{if gt .RexInput 0}} <span class="muted">{{if .RexInvert}}N.C.{{else}}N.O.{{end}}</span>{{end}}{{if .RexUnlock}} <span class="muted">unlock</span>{{end}}</td>
      <td class="mono">{{.HeldOpenSeconds}}s</td>
      <td class="mono">{{.ReaderAddress}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
</div>
{{end}}

{{if .Fire}}
<h2>Fire inputs (alarm suppression)</h2>
<div class="scroll"><table>
  <thead><tr><th>location</th><th>state</th></tr></thead>
  <tbody>{{range .Fire}}<tr><td class="mono">{{.Location}}</td><td>{{if .Active}}<span class="badge bad">ACTIVE · alarms suppressed</span>{{else}}<span class="muted">clear</span>{{end}}</td></tr>{{end}}</tbody>
</table></div>
{{end}}

{{if or .AuxOutputs .AuxInputs}}
<h2>Aux I/O</h2>
<div class="scroll"><table>
  <thead><tr><th>code</th><th>kind</th><th>location</th><th>state</th></tr></thead>
  <tbody>
  {{range .AuxOutputs}}<tr><td class="mono">{{.Code}}</td><td>output</td><td class="mono">{{.Location}}</td><td>{{if .Energized}}<span class="badge good">energized</span>{{else}}<span class="muted">off</span>{{end}}</td></tr>{{end}}
  {{range .AuxInputs}}<tr><td class="mono">{{.Code}}</td><td>input</td><td class="mono">{{.Location}}</td><td>{{if .Active}}<span class="badge warn">active</span>{{else}}<span class="muted">inactive</span>{{end}}</td></tr>{{end}}
  </tbody>
</table></div>
{{end}}

<h2>Recent decisions</h2>
{{if not .Decisions}}
  <p class="muted">No taps decided yet.</p>
{{else}}
<div class="scroll"><table>
  <thead><tr><th>time (UTC)</th><th>portal</th><th>credential</th><th>user</th><th>result</th><th>reason</th></tr></thead>
  <tbody>
  {{range .Decisions}}
    <tr>
      <td class="mono">{{.At.Format "15:04:05"}}</td>
      <td class="mono">{{.Portal}}</td>
      <td class="mono">{{.Cred}}</td>
      <td class="mono">{{or .User "—"}}</td>
      <td>{{if .Allow}}<span class="badge good">ALLOW</span>{{else}}<span class="badge bad">DENY</span>{{end}}</td>
      <td class="mono">{{.Reason}}</td>
    </tr>
  {{end}}
  </tbody>
</table></div>
{{end}}

{{if .Alarms}}
<h2>Recent alarms</h2>
<div class="scroll"><table>
  <thead><tr><th>time (UTC)</th><th>portal</th><th>kind</th></tr></thead>
  <tbody>{{range .Alarms}}<tr><td class="mono">{{.At.Format "15:04:05"}}</td><td class="mono">{{.Portal}}</td><td>{{.Kind}}</td></tr>{{end}}</tbody>
</table></div>
{{end}}

<footer>
  generated {{.GeneratedAt.Format "2006-01-02 15:04:05 MST"}} ·
  go {{.Build.GoVersion}}{{if .Build.Revision}} · build {{short .Build.Revision}}{{if .Build.Modified}}+dirty{{end}}{{if .Build.Time}} ({{.Build.Time}}){{end}}{{end}}
</footer>
</div><!-- #doc -->

<script>
(function () {
  var INTERVAL = 5000; // in-place refresh cadence; pause halts it, Refresh forces one
  var bar = document.getElementById('bar');
  var freshEl = document.getElementById('fresh');
  var pauseBtn = document.getElementById('pause');
  var refreshBtn = document.getElementById('refresh');
  var paused = false, lastOk = Date.now(), timer = null;

  function ago(ms) {
    var s = Math.round(ms / 1000);
    if (s < 2) return 'just now';
    if (s < 60) return s + 's ago';
    return Math.floor(s / 60) + 'm ' + (s % 60) + 's ago';
  }
  function paint() {
    if (bar.classList.contains('stale')) return; // keep the "connection lost" message
    freshEl.textContent = (paused ? 'paused · ' : 'updated ') + ago(Date.now() - lastOk);
  }
  // refresh always resolves (errors are swallowed into the stale state) so the
  // scheduling chain never stalls and keeps retrying.
  function refresh() {
    return fetch(location.pathname, { cache: 'no-store' })
      .then(function (r) { if (!r.ok) throw new Error('HTTP ' + r.status); return r.text(); })
      .then(function (t) {
        var next = new DOMParser().parseFromString(t, 'text/html').getElementById('doc');
        if (next) document.getElementById('doc').innerHTML = next.innerHTML;
        lastOk = Date.now();
        bar.classList.remove('stale');
        paint();
      })
      .catch(function () {
        bar.classList.add('stale');
        freshEl.textContent = 'connection lost · retrying';
      });
  }
  function schedule() { clearTimeout(timer); if (!paused) timer = setTimeout(tick, INTERVAL); }
  function tick() { refresh().then(schedule); }

  pauseBtn.addEventListener('click', function () {
    paused = !paused;
    pauseBtn.textContent = paused ? 'Resume' : 'Pause';
    pauseBtn.setAttribute('aria-pressed', String(paused));
    if (paused) clearTimeout(timer); else tick();
    paint();
  });
  refreshBtn.addEventListener('click', tick); // one refresh now; reschedules only if running

  setInterval(paint, 1000); // tick the "Ns ago" label between fetches
  paint();
  schedule();
})();
</script>
</body>
</html>
`
