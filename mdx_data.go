package main

import "encoding/json"

// mdx_data.go holds the stylesheets injected into shadow roots (which don't
// inherit the page's tokens, so each carries its own defaults) plus small JSON
// helpers for pretty-printing endpoint/JSON examples.

func jsonUnmarshal(s string, v *any) error { return json.Unmarshal([]byte(s), v) }

func jsonIndent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// jsValueToGo converts a parsed JS literal into plain Go values json.Marshal
// understands (jsObject → map, arrays recurse).
func jsValueToGo(v any) any {
	switch x := v.(type) {
	case *jsObject:
		m := map[string]any{}
		for _, k := range x.keys {
			m[k] = jsValueToGo(x.m[k])
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsValueToGo(e)
		}
		return out
	default:
		return v
	}
}

// diagramBaseCSS provides the visual-plan --wf-* token defaults and base
// styles for the .diagram-* primitives, injected ahead of author CSS inside a
// diagram's shadow root. Tokens live on :host (shadow roots have no :root).
// A prefers-color-scheme block re-tints the defaults so diagrams don't glare
// white on a dark page when the OS is dark.
const diagramBaseCSS = `
:host{
  display:block;
  --wf-ink:#1d2433;
  --wf-muted:#5b6577;
  --wf-line:#d8dee8;
  --wf-paper:#ffffff;
  --wf-card:#f8fafc;
  --wf-accent:#2563eb;
  --wf-accent-fg:#ffffff;
  --wf-radius:8px;
  font-family:system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
  font-size:13px;line-height:1.5;color:var(--wf-ink);
  padding:14px;
}
@media (prefers-color-scheme: dark){
  :host{
    --wf-ink:#e6ebf2;
    --wf-muted:#9aa6b8;
    --wf-line:#38445a;
    --wf-paper:#232e40;
    --wf-card:#1c2636;
    --wf-accent:#8aaedb;
    --wf-accent-fg:#06111d;
  }
}
*{box-sizing:border-box;}
img{max-width:100%;height:auto;}
.diagram-panel{
  display:flex;flex-wrap:wrap;gap:12px;align-items:flex-start;
  padding:14px;background:var(--wf-card);
  border:1px solid var(--wf-line);border-radius:var(--wf-radius);
}
.diagram-node,.diagram-card,.diagram-box{
  background:var(--wf-paper);
  border:1px solid var(--wf-line);
  border-radius:var(--wf-radius);
  padding:10px 12px;
  color:var(--wf-ink);
}
.diagram-card{box-shadow:0 1px 2px rgba(16,24,40,0.04);}
.diagram-node{display:inline-flex;align-items:center;gap:8px;}
.diagram-pill{
  display:inline-flex;align-items:center;gap:5px;
  padding:2px 10px;border-radius:999px;
  background:var(--wf-accent);color:var(--wf-accent-fg);
  font-size:11px;font-weight:600;
}
.diagram-muted{color:var(--wf-muted);font-size:12px;}
`

// htmlBlockBaseCSS is the neutral reset for author HTML in an HtmlBlock's
// shadow root.
const htmlBlockBaseCSS = `
:host { display: block; }
* { box-sizing: border-box; }
:host { font-family: system-ui, -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; font-size: 14px; line-height: 1.5; color: #1d2433; }
img { max-width: 100%; height: auto; }
@media (prefers-color-scheme: dark){ :host { color: #e6ebf2; } }
`

// wireframeBaseCSS styles the wf-* primitive vocabulary plan wireframes use
// (wf-card / wf-pill / wf-muted, --wf-line / --wf-warn tokens, plain buttons),
// scoped inside the wireframe surface's shadow root.
const wireframeBaseCSS = `
:host{
  display:block;height:100%;
  --wf-ink:#1d2433;--wf-muted:#5b6577;--wf-line:#d8dee8;
  --wf-paper:#ffffff;--wf-card:#f8fafc;--wf-accent:#2563eb;--wf-warn:#f59e0b;
  font-family:system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
  font-size:13px;line-height:1.45;color:var(--wf-ink);background:var(--wf-paper);
}
@media (prefers-color-scheme: dark){
  :host{--wf-ink:#e6ebf2;--wf-muted:#9aa6b8;--wf-line:#38445a;--wf-paper:#1c2636;--wf-card:#232e40;--wf-accent:#8aaedb;}
}
*{box-sizing:border-box;}
h2{font-size:16px;margin:0;font-weight:650;}
p{margin:0;}
small{font-size:11.5px;}
.wf-muted{color:var(--wf-muted);}
.wf-card{border:1px solid var(--wf-line);border-radius:8px;background:var(--wf-card);padding:10px 12px;}
.wf-pill{display:inline-flex;align-items:center;gap:5px;padding:2px 9px;border-radius:999px;border:1px solid var(--wf-line);font-size:11px;font-weight:600;white-space:nowrap;}
.wf-pill.accent{background:var(--wf-accent);color:#fff;border-color:var(--wf-accent);}
button{font:inherit;padding:5px 12px;border-radius:6px;border:1px solid var(--wf-line);background:var(--wf-card);color:var(--wf-ink);cursor:default;}
button.primary{background:var(--wf-accent);color:#fff;border-color:var(--wf-accent);}
[data-icon]{display:inline-block;width:16px;height:16px;border-radius:4px;background:var(--wf-line);}
`
