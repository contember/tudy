package llm_resolver

import "html/template"

// pages is the template set rendered for proxy.localhost and its sub-pages.
// All three pages share head + dark hero (topbar / welcome / stat cards) and
// a light content surface below; each page then defines its own content via
// the named template the handler executes.
var pages = template.Must(template.New("pages").Parse(`
{{- define "head" -}}
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tudy</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600;700&family=DM+Sans:ital,wght@0,400;0,500;0,600;0,700;1,400&display=swap" rel="stylesheet">
<style>
:root {
    /* surfaces */
    --bg: #f4f3ee;
    --card: #ffffff;
    --card-soft: #fbfaf6;
    --border: #e8e6e0;
    --border-strong: #d8d5cc;
    --shadow: 0 1px 2px rgba(20, 18, 12, 0.04), 0 8px 24px rgba(20, 18, 12, 0.06);

    /* text on light */
    --text: #1a1816;
    --text-secondary: #5a5650;
    --text-muted: #9a958a;

    /* hero */
    --hero-bg: #0c0c0a;
    --hero-bg-2: #16150f;
    --hero-text: #f5f3ed;
    --hero-text-secondary: #b8b3a5;
    --hero-text-muted: #6a6759;
    --hero-tile: rgba(255, 255, 255, 0.035);
    --hero-tile-strong: rgba(255, 255, 255, 0.05);
    --hero-line: rgba(255, 255, 255, 0.08);

    /* accents */
    --amber: #d4a843;
    --amber-soft: rgba(212, 168, 67, 0.12);
    --amber-deep: #b88a2c;
    --green: #5fb47e;
    --green-soft: rgba(95, 180, 126, 0.14);
    --blue: #5b8ed4;
    --blue-soft: rgba(91, 142, 212, 0.14);
    --red: #d97070;
    --red-soft: rgba(217, 112, 112, 0.14);
    --purple: #9d83cf;
    --purple-soft: rgba(157, 131, 207, 0.14);

    /* fonts */
    --mono: 'JetBrains Mono', 'SF Mono', 'Cascadia Code', monospace;
    --sans: 'DM Sans', system-ui, sans-serif;
}
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
body {
    font-family: var(--sans);
    background: var(--bg);
    color: var(--text);
    line-height: 1.55;
    -webkit-font-smoothing: antialiased;
    min-height: 100vh;
}
a { color: inherit; text-decoration: none; }

.page {
    max-width: 1280px;
    margin: 0 auto;
    padding: 20px 20px 60px;
}

/* ===================== HERO ===================== */
.hero {
    position: relative;
    overflow: hidden;
    background: linear-gradient(135deg, var(--hero-bg) 0%, var(--hero-bg-2) 100%);
    color: var(--hero-text);
    border-radius: 24px;
    padding: 28px 32px 32px;
    margin-bottom: 28px;
    isolation: isolate;
}
.hero::before {
    content: '';
    position: absolute;
    inset: 0;
    pointer-events: none;
    background:
        radial-gradient(ellipse 60% 45% at 18% 18%, rgba(255, 255, 255, 0.04), transparent 65%),
        radial-gradient(ellipse 80% 55% at 90% 28%, rgba(212, 168, 67, 0.05), transparent 60%),
        radial-gradient(ellipse 50% 35% at 70% 95%, rgba(126, 170, 196, 0.04), transparent 60%);
    z-index: -1;
}
.hero::after {
    /* subtle diagonal sheen */
    content: '';
    position: absolute;
    inset: -20% -10%;
    pointer-events: none;
    background: repeating-linear-gradient(
        118deg,
        transparent 0,
        transparent 80px,
        rgba(255, 255, 255, 0.012) 80px,
        rgba(255, 255, 255, 0.012) 200px
    );
    z-index: -1;
}

.topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    margin-bottom: 36px;
}
.brand {
    display: flex; align-items: center; gap: 10px;
    font-family: var(--mono);
    font-size: 16px;
    font-weight: 700;
    color: var(--hero-text);
    letter-spacing: -0.02em;
}
.brand-mark {
    width: 28px; height: 28px;
    border-radius: 8px;
    background: linear-gradient(135deg, var(--amber) 0%, var(--amber-deep) 100%);
    display: flex; align-items: center; justify-content: center;
    color: #1a1816;
    font-family: var(--mono);
    font-weight: 700;
    font-size: 14px;
    box-shadow: 0 4px 12px rgba(212, 168, 67, 0.25);
}
.brand-name { color: var(--hero-text); }
.brand-name span { color: var(--hero-text-muted); font-weight: 400; }

.tabs {
    display: flex;
    gap: 4px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--hero-line);
    border-radius: 999px;
    padding: 4px;
}
.tab {
    padding: 8px 18px;
    font-family: var(--sans);
    font-size: 13px;
    font-weight: 500;
    color: var(--hero-text-secondary);
    border-radius: 999px;
    transition: all 0.15s;
}
.tab:hover { color: var(--hero-text); }
.tab.active {
    color: var(--hero-text);
    background: rgba(255, 255, 255, 0.08);
    font-weight: 600;
}

.topbar-right {
    display: flex; align-items: center; gap: 14px;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--hero-text-secondary);
}
.model-pill {
    display: inline-flex; align-items: center; gap: 8px;
    padding: 6px 12px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--hero-line);
    border-radius: 999px;
    color: var(--hero-text-secondary);
}
.model-pill::before {
    content: '';
    width: 6px; height: 6px;
    border-radius: 50%;
    background: var(--amber);
    box-shadow: 0 0 6px rgba(212, 168, 67, 0.6);
}

.status-dot {
    width: 8px; height: 8px;
    border-radius: 50%;
    background: var(--green);
    box-shadow: 0 0 8px rgba(95, 180, 126, 0.6);
    animation: pulse 2.6s ease-in-out infinite;
}
.status-dot.stale {
    background: var(--hero-text-muted);
    box-shadow: none;
    animation: none;
}
@keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
}

.hero-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: end;
    gap: 32px;
}
.hero-left { min-width: 0; }
.welcome-pill {
    display: inline-flex; align-items: center; gap: 8px;
    padding: 6px 14px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--hero-line);
    border-radius: 999px;
    font-size: 12px;
    color: var(--hero-text-secondary);
    margin-bottom: 16px;
}
.welcome-pill::before {
    content: '';
    width: 6px; height: 6px;
    border-radius: 50%;
    background: var(--green);
    box-shadow: 0 0 6px rgba(95, 180, 126, 0.6);
}
.hero-headline {
    font-family: var(--sans);
    font-size: 38px;
    line-height: 1.1;
    font-weight: 600;
    letter-spacing: -0.025em;
    color: var(--hero-text);
    max-width: 520px;
}
.hero-headline em {
    font-style: italic;
    font-weight: 500;
    color: var(--amber);
}
.hero-sub {
    margin-top: 12px;
    font-size: 14px;
    color: var(--hero-text-secondary);
    max-width: 460px;
}

.stat-grid {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 140px));
    gap: 10px;
}
.stat-card {
    position: relative;
    background: var(--hero-tile);
    border: 1px solid var(--hero-line);
    border-radius: 14px;
    padding: 16px 18px;
    min-height: 116px;
    display: flex; flex-direction: column; justify-content: space-between;
    overflow: hidden;
}
.stat-card-top {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 12px;
}
.stat-value {
    font-family: var(--sans);
    font-size: 30px;
    font-weight: 700;
    color: var(--hero-text);
    line-height: 1;
    letter-spacing: -0.02em;
}
.stat-value.text {
    font-size: 18px;
    text-transform: capitalize;
    font-weight: 600;
}
.stat-icon {
    width: 32px; height: 32px;
    border-radius: 8px;
    display: flex; align-items: center; justify-content: center;
    flex-shrink: 0;
}
.stat-icon svg { width: 16px; height: 16px; }
.stat-icon.amber { background: rgba(212, 168, 67, 0.18); color: var(--amber); }
.stat-icon.green { background: rgba(95, 180, 126, 0.18); color: var(--green); }
.stat-icon.blue { background: rgba(91, 142, 212, 0.18); color: var(--blue); }
.stat-icon.red { background: rgba(217, 112, 112, 0.18); color: var(--red); }
.stat-icon.purple { background: rgba(157, 131, 207, 0.18); color: var(--purple); }
.stat-label {
    font-size: 13px;
    color: var(--hero-text);
    font-weight: 500;
}
.stat-sub {
    font-family: var(--mono);
    font-size: 10px;
    color: var(--hero-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-top: 2px;
}

/* ===================== Banner ===================== */
.banner {
    padding: 14px 18px;
    margin-bottom: 24px;
    border-radius: 14px;
    font-size: 13px;
    background: #fff7e6;
    border: 1px solid #f0d499;
    color: #7d5a1e;
    box-shadow: var(--shadow);
}
.banner strong { font-weight: 700; color: #5a4015; }
.banner code {
    font-family: var(--mono);
    font-size: 12px;
    padding: 2px 6px;
    background: rgba(0,0,0,0.05);
    border-radius: 4px;
    color: #3a2c0e;
}

/* ===================== Card ===================== */
.card {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 18px;
    box-shadow: var(--shadow);
    overflow: hidden;
}
.card + .card { margin-top: 24px; }

.card-head {
    display: flex; align-items: baseline; justify-content: space-between;
    gap: 12px;
    padding: 20px 24px 12px;
}
.card-head-left { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
.card-title {
    font-family: var(--sans);
    font-size: 18px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: -0.01em;
}
.card-count {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 22px; height: 22px;
    padding: 0 7px;
    background: var(--bg);
    border-radius: 999px;
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary);
}
.card-sub {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
}
.card-actions { font-size: 12px; color: var(--text-secondary); }

/* ===================== Routes card ===================== */
.routes-list { display: flex; flex-direction: column; }
.route-group-label {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 24px;
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.1em;
    background: var(--card-soft);
    border-top: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
}
.route-group-label .count {
    color: var(--text-secondary);
    font-weight: 500;
    font-size: 11px;
}

.route {
    display: grid;
    grid-template-columns:
        minmax(0, 1.6fr)    /* hostname + tag */
        minmax(0, 1.4fr)    /* target */
        78px                /* port */
        180px               /* sparkline */
        minmax(130px, auto) /* counters */
        82px                /* last seen */
        36px;               /* delete */
    align-items: center;
    gap: 18px;
    padding: 14px 24px;
    border-bottom: 1px solid var(--border);
    transition: background 0.12s;
}
.route:last-child { border-bottom: none; }
.route:hover { background: var(--card-soft); }
.route.idle .spark-wrap { opacity: 0.45; }

.route-host {
    display: flex; align-items: center; gap: 10px;
    min-width: 0;
}
.route-host a {
    font-family: var(--mono);
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
    border-bottom: 1px solid transparent;
    transition: border-color 0.15s;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.route-host a:hover { border-color: var(--amber); }

.route-target {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
    padding: 4px 8px;
    margin: -4px -8px;
    border-radius: 6px;
}
.route-target:hover { background: var(--amber-soft); color: var(--text); }
.route-target.empty { color: var(--text-muted); font-style: italic; cursor: default; }
.route-target.empty:hover { background: transparent; color: var(--text-muted); }

.route-port {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-secondary);
    text-align: right;
    padding: 4px 6px;
    margin: -4px -6px;
    border-radius: 6px;
}
.route-port.editable { cursor: pointer; }
.route-port.editable:hover { background: var(--amber-soft); color: var(--text); }
.route-port.empty { color: var(--text-muted); opacity: 0.6; }

.spark-wrap {
    position: relative;
    display: block;
    height: 32px;
    width: 100%;
    border-radius: 6px;
    cursor: pointer;
    transition: background 0.15s;
    padding: 0 4px;
}
.spark-wrap:hover { background: var(--amber-soft); }
.spark-wrap::after {
    content: 'view logs →';
    position: absolute;
    top: 50%; right: 6px;
    transform: translateY(-50%);
    font-family: var(--mono);
    font-size: 10px;
    color: var(--amber-deep);
    background: var(--card);
    padding: 2px 6px;
    border-radius: 4px;
    opacity: 0;
    transition: opacity 0.15s;
    pointer-events: none;
    box-shadow: 0 1px 3px rgba(20, 18, 12, 0.1);
}
.spark-wrap:hover::after { opacity: 1; }
.spark-wrap svg { display: block; width: 100%; height: 100%; }
.spark-empty {
    position: absolute; inset: 0;
    display: flex; align-items: center; justify-content: center;
    font-family: var(--mono); font-size: 10px;
    color: var(--text-muted);
    letter-spacing: 0.1em;
    text-transform: uppercase;
}

.route-counters {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-secondary);
    white-space: nowrap;
    display: flex; align-items: baseline; gap: 8px;
}
.route-counters .num { color: var(--text); font-weight: 700; font-size: 13px; }
.route-counters .err { color: var(--red); font-weight: 700; }
.route-counters .err.zero { color: var(--text-muted); font-weight: 400; }
.route-counters .sep { color: var(--text-muted); }

.route-last {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-muted);
    text-align: right;
    white-space: nowrap;
}

.route-delete {
    display: inline-flex; align-items: center; justify-content: center;
    width: 28px; height: 28px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 6px;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s;
}
.route-delete:hover {
    background: var(--red-soft);
    border-color: rgba(217, 112, 112, 0.25);
    color: var(--red);
}
.route-delete svg { width: 14px; height: 14px; }
.route-delete.disabled { visibility: hidden; }

/* ===================== Tags ===================== */
.tag {
    display: inline-flex; align-items: center; gap: 5px;
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 600;
    padding: 3px 9px;
    border-radius: 999px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    flex-shrink: 0;
}
.tag::before { content: ''; width: 5px; height: 5px; border-radius: 50%; }
.tag-process { background: var(--green-soft); color: #2d7a4e; }
.tag-process::before { background: var(--green); }
.tag-docker { background: var(--blue-soft); color: #2f5d99; }
.tag-docker::before { background: var(--blue); }
.tag-info { background: var(--green-soft); color: #2d7a4e; }
.tag-info::before { background: var(--green); }
.tag-warn { background: var(--amber-soft); color: var(--amber-deep); }
.tag-warn::before { background: var(--amber); }
.tag-error { background: var(--red-soft); color: #a64545; }
.tag-error::before { background: var(--red); }
.tag-debug { background: rgba(154, 149, 138, 0.15); color: var(--text-muted); }
.tag-debug::before { background: var(--text-muted); }
.tag-none {
    background: transparent; color: var(--text-muted);
    border: 1px dashed var(--border-strong);
}
.tag-none::before { display: none; }

/* ===================== Editable inputs ===================== */
.route-target.editing, .route-port.editing { padding: 0; margin: 0; }
.route-target.editing input, .route-target.editing select,
.route-port.editing input {
    font-family: var(--mono);
    font-size: 12px;
    background: var(--card);
    border: 1px solid var(--amber);
    border-radius: 6px;
    color: var(--text);
    padding: 5px 8px;
    outline: none;
    box-shadow: 0 0 0 3px var(--amber-soft);
}
.route-target.editing select { width: 100%; min-width: 240px; }
.route-port.editing input { width: 76px; text-align: right; }

/* ===================== Empty state ===================== */
.empty {
    padding: 40px 24px;
    text-align: center;
    color: var(--text-secondary);
}
.empty-hint {
    margin-top: 6px;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-muted);
}
.empty code {
    font-family: var(--mono);
    padding: 1px 6px;
    background: var(--bg);
    border-radius: 4px;
    color: var(--text-secondary);
}

/* ===================== Filter chip ===================== */
.filter-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 16px;
}
.filter-chips .filter-chip { margin-bottom: 0; }
.filter-chip {
    display: inline-flex; align-items: center; gap: 8px;
    padding: 6px 8px 6px 14px;
    margin-bottom: 16px;
    background: var(--amber-soft);
    border: 1px solid rgba(212, 168, 67, 0.3);
    border-radius: 999px;
    font-size: 13px;
    color: var(--text);
    animation: slideUp 0.3s ease-out;
}
.filter-chip-label {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--amber-deep);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: 600;
}
.filter-chip-value {
    font-family: var(--mono);
    font-size: 13px;
    font-weight: 600;
}
.filter-chip-clear {
    display: inline-flex; align-items: center; justify-content: center;
    width: 22px; height: 22px;
    border-radius: 50%;
    background: rgba(212, 168, 67, 0.15);
    color: var(--amber-deep);
    transition: background 0.12s;
}
.filter-chip-clear:hover { background: rgba(212, 168, 67, 0.3); }
.filter-chip-clear svg { width: 12px; height: 12px; }

/* ===================== Logs timeline ===================== */
.timeline-card { padding: 16px 24px 18px; }
.timeline-svg {
    display: block;
    width: 100%;
    height: 72px;
    overflow: visible;
    cursor: crosshair;
    user-select: none;
}
.tl-bar {
    fill: var(--amber);
    opacity: 0.55;
    pointer-events: none;
    transition: opacity 0.12s, fill 0.12s;
}
.tl-bar.tl-bar-selected { opacity: 1; fill: var(--amber-deep); }
.tl-bar-err { fill: var(--red); opacity: 0.9; pointer-events: none; }
.tl-hit {
    fill: transparent;
    transition: fill 0.1s;
}
.tl-hit:hover { fill: rgba(212, 168, 67, 0.06); }
.tl-brush {
    fill: var(--amber);
    opacity: 0.18;
    pointer-events: none;
    stroke: var(--amber-deep);
    stroke-width: 0.5;
    stroke-opacity: 0.5;
}
.timeline-baseline {
    stroke: var(--border-strong);
    stroke-width: 1;
}
.timeline-hint {
    font-family: var(--mono);
    font-size: 10px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-top: 6px;
}

/* ===================== Method + status pills (logs) ===================== */
.method {
    display: inline-flex; align-items: center;
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 700;
    padding: 2px 7px;
    border-radius: 4px;
    letter-spacing: 0.04em;
}
.method-GET     { background: var(--blue-soft);  color: #2f5d99; }
.method-POST    { background: var(--green-soft); color: #2d7a4e; }
.method-PUT     { background: var(--amber-soft); color: var(--amber-deep); }
.method-PATCH   { background: var(--amber-soft); color: var(--amber-deep); }
.method-DELETE  { background: var(--red-soft);   color: #a64545; }
.method-HEAD,
.method-OPTIONS { background: rgba(154, 149, 138, 0.15); color: var(--text-muted); }

.status-pill {
    display: inline-flex; align-items: center;
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 4px;
    min-width: 36px;
    justify-content: center;
}
.status-2xx    { background: var(--green-soft); color: #2d7a4e; }
.status-3xx    { background: var(--blue-soft);  color: #2f5d99; }
.status-4xx    { background: var(--amber-soft); color: var(--amber-deep); }
.status-5xx    { background: var(--red-soft);   color: #a64545; }
.status-other  { background: rgba(154, 149, 138, 0.15); color: var(--text-muted); }

.log-host {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text);
    font-weight: 500;
    border-bottom: 1px solid transparent;
    transition: border-color 0.15s;
    white-space: nowrap;
}
.log-host:hover { border-color: var(--amber); }
.log-path {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-secondary);
    max-width: 360px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.log-dur {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-muted);
    margin-left: 8px;
}

/* ===================== Tables (discovery + logs) ===================== */
table { width: 100%; border-collapse: collapse; }
thead th {
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 12px 18px;
    text-align: left;
    background: var(--card-soft);
    border-bottom: 1px solid var(--border);
    border-top: 1px solid var(--border);
}
thead th:first-child { padding-left: 24px; }
thead th:last-child { padding-right: 24px; }
tbody td {
    font-size: 13px;
    padding: 11px 18px;
    border-bottom: 1px solid var(--border);
    vertical-align: middle;
}
tbody td:first-child { padding-left: 24px; }
tbody td:last-child { padding-right: 24px; }
tbody tr:last-child td { border-bottom: none; }
tbody tr { transition: background 0.12s; }
tbody tr:hover { background: var(--card-soft); }
.cell-mono { font-family: var(--mono); font-size: 12px; color: var(--text-secondary); }
.cell-dim { font-family: var(--mono); font-size: 12px; color: var(--text-muted); }
.cell-strong {
    font-family: var(--mono);
    font-size: 12px;
    font-weight: 700;
    color: var(--text);
}
.cell-cmd {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-secondary);
    max-width: 380px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.cell-dir {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-muted);
    max-width: 280px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: left;
}
.cell-details {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-muted);
    max-width: 340px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* ===================== Animations ===================== */
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
@keyframes slideUp { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }
.hero { animation: fadeIn 0.4s ease-out; }
.banner { animation: slideUp 0.35s ease-out; }
.card { animation: slideUp 0.4s ease-out both; }

/* ===================== Responsive ===================== */
@media (max-width: 1100px) {
    .stat-grid { grid-template-columns: repeat(4, minmax(0, 130px)); }
}
@media (max-width: 980px) {
    .hero-body { grid-template-columns: 1fr; }
    .stat-grid { grid-template-columns: repeat(4, 1fr); }
}
@media (max-width: 720px) {
    .page { padding: 12px 12px 60px; }
    .hero { padding: 20px 20px 24px; border-radius: 18px; }
    .hero-headline { font-size: 28px; }
    .topbar { flex-direction: column; align-items: flex-start; gap: 14px; }
    .tabs { width: 100%; justify-content: stretch; }
    .tab { flex: 1; text-align: center; padding: 8px 10px; }
    .stat-grid { grid-template-columns: repeat(2, 1fr); }
    .route {
        grid-template-columns: 1fr auto;
        gap: 8px 12px;
        padding: 14px 18px;
    }
    .route-host { grid-column: 1 / -1; }
    .route-target { grid-column: 1; }
    .route-port { grid-column: 2; }
    .spark-wrap { grid-column: 1 / -1; }
    .route-counters { grid-column: 1; }
    .route-last { grid-column: 2; }
    .route-delete { grid-column: 1 / -1; justify-self: end; }
}
</style>
{{- end -}}

{{- /* ====================== Shared chrome (hero) ====================== */ -}}
{{- define "chrome-hero" -}}
<div class="hero">
    <div class="topbar">
        <a href="/" class="brand">
            <span class="brand-mark">t</span>
            <span class="brand-name">tudy <span>// proxy</span></span>
        </a>
        <nav class="tabs">
            <a href="/" class="tab{{ if eq .Page "activity" }} active{{ end }}">Activity</a>
            <a href="/discovery" class="tab{{ if eq .Page "discovery" }} active{{ end }}">Discovery</a>
            <a href="/logs" class="tab{{ if eq .Page "logs" }} active{{ end }}">Logs</a>
        </nav>
        <div class="topbar-right">
            <span class="model-pill">{{ .Model }}</span>
            <span class="status-dot" id="status-dot" title="Connected"></span>
        </div>
    </div>
    <div class="hero-body">
        <div class="hero-left">
            <div class="welcome-pill">{{ .Welcome }}</div>
            <div class="hero-headline">{{ .Headline }}</div>
            {{ if .HeadlineSub }}<div class="hero-sub">{{ .HeadlineSub }}</div>{{ end }}
        </div>
        {{ if .Stats }}
        <div class="stat-grid">
            {{ range .Stats }}
            <div class="stat-card" data-stat-label="{{ .Label }}">
                <div class="stat-card-top">
                    <div class="stat-value{{ if not (or (eq .Value "0") (eq .Value "1") (eq .Value "2") (eq .Value "3") (eq .Value "4") (eq .Value "5") (eq .Value "6") (eq .Value "7") (eq .Value "8") (eq .Value "9")) }}{{ end }}" data-stat-value>{{ .Value }}</div>
                    <div class="stat-icon {{ .Color }}">{{ .IconSVG }}</div>
                </div>
                <div>
                    <div class="stat-label">{{ .Label }}</div>
                    <div class="stat-sub">{{ .Sub }}</div>
                </div>
            </div>
            {{ end }}
        </div>
        {{ end }}
    </div>
</div>
{{- end -}}

{{- define "chrome-banner" -}}
{{ if eq .TunnelStatus "broken" }}
<div class="banner">
    <strong>Docker tunnel broken.</strong>
    docker-mac-net-connect is running but its WireGuard peer inside Docker Desktop's VM is gone &mdash; usually after Docker restarts or upgrades. Container IPs aren't routable until dmnc is restarted.<br>
    Restart now: <code>sudo brew services restart docker-mac-net-connect</code><br>
    Prevent recurrence: <code>sudo scripts/install-dmnc-healer.sh</code>
</div>
{{ end }}
{{- end -}}

{{- /* ====================== Main dashboard ====================== */ -}}
{{- define "dashboard" -}}
<!DOCTYPE html>
<html lang="en">
<head>{{ template "head" . }}</head>
<body>
<div class="page">
    {{ template "chrome-hero" . }}
    {{ template "chrome-banner" . }}

    <div class="card">
        <div class="card-head">
            <div class="card-head-left">
                <div class="card-title">Routes</div>
                <div class="card-count" id="routes-card-count">{{ len .Snapshot.Routes }}</div>
            </div>
            <div class="card-sub">last 5 min &middot; live</div>
        </div>
        <div id="routes-panel">
            <div class="empty" id="routes-empty" style="display:none">
                No routes yet.
                <div class="empty-hint">Visit a <code>*.localhost</code> domain to create one.</div>
            </div>
            <div class="routes-list" id="routes-list"></div>
        </div>
    </div>
</div>

<script id="initial-snapshot" type="application/json">{{ .Snapshot.JSONForTemplate }}</script>
<script>
(function () {
    const dot = document.getElementById('status-dot');
    const list = document.getElementById('routes-list');
    const emptyEl = document.getElementById('routes-empty');
    const countEl = document.getElementById('routes-card-count');

    const initial = JSON.parse(document.getElementById('initial-snapshot').textContent);
    let availableTargets = initial.availableTargets || [];
    let bucketCount = initial.bucketCount || 30;

    function setStale(stale) {
        dot.classList.toggle('stale', stale);
        dot.title = stale ? 'Disconnected (refresh to retry)' : 'Connected';
    }

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
        }[c]));
    }

    function sparklineSVG(buckets) {
        if (!buckets || buckets.length === 0) return '';
        const w = 180, h = 32;
        const n = buckets.length;
        const stepX = w / Math.max(1, n - 1);
        let max = 0;
        for (const b of buckets) if (b.requests > max) max = b.requests;
        if (max === 0) return '<div class="spark-empty">idle</div>';
        const usableH = h - 5;
        let line = '';
        for (let i = 0; i < n; i++) {
            const x = i * stepX;
            const y = h - 2 - (buckets[i].requests / max) * usableH;
            line += (i === 0 ? 'M' : 'L') + x.toFixed(1) + ',' + y.toFixed(1);
        }
        const area = line + ' L' + w.toFixed(1) + ',' + h + ' L0,' + h + ' Z';
        let errs = '';
        for (let i = 0; i < n; i++) {
            if (buckets[i].errors > 0) {
                const x = i * stepX;
                errs += '<circle cx="' + x.toFixed(1) + '" cy="' + (h - 2) + '" r="2.4" fill="var(--red)"></circle>';
            }
        }
        return '<svg viewBox="0 0 ' + w + ' ' + h + '" preserveAspectRatio="none">' +
            '<path d="' + area + '" fill="var(--amber-soft)"></path>' +
            '<path d="' + line + '" fill="none" stroke="var(--amber)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"></path>' +
            errs +
            '</svg>';
    }

    function renderRoute(r) {
        const idleClass = r.active ? '' : ' idle';
        const tag = r.hasMapping
            ? '<span class="tag ' + esc(r.tagClass) + '">' + esc(r.type) + '</span>'
            : '<span class="tag tag-none">unmapped</span>';

        const target = r.hasMapping
            ? '<div class="route-target" data-action="edit-target">' + esc(r.target) + '</div>'
            : '<div class="route-target empty">no mapping</div>';

        let port;
        if (!r.hasMapping) {
            port = '<div class="route-port empty">&mdash;</div>';
        } else if (r.portEditable) {
            port = '<div class="route-port editable" data-action="edit-port">' + esc(r.port) + '</div>';
        } else {
            port = '<div class="route-port">' + esc(r.port) + '</div>';
        }

        const spark = '<a class="spark-wrap" href="/logs?host=' + encodeURIComponent(r.hostname) + '" title="View logs for ' + esc(r.hostname) + '">' + sparklineSVG(r.buckets) + '</a>';

        const errCls = r.windowErr > 0 ? 'err' : 'err zero';
        const counters = '<div class="route-counters">' +
            '<span><span class="num">' + r.windowReq + '</span> req</span>' +
            '<span class="sep">&middot;</span>' +
            '<span class="' + errCls + '">' + r.windowErr + ' err</span>' +
            '</div>';

        const last = '<div class="route-last">' + esc(r.lastSeenAgo || '—') + '</div>';

        const del = r.hasMapping
            ? '<button class="route-delete" data-action="delete" title="Remove mapping">' +
              '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6">' +
              '<line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg></button>'
            : '<div class="route-delete disabled"></div>';

        const node = document.createElement('div');
        node.className = 'route' + idleClass;
        node.dataset.hostname = r.hostname;
        node.dataset.type = r.type || '';
        node.dataset.target = r.target || '';
        node.dataset.port = String(r.port || 0);
        node.dataset.hasMapping = r.hasMapping ? '1' : '';
        node.innerHTML =
            '<div class="route-host"><a href="https://' + esc(r.hostname) + '" target="_blank">' + esc(r.hostname) + '</a>' + tag + '</div>' +
            target + port + spark + counters + last + del;
        return node;
    }

    function setText(el, text) {
        if (el && el.textContent !== text) el.textContent = text;
    }
    function setClass(el, cls) {
        if (el && el.className !== cls) el.className = cls;
    }

    function renderHeroStats(snap) {
        if (!snap.totals) return;
        const t = snap.totals;
        const map = {
            'Routes': t.routes,
            'Active': t.active,
            'Requests': t.windowReq,
            'Errors': t.windowErr,
        };
        document.querySelectorAll('.stat-card').forEach(card => {
            const label = card.dataset.statLabel;
            if (label in map) {
                setText(card.querySelector('[data-stat-value]'), String(map[label]));
            }
        });
    }

    function makeLabel(item) {
        const el = document.createElement('div');
        el.className = 'route-group-label';
        el.innerHTML = '<span></span><span class="count"></span>';
        updateLabel(el, item);
        return el;
    }
    function updateLabel(el, item) {
        const spans = el.querySelectorAll('span');
        setText(spans[0], item.label);
        setText(spans[1], String(item.count));
    }

    function updateRow(row, r) {
        const tag = row.querySelector('.tag');
        if (tag) {
            setClass(tag, 'tag ' + (r.hasMapping ? r.tagClass : 'tag-none'));
            setText(tag, r.hasMapping ? r.type : 'unmapped');
        }
        const tgt = row.querySelector('.route-target');
        if (tgt && !tgt.querySelector('select, input')) {
            setText(tgt, r.hasMapping ? r.target : 'no mapping');
            tgt.classList.toggle('empty', !r.hasMapping);
        }
        const port = row.querySelector('.route-port');
        if (port && !port.querySelector('input')) {
            setText(port, r.hasMapping ? String(r.port) : '—');
            port.classList.toggle('empty', !r.hasMapping);
            port.classList.toggle('editable', !!(r.hasMapping && r.portEditable));
            port.dataset.action = (r.hasMapping && r.portEditable) ? 'edit-port' : '';
        }
        const spark = row.querySelector('.spark-wrap');
        if (spark) {
            const href = '/logs?host=' + encodeURIComponent(r.hostname);
            if (spark.getAttribute('href') !== href) spark.setAttribute('href', href);
            spark.innerHTML = sparklineSVG(r.buckets);
        }
        const num = row.querySelector('.route-counters .num');
        setText(num, String(r.windowReq));
        const err = row.querySelector('.route-counters .err');
        if (err) {
            setClass(err, r.windowErr > 0 ? 'err' : 'err zero');
            setText(err, r.windowErr + ' err');
        }
        setText(row.querySelector('.route-last'), r.lastSeenAgo || '—');
        row.classList.toggle('idle', !r.active);
        row.dataset.type = r.type || '';
        row.dataset.target = r.target || '';
        row.dataset.port = String(r.port || 0);
        row.dataset.hasMapping = r.hasMapping ? '1' : '';
    }

    function reconcile(desired) {
        const existing = new Map();
        for (const el of Array.from(list.children)) existing.set(el.dataset.key, el);

        let cursor = list.firstChild;
        for (const item of desired) {
            let el = existing.get(item.key);
            if (!el) {
                el = item.type === 'label' ? makeLabel(item) : renderRoute(item.route);
                el.dataset.key = item.key;
            } else {
                existing.delete(item.key);
                if (item.type === 'label') updateLabel(el, item);
                else updateRow(el, item.route);
            }
            if (el !== cursor) list.insertBefore(el, cursor);
            cursor = el.nextSibling;
        }
        // Anything left in the existing map is stale — drop unless mid-edit.
        for (const el of existing.values()) {
            if (!el.querySelector('select, input')) el.remove();
        }
    }

    function render(snap) {
        availableTargets = snap.availableTargets || availableTargets;
        bucketCount = snap.bucketCount || bucketCount;

        renderHeroStats(snap);

        const routes = (snap.routes || []).slice().sort((a, b) => {
            if (a.active !== b.active) return a.active ? -1 : 1;
            if (a.hasMapping !== b.hasMapping) return a.hasMapping ? -1 : 1;
            return a.hostname.localeCompare(b.hostname);
        });

        const mappingCount = routes.filter(r => r.hasMapping).length;
        setText(countEl, String(mappingCount));

        if (routes.length === 0) {
            emptyEl.style.display = '';
            // empty the list but only if no edit-in-progress
            for (const el of Array.from(list.children)) {
                if (!el.querySelector('select, input')) el.remove();
            }
            return;
        }
        emptyEl.style.display = 'none';

        const active = routes.filter(r => r.active);
        const idle = routes.filter(r => !r.active);
        const desired = [];
        if (active.length) {
            desired.push({type: 'label', key: 'lbl-active', label: 'Active', count: active.length});
            for (const r of active) desired.push({type: 'route', key: 'route-' + r.hostname, route: r});
        }
        if (idle.length) {
            desired.push({type: 'label', key: 'lbl-idle', label: 'Idle', count: idle.length});
            for (const r of idle) desired.push({type: 'route', key: 'route-' + r.hostname, route: r});
        }
        reconcile(desired);
    }

    render(initial);

    list.addEventListener('click', (e) => {
        const target = e.target.closest('[data-action]');
        if (!target) return;
        const row = target.closest('.route');
        if (!row) return;
        const action = target.dataset.action;
        if (action === 'edit-target') editTarget(target, row);
        else if (action === 'edit-port') editPort(target, row);
        else if (action === 'delete') deleteMapping(row);
    });

    function editTarget(cell, row) {
        if (cell.querySelector('select')) return;
        const original = cell.textContent;
        const select = document.createElement('select');
        const cur = document.createElement('option');
        cur.value = ''; cur.textContent = row.dataset.target; cur.selected = true;
        select.appendChild(cur);
        const procs = availableTargets.filter(t => t.type === 'process');
        if (procs.length) {
            const g = document.createElement('optgroup'); g.label = 'Processes';
            procs.forEach(t => {
                const o = document.createElement('option');
                o.value = JSON.stringify(t); o.textContent = t.label;
                g.appendChild(o);
            });
            select.appendChild(g);
        }
        const dockers = availableTargets.filter(t => t.type === 'docker');
        if (dockers.length) {
            const g = document.createElement('optgroup'); g.label = 'Containers';
            dockers.forEach(t => {
                const o = document.createElement('option');
                o.value = JSON.stringify(t); o.textContent = t.label;
                g.appendChild(o);
            });
            select.appendChild(g);
        }
        cell.classList.add('editing');
        cell.textContent = '';
        cell.appendChild(select);
        select.focus();
        const cancel = () => { cell.classList.remove('editing'); cell.textContent = original; };
        select.onchange = () => { if (select.value) saveMapping(row, JSON.parse(select.value)); else cancel(); };
        select.onkeydown = (e) => { if (e.key === 'Escape') cancel(); };
        select.onblur = () => setTimeout(() => { if (cell.contains(select)) cancel(); }, 150);
    }

    function editPort(cell, row) {
        if (cell.querySelector('input')) return;
        const original = cell.textContent;
        const current = parseInt(row.dataset.port, 10) || 0;
        const input = document.createElement('input');
        input.type = 'number'; input.value = current; input.min = 1; input.max = 65535;
        cell.classList.add('editing');
        cell.textContent = '';
        cell.appendChild(input);
        input.focus(); input.select();
        const save = () => {
            const np = parseInt(input.value, 10);
            if (np && np !== current) {
                saveMapping(row, { type: row.dataset.type, target: row.dataset.target, port: np });
            } else {
                cell.classList.remove('editing');
                cell.textContent = original;
            }
        };
        input.onkeydown = (e) => {
            if (e.key === 'Enter') { e.preventDefault(); save(); }
            if (e.key === 'Escape') { cell.classList.remove('editing'); cell.textContent = original; }
        };
        input.onblur = save;
    }

    async function saveMapping(row, data) {
        row.style.opacity = '0.5';
        const resp = await fetch('/_api/mappings/' + encodeURIComponent(row.dataset.hostname), {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({type: data.type, target: data.target, port: data.port}),
        });
        if (!resp.ok) { row.style.opacity = '1'; alert('Failed to update mapping'); }
    }

    async function deleteMapping(row) {
        if (!confirm('Remove route mapping for ' + row.dataset.hostname + '?')) return;
        row.style.opacity = '0.3';
        const resp = await fetch('/_api/mappings/' + encodeURIComponent(row.dataset.hostname), { method: 'DELETE' });
        if (!resp.ok) { row.style.opacity = '1'; alert('Failed to delete mapping'); }
    }

    let es;
    function connect() {
        try { es = new EventSource('/_events'); } catch (e) { setStale(true); return; }
        es.onopen = () => setStale(false);
        es.onmessage = (e) => {
            try { render(JSON.parse(e.data)); setStale(false); } catch (err) {}
        };
        es.onerror = () => setStale(true);
    }
    connect();
})();
</script>
</body>
</html>
{{- end -}}

{{- /* ====================== Discovery page ====================== */ -}}
{{- define "discovery" -}}
<!DOCTYPE html>
<html lang="en">
<head>{{ template "head" . }}</head>
<body>
<div class="page">
    {{ template "chrome-hero" . }}
    {{ template "chrome-banner" . }}

    <div class="card">
        <div class="card-head">
            <div class="card-head-left">
                <div class="card-title">Local Processes</div>
                <div class="card-count">{{ len .Processes }}</div>
            </div>
            <div class="card-sub">listening on a port</div>
        </div>
{{- if eq (len .Processes) 0 }}
        <div class="empty">No local processes detected.</div>
{{- else }}
        <table>
            <thead><tr><th>Port</th><th>Command</th><th>Directory</th></tr></thead>
            <tbody>
{{- range .Processes }}
            <tr>
                <td class="cell-strong">:{{ .Port }}</td>
                <td class="cell-cmd" title="{{ .Command }}">{{ .Command }}</td>
                <td class="cell-dir" title="{{ .Workdir }}">{{ .Workdir }}</td>
            </tr>
{{- end }}
            </tbody>
        </table>
{{- end }}
    </div>

    <div class="card">
        <div class="card-head">
            <div class="card-head-left">
                <div class="card-title">Docker Containers</div>
                <div class="card-count">{{ len .Containers }}</div>
            </div>
            <div class="card-sub">running</div>
        </div>
{{- if eq (len .Containers) 0 }}
        <div class="empty">No Docker containers detected.</div>
{{- else }}
        <table>
            <thead><tr><th>Name</th><th>Image</th><th>Ports</th><th>IP</th><th>Directory</th></tr></thead>
            <tbody>
{{- range .Containers }}
            <tr>
                <td class="cell-strong">{{ .Name }}</td>
                <td class="cell-mono">{{ .Image }}</td>
                <td class="cell-dim">{{ .Ports }}</td>
                <td class="cell-mono">{{ .IP }}</td>
                <td class="cell-dir" title="{{ .Workdir }}">{{ .Workdir }}</td>
            </tr>
{{- end }}
            </tbody>
        </table>
{{- end }}
    </div>
</div>
</body>
</html>
{{- end -}}

{{- /* ====================== Logs page ====================== */ -}}
{{- define "logs" -}}
<!DOCTYPE html>
<html lang="en">
<head>{{ template "head" . }}</head>
<body>
<div class="page">
    {{ template "chrome-hero" . }}
    {{ template "chrome-banner" . }}

    {{ if or .FilterHost .FilterTimeLabel }}
    <div class="filter-chips">
        {{ if .FilterHost }}
        <a class="filter-chip" href="{{ .ClearHostURL }}" title="Clear host filter">
            <span class="filter-chip-label">Host</span>
            <span class="filter-chip-value">{{ .FilterHost }}</span>
            <span class="filter-chip-clear" aria-label="Clear">
                <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
            </span>
        </a>
        {{ end }}
        {{ if .FilterTimeLabel }}
        <a class="filter-chip" href="{{ .ClearTimeURL }}" title="Clear time filter">
            <span class="filter-chip-label">Time</span>
            <span class="filter-chip-value">{{ .FilterTimeLabel }}</span>
            <span class="filter-chip-clear" aria-label="Clear">
                <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
            </span>
        </a>
        {{ end }}
    </div>
    {{ end }}

    {{ if .Timeline }}
    <div class="card timeline-card" id="timeline-card">
        <div class="card-head" style="padding: 0 0 12px;">
            <div class="card-head-left">
                <div class="card-title">Timeline</div>
                <div class="card-sub">{{ .Timeline.StartLabel }} → {{ .Timeline.EndLabel }} &middot; drag to select a range</div>
            </div>
        </div>
        <svg id="timeline-svg" class="timeline-svg"
             viewBox="0 0 {{ printf "%.0f" .Timeline.ViewBoxW }} {{ .Timeline.ViewBoxH }}"
             preserveAspectRatio="none"
             data-bucket-count="{{ .Timeline.BucketCount }}"
             data-stride="{{ printf "%.2f" .Timeline.BarStride }}"
             data-viewbox-w="{{ printf "%.0f" .Timeline.ViewBoxW }}"
             data-viewbox-h="{{ .Timeline.ViewBoxH }}">
            <line class="timeline-baseline" x1="0" y1="{{ .Timeline.ViewBoxH }}" x2="{{ printf "%.0f" .Timeline.ViewBoxW }}" y2="{{ .Timeline.ViewBoxH }}" />
            {{ range $i, $b := .Timeline.Buckets }}
            <rect class="tl-hit" data-idx="{{ $i }}"
                  x="{{ printf "%.2f" $b.BarX }}" y="0"
                  width="{{ $.Timeline.BarStride }}" height="{{ $.Timeline.ViewBoxH }}">
                <title>{{ $b.Total }} entries{{ if gt $b.Errors 0 }} ({{ $b.Errors }} error{{ if gt $b.Errors 1 }}s{{ end }}){{ end }}</title>
            </rect>
            {{ end }}
            {{ range .Timeline.Buckets }}
            {{ if gt .Total 0 }}
            <rect class="tl-bar{{ if .Selected }} tl-bar-selected{{ end }}"
                  x="{{ printf "%.2f" .BarX }}" y="{{ printf "%.2f" .BarY }}"
                  width="{{ $.Timeline.BarWidth }}" height="{{ printf "%.2f" .BarH }}" />
            {{ if gt .Errors 0 }}
            <rect class="tl-bar-err"
                  x="{{ printf "%.2f" .BarX }}" y="{{ printf "%.2f" .ErrY }}"
                  width="{{ $.Timeline.BarWidth }}" height="{{ printf "%.2f" .ErrH }}" />
            {{ end }}
            {{ end }}
            {{ end }}
        </svg>
        <script id="timeline-data" type="application/json">{{ .Timeline.BucketsJSON }}</script>
    </div>
    {{ end }}

    <div class="card">
        <div class="card-head">
            <div class="card-head-left">
                <div class="card-title">{{ if .FilterHost }}Logs for {{ .FilterHost }}{{ else }}Recent Logs{{ end }}</div>
                <div class="card-count">{{ len .LogEntries }}</div>
            </div>
            <div class="card-sub">newest first</div>
        </div>
{{- if eq (len .LogEntries) 0 }}
        <div class="empty">{{ if .FilterHost }}No log entries for <code>{{ .FilterHost }}</code> yet.{{ else }}No log entries yet.{{ end }}</div>
{{- else }}
        <table>
            <thead><tr><th>Time</th><th>Method</th><th>Host</th><th>Path</th><th>Status</th><th>Details</th></tr></thead>
            <tbody>
{{- range .LogEntries }}
{{- if .IsRequest }}
            <tr>
                <td class="cell-dim">{{ .Time }}</td>
                <td><span class="method method-{{ .Method }}">{{ .Method }}</span></td>
                <td><a href="/logs?host={{ .Host }}" class="log-host">{{ .Host }}</a></td>
                <td class="log-path" title="{{ .Path }}">{{ .Path }}</td>
                <td><span class="status-pill {{ .StatusClass }}">{{ .Status }}</span></td>
                <td class="cell-dim">{{ .Duration }}</td>
            </tr>
{{- else }}
            <tr>
                <td class="cell-dim">{{ .Time }}</td>
                <td><span class="tag {{ .TagClass }}">{{ .Level }}</span></td>
                <td class="cell-mono" colspan="3">{{ .Message }}</td>
                <td class="cell-details" title="{{ .Details }}">{{ .Details }}</td>
            </tr>
{{- end }}
{{- end }}
            </tbody>
        </table>
{{- end }}
    </div>
</div>
<script>
(function () {
    const svg = document.getElementById('timeline-svg');
    if (!svg) return;
    const dataNode = document.getElementById('timeline-data');
    let buckets;
    try { buckets = JSON.parse(dataNode.textContent); } catch (e) { return; }
    const N = parseInt(svg.dataset.bucketCount, 10);
    const vbW = parseFloat(svg.dataset.viewboxW);
    const vbH = parseFloat(svg.dataset.viewboxH);
    const stride = parseFloat(svg.dataset.stride);
    if (!N || !buckets || buckets.length !== N) return;

    const SVG_NS = 'http://www.w3.org/2000/svg';
    let brush = null;
    let dragFrom = null;
    let dragTo = null;

    function bucketIndexFromEvent(ev) {
        const rect = svg.getBoundingClientRect();
        const xRel = ev.clientX - rect.left;
        const xFrac = Math.max(0, Math.min(1, xRel / rect.width));
        return Math.max(0, Math.min(N - 1, Math.floor(xFrac * N)));
    }

    function ensureBrush() {
        if (brush) return brush;
        brush = document.createElementNS(SVG_NS, 'rect');
        brush.setAttribute('class', 'tl-brush');
        brush.setAttribute('y', '0');
        brush.setAttribute('height', String(vbH));
        svg.appendChild(brush);
        return brush;
    }

    function drawBrush(lo, hi) {
        const r = ensureBrush();
        r.setAttribute('x', String(lo * stride));
        r.setAttribute('width', String((hi - lo + 1) * stride));
    }

    function clearBrush() {
        if (brush) { brush.remove(); brush = null; }
    }

    function applySelection(lo, hi) {
        const fromUnix = buckets[lo][0];
        const toUnix = buckets[hi][1];
        const next = new URLSearchParams(window.location.search);
        next.set('from', String(fromUnix));
        next.set('to', String(toUnix));
        window.location.href = window.location.pathname + '?' + next.toString();
    }

    svg.addEventListener('mousedown', (e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        dragFrom = bucketIndexFromEvent(e);
        dragTo = dragFrom;
        drawBrush(dragFrom, dragTo);
    });
    window.addEventListener('mousemove', (e) => {
        if (dragFrom == null) return;
        dragTo = bucketIndexFromEvent(e);
        const lo = Math.min(dragFrom, dragTo);
        const hi = Math.max(dragFrom, dragTo);
        drawBrush(lo, hi);
    });
    window.addEventListener('mouseup', (e) => {
        if (dragFrom == null) return;
        const lo = Math.min(dragFrom, dragTo);
        const hi = Math.max(dragFrom, dragTo);
        dragFrom = null;
        dragTo = null;
        clearBrush();
        applySelection(lo, hi);
    });
    // Cancel an in-progress drag if the user presses Escape.
    window.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && dragFrom != null) {
            dragFrom = null;
            dragTo = null;
            clearBrush();
        }
    });
})();
</script>
</body>
</html>
{{- end -}}
`))
