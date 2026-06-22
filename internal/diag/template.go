package diag

import (
	"html/template"
)

// statusTemplate renders the /status page. It is fully self-contained — inline
// CSS, no JavaScript, no external assets — so it works on a box with no network
// and never enters the accessd UI's Vite/embed pipeline. A meta refresh gives a
// live feel (tap a card, watch it appear) without scripting.
var statusTemplate = template.Must(template.New("status").Funcs(template.FuncMap{
	"short": func(s string) string {
		if len(s) > 12 {
			return s[:12]
		}
		return s
	},
}).Parse(statusHTML))

const statusHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="2">
<title>access-controller · {{.Identity.Controller}}</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 14px/1.45 system-ui, sans-serif; margin: 0; padding: 1rem 1.25rem 3rem; }
  h1 { font-size: 1.25rem; margin: 0 0 .15rem; }
  h2 { font-size: .95rem; text-transform: uppercase; letter-spacing: .04em; color: #888; margin: 1.5rem 0 .4rem; }
  .sub { color: #888; margin: 0 0 .75rem; }
  .strip { display: flex; flex-wrap: wrap; gap: .5rem; margin: .5rem 0 0; }
  .badge { display: inline-block; padding: .1rem .5rem; border-radius: .5rem; font-size: .82rem; font-weight: 600; }
  .good { background: #1f7a3d; color: #fff; }
  .bad  { background: #b3261e; color: #fff; }
  .warn { background: #9a6b00; color: #fff; }
  .muted { color: #888; }
  .scroll { overflow-x: auto; }
  table { border-collapse: collapse; width: 100%; font-size: .88rem; }
  th, td { text-align: left; padding: .3rem .55rem; border-bottom: 1px solid #8884; white-space: nowrap; }
  th { color: #888; font-weight: 600; }
  code, .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .banner { background: #b3261e; color: #fff; padding: .75rem 1rem; border-radius: .5rem; font-weight: 600; }
  .row-warn td { background: #9a6b0022; }
  .kv { display: grid; grid-template-columns: max-content 1fr; gap: .15rem 1rem; }
  .kv dt { color: #888; }
  .kv dd { margin: 0; }
  footer { margin-top: 2rem; color: #888; font-size: .8rem; }
</style>
</head>
<body>

<h1>{{or .Identity.Controller "(no controller code)"}}</h1>
<p class="sub">location <code>{{or .Identity.Location "—"}}</code> · reader <code>{{.Identity.Reader}}</code> · driver <code>{{.Identity.Driver}}</code>{{if .Identity.Model}} · model <code>{{.Identity.Model}}</code>{{end}}</p>

<div class="strip">
  {{if .Policy.Synced}}<span class="badge good">policy synced</span>{{else}}<span class="badge bad">DEFAULT-DENY · policy not loaded</span>{{end}}
  {{if .NATS.Connected}}<span class="badge good">NATS connected</span>{{else}}<span class="badge bad">NATS disconnected</span>{{end}}
</div>

<h2>Identity &amp; connectivity</h2>
<dl class="kv">
  <dt>controller</dt><dd class="mono">{{or .Identity.Controller "(unset)"}}</dd>
  <dt>location</dt><dd class="mono">{{or .Identity.Location "—"}}</dd>
  <dt>subjects.app</dt><dd class="mono">{{.Identity.SubjectsApp}}</dd>
  <dt>reader / driver</dt><dd class="mono">{{.Identity.Reader}} / {{.Identity.Driver}}{{if .Identity.Model}} ({{.Identity.Model}}){{end}}</dd>
  <dt>NATS</dt><dd class="mono">{{if .NATS.Connected}}{{.NATS.URL}}{{else}}disconnected{{end}} · {{.NATS.Reconnects}} reconnects</dd>
  <dt>uptime</dt><dd class="mono">{{.Identity.Uptime}}</dd>
</dl>

<h2>Policy</h2>
<p class="mono">
{{range $k, $v := .Policy.Counts}}{{$k}}=<b>{{$v}}</b>&nbsp;&nbsp;{{end}}
</p>

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
      <td class="mono">{{.LockRelay}}</td>
      <td class="mono">{{.DpsInput}}</td>
      <td class="mono">{{.RexInput}}</td>
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
  generated {{.GeneratedAt.Format "2006-01-02 15:04:05 MST"}} · auto-refresh 2s ·
  go {{.Build.GoVersion}}{{if .Build.Revision}} · build {{short .Build.Revision}}{{if .Build.Modified}}+dirty{{end}}{{if .Build.Time}} ({{.Build.Time}}){{end}}{{end}}
</footer>

</body>
</html>
`
