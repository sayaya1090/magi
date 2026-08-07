package main

// indexHTML is the whole front end: one file, no build step, nothing fetched at load.
//
// A framework would mean a bundler, a lockfile and a second toolchain for a transcript and a text
// box — and magi is one static binary precisely so there is nothing to install. The page is also
// readable end to end, which matters for something that puts a working directory's contents on a
// port.
//
// # Two views, one document
//
// `/` is the fleet: every daemon this config directory knows about. `/?d=<socket>` is one of them,
// with its transcript and a composer. Same page, switched by the query string and pushState, so
// entering an agent and coming back is instant and there is only ever one idea of what magi looks
// like. The server tells them apart the same way — see server.target.
//
// # The colours are magi's, not a second scheme
//
// Every value below is lifted from styles.go: the same Material Design 3 colour ROLES themed after
// NERV/MAGI — amber primary, cyan accent, warm dark surface — and the same three councillor hues.
// A browser view that invented its own palette would be a second thing to keep in step, and the
// first time somebody retuned the terminal the two would disagree about what magi looks like.
//
// Kept as CSS custom properties with the role names the Go side uses (primary, accent, muted,
// outline, surface, primaryContainer, outlineVariant, error, success, warn), so the correspondence
// is checkable by reading rather than by remembering.
//
// # Phones are not a smaller desktop
//
// The page is installable — a manifest and an icon, so adding it to a home screen opens it without
// browser chrome. That is the whole of magi's answer to the native mobile app other orchestrators
// ship: the thing you actually want on a phone is to see what your agents are doing and to say one
// sentence to one of them, and a page that launches full screen does that without a second client
// to keep in step with the daemon protocol.
//
// The layout starts at one column and grows, rather than starting wide and collapsing. Three things
// on a phone are not preferences: inputs at 16px (anything smaller makes iOS Safari zoom the page
// on focus, and it does not zoom back), the composer padded by env(safe-area-inset-bottom) (a home
// indicator otherwise sits on top of the send button), and controls at least 44px tall. The
// transcript's label column folds above its text below 640px, because five and a half characters of
// gutter is most of a narrow screen.
//
// Nothing is interpolated into this string. It used to substitute a session id, and the format verb
// that did it read `width:100%` as a verb and broke the build; there is now nothing to substitute
// because the page learns what it is looking at from /fleet.
const indexHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<meta name="theme-color" content="#14110d">
<link rel="manifest" href="/manifest.webmanifest">
<link rel="icon" href="/icon.svg">
<link rel="apple-touch-icon" href="/icon.svg">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-title" content="magi">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<title>magi</title>
<style>
  /* Roles, verbatim from internal/adapter/tui/styles.go — nervDark / nervLight. */
  :root {
    color-scheme: dark light;
    --primary:#FF7A1A; --accent:#5CD8E6; --muted:#C9C2B8; --outline:#5A5048;
    --error:#F2B8B5; --success:#86EFAC; --surface:#211B14;
    --primaryContainer:#4A2E0B; --outlineVariant:#463E34; --warn:#FFD479;
    /* The three council members' colours. Declared and unused HERE: the palette is the terminal's
       and a test requires this page to carry every role of it, so that retuning one surface can
       never leave the two disagreeing. The console shows no council, so nothing paints with them —
       which is a contract kept, not a leftover. */
    --melchior:#FFB454; --balthasar:#5CD8E6; --casper:#FF8A8A;
    --bg:#14110d; --fg:#E8E2D8;

    /* ── M3, dark ─────────────────────────────────────────────────────────── */
    /* The roles above are the terminal's, verbatim (a test pins them). These are the Material 3
       roles the terminal has no use for: a TUI paints on a background it does not own, so it
       cannot have tonal surfaces, and it never needed an on- pair because it draws text on one
       background. A browser has both, and without them "Material 3" would be a set of borrowed
       names — which is exactly what this page was until it was measured. */
    --md-on-primary:#2A1500;              /* on #FF7A1A */
    --md-on-primary-container:#FFD9B8;    /* on #4A2E0B */
    --md-on-error:#3A0A08;
    --md-on-surface:#E8E2D8;
    --md-on-surface-variant:#C9C2B8;
    /* Tonal layers, low → high. Dark themes get LIGHTER as they rise. */
    --md-surface-dim:#14110d;
    --md-surface-container-lowest:#0F0D0A;
    --md-surface-container-low:#1B1712;
    --md-surface-container:#211B14;
    --md-surface-container-high:#2B251C;
    --md-surface-container-highest:#352E24;
  }
  @media (prefers-color-scheme: light) {
    :root {
      --primary:#B45309; --accent:#0E7490; --muted:#4A453C; --outline:#8A7E6E;
      --error:#B3261E; --success:#15803D; --surface:#F5EEE3;
      --primaryContainer:#F8D9A8; --outlineVariant:#D8CFC0; --warn:#92600A;
      --melchior:#B45309; --balthasar:#0E7490; --casper:#B3261E;
      --bg:#FBF8F3; --fg:#221D16;

      /* ── M3, light ─────────────────────────────────────────────────────── */
      /* The layers invert: a light theme gets DARKER as it rises. Built as its own ramp rather
         than by dimming the dark one — a light theme has less headroom, and this page has been
         caught before with eight of thirteen dimmed pairs under AA, the worst at 2.47:1. */
      --md-on-primary:#FFFFFF;
      --md-on-primary-container:#3A1B00;
      --md-on-error:#FFFFFF;
      --md-on-surface:#221D16;
      --md-on-surface-variant:#4A453C;
      --md-surface-dim:#EFE9DF;
      --md-surface-container-lowest:#FFFFFF;
      --md-surface-container-low:#F7F3EC;
      --md-surface-container:#F2ECE2;
      --md-surface-container-high:#ECE5D9;
      --md-surface-container-highest:#E6DED1;
    }
  }

  /* Newsreader, an editorial serif drawn for reading on screens, served from this binary — see
     fonts/README.md. A font CDN would make the page's appearance depend on somebody else's machine
     and tell it when you look at your agents; embedding costs 60KB and nothing leaves the host.
     swap, so the page is readable in the fallback before the face arrives. */
  @font-face {
    font-family:"Newsreader"; font-style:normal; font-weight:400; font-display:swap;
    src:url(/font/newsreader-400.woff2) format("woff2");
  }
  @font-face {
    font-family:"Newsreader"; font-style:normal; font-weight:600; font-display:swap;
    src:url(/font/newsreader-600.woff2) format("woff2");
  }
  @font-face {
    font-family:"Newsreader"; font-style:italic; font-weight:400; font-display:swap;
    src:url(/font/newsreader-italic.woff2) format("woff2");
  }

  /* Two families. The serif for what a person READS — names, the lead line, the empty state — and
     monospace for everything that is a fact from the machine: paths, commands, transcript. The
     system stack behind Newsreader is not decoration: it carries every script the subset does not,
     so a Korean workspace name renders in the platform's serif rather than in tofu. */
  :root {
    --display: "Newsreader", "Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif;
    --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    /* ── state layers ───────────────────────────────────────────────────────── */
  /* M3 does not recolour text on hover; it lays the on- colour over the surface at a fixed
     opacity — 8% hover, 12% focus and press. One recipe, applied by adding the state class to
     anything that responds, so a new control gets the behaviour by being told what it is rather
     than by somebody remembering four rules. */

  /* ── the M3 shape scale, and nothing off it ───────────────────────────── */
    /* 4 · 8 · 12 · 16 · 24 · full. Every radius on this page is one of these; the page used to be
       2px everywhere, which is not a value the scale has. */
    --shape-xs:4px; --shape-s:8px; --shape-m:12px; --shape-l:16px; --shape-xl:24px;
    --shape-full:9999px;

    /* ── M3 motion ────────────────────────────────────────────────────────── */
    /* Verified against material-components-android's Motion.md. The .12s ease this page used was a
       number somebody typed; these are the system's, and using them is what makes two surfaces
       feel like one. */
    --ease-standard:cubic-bezier(0.2, 0, 0, 1);
    --ease-decelerate:cubic-bezier(0.05, 0.7, 0.1, 1);
    --dur-short2:100ms; --dur-short4:200ms; --dur-medium2:300ms;

    /* ── the M3 type scale ────────────────────────────────────────────────── */
    /* size/line-height pairs, taken as pairs: matching a size and inventing a line height is how
       the rhythm goes. The page used to carry 9.5 · 10.5 · 11.5 · 12.5 · 13.5 · 15.5 · 17px, none
       of which is on the scale. The typeface is ours — M3 allows that; the scale is not. */
    --headline-s:24px/32px;
    --title-l:22px/28px; --title-m:16px/24px; --title-s:14px/20px;
    --body-l:16px/24px;  --body-m:14px/20px;  --body-s:12px/16px;
    --label-l:14px/20px; --label-m:12px/16px; --label-s:11px/16px;

    --measure: 74ch;   /* prose */
    --wide: 108ch;     /* transcript, where lines are code and wrapping costs more than width */
  }

  * { box-sizing:border-box; }

  /* Keyboard focus, said once and loudly.
     A dashboard is a page of links and buttons, and the fleet is navigated with tab as readily as
     with a mouse — the underline this layout uses for a pressed state is not a focus ring, and a
     border colour that shifts by one step is not one either. :focus-visible so a mouse click does
     not leave a ring behind it, and an offset so the ring is not mistaken for the element's own
     rule. The outline:none below applies to :focus, which this then overrides for the keyboard. */
  :focus-visible {
    outline:2px solid var(--primary); outline-offset:3px; border-radius:var(--shape-xs);
  }
  html { scrollbar-gutter:stable; -webkit-text-size-adjust:100%; }
  body {
    margin:0; background:var(--bg); color:var(--fg);
    font:14px/1.65 var(--mono);
    -webkit-font-smoothing:antialiased; text-rendering:optimizeLegibility;
    font-variant-numeric:tabular-nums;  /* ages and step counts line up down the column */
  }
  [hidden] { display:none !important; }

  /* A kicker: the small letterspaced label an editorial layout puts above a headline. Here it is
     the state, which is the first thing you want and the last thing that deserves a box. */
  /* On opacity: every value below is set so the RESULT clears WCAG AA (4.5:1) against the page in
     BOTH themes, which is checked in page_test.go. Editorial layouts get their hierarchy from
     dimming secondary text, and the arithmetic is easy to get wrong twice over — the muted role is
     already lowered, and light mode has less headroom than dark. Measured before this note: eight
     of thirteen dimmed pairs were under, the worst at 2.47:1. */
  .kicker {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted);
  }

  /* ── masthead ───────────────────────────────────────────────────────────── */
  header {
    position:sticky; top:0; z-index:2; background:var(--bg);
    border-bottom:1px solid var(--fg);
    box-shadow:0 3px 0 -2px var(--outlineVariant);   /* the hairline under the rule */
    padding:.85rem 1.4rem .6rem;
    padding-top:calc(.7rem + env(safe-area-inset-top));
    display:flex; gap:1rem; align-items:baseline; flex-wrap:wrap;
    max-width:var(--wide); margin:0 auto;
  }
  .mark {
    font:600 var(--headline-s) var(--display); letter-spacing:.01em; color:var(--primary);
    font-feature-settings:"liga" 1;
  }
  /* The three councillors, in their own hues — the signature the terminal wears, set as a
     nameplate's standing line. */
  .sid { color:var(--muted); font-size:11px; letter-spacing:.04em; opacity:.8; overflow-wrap:anywhere; }
  #state {
    margin-left:auto; font:600 11px/1.4 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); display:flex; align-items:center; gap:.45rem;
  }
  #state::before { content:""; width:6px; height:6px; border-radius:50%; background:var(--outline); }
  #state.live::before { background:var(--success); box-shadow:0 0 0 3px color-mix(in srgb, var(--success) 20%, transparent); }
  #state.lost::before { background:var(--error); }
  #back {
    color:var(--muted); text-decoration:none; font-size:11px; letter-spacing:.12em;
    text-transform:uppercase; border-bottom:1px solid var(--outlineVariant); padding-bottom:2px;
  }
  #back:hover { color:var(--primary); border-bottom-color:var(--primary); }

  /* The dock is fixed and its height changes — a prompt bar appears above the composer, and the
     composer itself grows with what you type. A constant padding here either wastes a screen of
     space or hides the last thing the agent said behind the controls, and on a phone it is the
     second one. The page measures the dock and reserves exactly that. */
  main { padding:1.6rem 1.4rem calc(var(--dock, 8rem) + 2rem); max-width:var(--wide); margin:0 auto; }

  /* ── tabs: the resources this console shows ─────────────────────────────── */
  /* Wrapping, because these are sentences and there are three of them now: at 390px the three
     labels are wider than the column, and a nav that overflows takes the whole page sideways with
     it — the one scroll direction a phone should never get. */
  #tabs { display:flex; flex-wrap:wrap; gap:.4rem 1.6rem; padding:.9rem 0 0; }
  #tabs a {
    font:600 11px/1.4 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); text-decoration:none; padding-bottom:.5rem; border-bottom:2px solid transparent;
  }

  #tabs a.on { color:var(--fg); border-bottom-color:var(--primary); }

  /* ── what I had to say ───────────────────────────────────────────────────── */
  /* Grouped by what was said, because the repetition IS the finding: one correction is a remark,
     the same one to three companions is a rule waiting to be written. */
  #ivs { display:block; max-width:var(--measure); }
  .iv {
    display:grid; grid-template-columns:3.5rem 1fr; gap:1rem; align-items:baseline;
    border-bottom:1px solid var(--outlineVariant); padding:.9rem 0;
  }
  .iv .times { font:600 var(--title-l) var(--display); color:var(--primary); text-align:right; }
  .iv .said { font:italic 16px/1.5 var(--display); color:var(--fg); overflow-wrap:anywhere; }
  .iv .where {
    margin-top:.35rem; font-size:11px; letter-spacing:.05em; color:var(--muted);
  }
  .iv.denied .times { color:var(--error); }
  .iv .promote { display:flex; gap:1rem; margin-top:.5rem; flex-wrap:wrap; align-items:center; }
  .iv .promote button {
    background:none; border:0; border-bottom:1px solid var(--outlineVariant); border-radius:0;
    color:var(--muted); font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:.3rem .1rem; min-height:48px; cursor:pointer; white-space:nowrap;
  }
  .iv .promote button:hover { color:var(--primary); border-bottom-color:var(--primary); }
  .iv .promote .done { color:var(--success); font:600 11px/1 var(--mono); letter-spacing:.14em;
                       text-transform:uppercase; }

  /* ── what they have learned ─────────────────────────────────────────────── */
  /* Two tiers on one page, the crossing one first. The boundary between them is the whole of
     context hygiene, and it is only as good as somebody's ability to see it: a rule in the global
     tier reaches every prompt on every project, and after the day it was written nothing else in
     the system mentions it again. */
  #skills { display:block; max-width:var(--measure); }
  .sk { border-bottom:1px solid var(--outlineVariant); padding:.9rem 0; }
  .sk .top { display:flex; gap:.7rem; align-items:baseline; flex-wrap:wrap; }
  .sk .tier {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    flex-basis:100%; order:-1;
  }
  .sk.global .tier { color:var(--warn); }
  .sk.project .tier { color:var(--accent); }
  .sk .what { font:600 16px/1.35 var(--display); color:var(--fg); overflow-wrap:anywhere; }
  /* A fact is quoted, not instructed: it reads as something the companion believes rather than
     something it was told to do, which is the difference a person is judging on this page. */
  .sk.fact .what { font:italic 400 16px/1.4 var(--display); }
  .sk .meta { margin-top:.3rem; font-size:11px; letter-spacing:.05em; color:var(--muted); }
  .sk .drop {
    background:none; border:0; border-bottom:1px solid var(--outlineVariant); border-radius:0;
    /* Zero on purpose: this is a text button set as an underlined word in a reading column, and a
       pill here would read as a control floating over prose. M3's shape scale includes none. */
    color:var(--muted); font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:.3rem .1rem; min-height:48px; cursor:pointer; margin-left:auto;
  }
  .sk .drop:hover { color:var(--error); border-bottom-color:var(--error); }

  /* ── what they can reach ────────────────────────────────────────────────── */
  /* An MCP server is where a companion's reach leaves this machine's file system. The list is
     read to answer one question — which of them can see that thing — so the transport line is
     monospace and complete rather than tidied. */
  #mcp { display:block; max-width:var(--measure); }
  .srv { border-bottom:1px solid var(--outlineVariant); padding:.9rem 0; }
  .srv .top { display:flex; gap:.7rem; align-items:baseline; flex-wrap:wrap; }
  .srv .tier {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    flex-basis:100%; order:-1;
  }
  .srv.global .tier { color:var(--warn); }
  .srv.project .tier { color:var(--accent); }
  .srv .what { font:600 var(--body-l) var(--mono); color:var(--fg); overflow-wrap:anywhere; }
  .srv .how { margin-top:.3rem; font:12px/1.5 var(--mono); color:var(--muted); overflow-wrap:anywhere; }
  .srv .where { margin-top:.2rem; font-size:11px; color:var(--muted); opacity:.85; overflow-wrap:anywhere; }
  .srv .drop {
    background:none; border:0; border-bottom:1px solid var(--outlineVariant); border-radius:0;
    color:var(--muted); font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:.3rem .1rem; min-height:48px; cursor:pointer; margin-left:auto;
  }
  .srv .drop:hover { color:var(--error); border-bottom-color:var(--error); }
  #mcpAdd { display:grid; gap:.6rem; margin:1.4rem 0; max-width:var(--measure); }
  #mcpAdd input, #mcpAdd select {
    background:var(--surfaceContainer); color:var(--fg); border:1px solid var(--outlineVariant);
    border-radius:var(--shape-xs); padding:.55rem .7rem; font:12px/1.4 var(--mono); min-height:48px;
  }
  #mcpAdd button {
    justify-self:start; background:none; border:0; border-bottom:1px solid var(--primary);
    border-radius:0; color:var(--primary); font:600 11px/1 var(--mono); letter-spacing:.16em;
    text-transform:uppercase; padding:.4rem .1rem; min-height:48px; cursor:pointer;
  }
  #mcpAdd .note { font-size:11px; color:var(--muted); }

  /* The recipe. The layer is a pseudo-element so the label's own contrast is never touched, and it
     is inert to the pointer so it can cover the whole control without eating its clicks. */
  .state { position:relative; }
  .state::after {
    content:''; position:absolute; inset:0; border-radius:inherit; pointer-events:none;
    background:currentColor; opacity:0; transition:opacity var(--dur-short2) var(--ease-standard);
  }
  .state:hover::after { opacity:.08; }
  .state:focus-visible::after, .state:active::after { opacity:.12; }
  /* Material's minimum touch target is 48dp, with 8dp between targets. */
  .state { min-height:48px; }

  /* ── the fleet, as a resource table ─────────────────────────────────────── */
  /* The shape a Kubernetes console reaches for, and for the same reason: one row per thing, fixed
     columns, a status word you scan down rather than read across. "kubectl get pods" is the
     archetype of seeing twenty of something at once, and twenty agents is what this is for.
     The editorial part survives in the type — hairlines instead of borders, a serif name, tabular
     figures — because a stock listing is a table too. */

  /* The summary: how many of each, and a filter. A dashboard's first question is "does anything
     need me", and counting cards to answer it is the thing this row removes. */
  #summary { display:flex; gap:1.6rem; flex-wrap:wrap; align-items:baseline;
             border-bottom:1px solid var(--outlineVariant); padding-bottom:.9rem; margin-bottom:.2rem; }
  .tile {
    background:none; border:0; padding:.2rem 0; cursor:pointer; text-align:left;
    display:flex; flex-direction:column; gap:.15rem; min-height:48px;
  }
  .tile .n { font:600 var(--headline-s) var(--display); color:var(--fg); }
  .tile .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    display:flex; align-items:center; gap:.35rem;
  }
  /* A status dot AND the word — the colour is never the only thing carrying the state. */
  .tile .k::before { content:""; width:7px; height:7px; border-radius:50%; background:currentColor; }
  .tile.waiting .k { color:var(--warn); }
  .tile.working .k { color:var(--success); }
  .tile.idle    .k { color:var(--accent); }
  .tile.gone    .k { color:var(--error); }
  .tile[aria-pressed="true"] { border-bottom:2px solid var(--primary); }
  /* A count of zero reads as zero; it does not need to be faint as well, and dimming it put the
     label under AA in both themes (2.25:1 in light — measured by the contrast check). */
  .tile:disabled { cursor:default; }
  .tile:disabled .n, .tile:disabled .k { color:var(--muted); }

  #fleet { display:block; }
  /* One grid for the header and every row, so the columns line up without a table element and
     collapse to two lines on a phone. */
  .thead, .card {
    display:grid; align-items:baseline;
    grid-template-columns: 8.5rem minmax(12rem, 1.4fr) minmax(0, 2fr) 4.5rem 4rem 9rem 7rem;
    gap:.9rem;
  }
  .thead {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    padding:.9rem 0 .5rem; border-bottom:1px solid var(--fg);
  }
  .thead .r, .card .r { text-align:right; }

  .card {
    text-decoration:none; color:var(--md-on-surface); border-bottom:1px solid var(--outlineVariant);
    padding:.75rem .8rem .8rem; margin-left:-.8rem; border-left:2px solid transparent;
    border-radius:var(--shape-s); position:relative;
  }
  .card:hover { background:color-mix(in srgb, var(--primary) 5%, transparent); }
  .card.here { border-left-color:var(--primary); }
  .card.working { border-left-color:var(--success); }
  .card.waiting { border-left-color:var(--warn); }
  .card.abandoned { border-left-color:var(--error); }
  .card.stopped { opacity:.8; }

  /* status */
  .card .badge {
    font:600 11px/1.6 var(--mono); letter-spacing:.14em; text-transform:uppercase; color:var(--muted);
    display:flex; align-items:center; gap:.4rem;
  }
  .card .badge::before { content:""; width:7px; height:7px; border-radius:50%; background:currentColor; flex:none; }
  .card.working .badge { color:var(--success); }
  .card.waiting .badge { color:var(--warn); }
  .card.idle .badge { color:var(--accent); }
  .card.abandoned .badge, .card.stopped .badge { color:var(--error); }

  /* name + workspace, the way a console stacks a resource over its namespace */
  .card .name { font:600 16px/1.3 var(--display); color:var(--fg); overflow-wrap:anywhere; }
  .card:hover .name { color:var(--primary); }
  .card .plan {
    font:600 11px/1.4 var(--mono); letter-spacing:.1em; color:var(--muted); align-self:center;
  }
  .card .role {
    font:600 11px/1.4 var(--mono); letter-spacing:.04em; color:var(--accent);
    overflow-wrap:anywhere; margin-top:.15rem;
  }
  .card .team {
    font:600 11px/1.4 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); margin-top:.2rem;
  }
  .card .path { font-size:11px; color:var(--muted); opacity:.9; overflow-wrap:anywhere; }

  /* what it is doing: one line, clipped — the detail view is one click away for the rest */
  .card .last {
    font:italic 14px/1.45 var(--display); color:var(--fg);
    display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden;
  }
  .card .asking { font:600 12px/1.45 var(--mono); color:var(--warn); overflow-wrap:anywhere; }
  .card .num { font-size:12px; color:var(--muted); }
  .card .host { font-size:11px; color:var(--muted); overflow-wrap:anywhere; }
  .card .host b { font-weight:400; color:var(--fg); opacity:.85; }

  /* Row actions. Open is the row itself as well, but a named control is what makes it discoverable
     — and stopping must never require entering first, which is the whole point of a console. */
  .actions { display:flex; gap:.8rem; justify-content:flex-end; align-items:center; }
  .actions button, .actions .open {
    background:none; border:0; border-bottom:1px solid var(--outlineVariant); border-radius:0;
    color:var(--muted); font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:.3rem .1rem; min-height:48px; cursor:pointer; text-decoration:none; white-space:nowrap;
  }
  .actions .open:hover { color:var(--primary); border-bottom-color:var(--primary); }
  .actions .stop:hover { color:var(--error); border-bottom-color:var(--error); }

  /* answering, inline in the row that is asking */
  .answer { display:flex; gap:1rem; margin-top:.5rem; flex-wrap:wrap; align-items:center; }
  .answer button {
    background:none; border:0; border-bottom:1px solid var(--warn); border-radius:0;
    color:var(--warn); font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:.3rem .1rem; min-height:48px; cursor:pointer;
  }
  .answer button:hover { color:var(--primary); border-bottom-color:var(--primary); }
  .answer input {
    flex:1; min-width:9rem; background:transparent; color:var(--fg); font:16px/1.5 var(--mono);
    border:0; border-bottom:1px solid var(--outline); border-radius:0; padding:.4rem .1rem;
  }
  .answer input:focus { outline:none; border-bottom-color:var(--primary); }
  .answer input:focus-visible { outline:2px solid var(--primary); outline-offset:3px; }

  .empty { font:16px/1.7 var(--display); color:var(--muted); padding:2.5rem 0; max-width:52ch; }
  .empty code { font:14px/1 var(--mono); color:var(--accent); }

  /* ── the agent's own header, so a detail page says what it is looking at ──── */
  #detail {
    display:grid; grid-template-columns:repeat(auto-fit, minmax(9rem, auto)); gap:1.4rem;
    border-bottom:1px solid var(--outlineVariant); padding-bottom:1rem; margin-bottom:1.4rem;
  }
  #detail .f { display:flex; flex-direction:column; gap:.2rem; }
  #detail .f .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
  }
  #detail .f .v { font:var(--body-m) var(--mono); color:var(--fg); overflow-wrap:anywhere; }
  #detail .f .v.state { font-weight:600; letter-spacing:.1em; text-transform:uppercase; font-size:11px; }
  /* The window, as a rule under the number rather than a gauge beside it: this is a fill level and
     the page already spends its colour on state. Unknown windows draw no bar at all — an empty
     track reads as "nearly empty", which is the opposite of "we do not know". */
  #detail .f .bar { height:2px; background:var(--outlineVariant); margin-top:.35rem; }
  #detail .f .bar i { display:block; height:100%; background:var(--primary); }
  #detail .f .bar.tight i { background:var(--warn); }
  #detail .f .v small { color:var(--muted); font-size:11px; }
  #detail .f .fold {
    justify-self:start; margin-top:.4rem; background:none; border:0;
    border-bottom:1px solid var(--outlineVariant); border-radius:0; color:var(--muted);
    font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:.3rem .1rem; min-height:48px; cursor:pointer;
  }
  #detail .f .fold:hover:not(:disabled) { color:var(--primary); border-bottom-color:var(--primary); }
  /* Disabled by the cursor and the missing hover, not by fading the text: a control dimmed below
     the contrast floor is one somebody has to lean in to read to find out it is unavailable. */
  #detail .f .fold:disabled { cursor:default; border-bottom-color:transparent; }

  /* ── the agent's own plan ───────────────────────────────────────────────── */
  #plan { max-width:var(--measure); margin-bottom:1.2rem; }
  #plan .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted); margin-bottom:.4rem;
  }
  .td { display:grid; grid-template-columns:1.2rem 1fr; gap:.6rem; padding:.15rem 0; }
  .td .mark { font:12px/1.6 var(--mono); color:var(--muted); text-align:center; }
  .td .what { font-size:14px; color:var(--fg); overflow-wrap:anywhere; }
  .td.completed .what, .td.done .what { color:var(--muted); text-decoration:line-through; }
  .td.in_progress .mark { color:var(--primary); }
  .td.in_progress .what { color:var(--primary); }

  /* ── work handed to other companions ────────────────────────────────────── */
  #handoffs {
    max-width:var(--measure); border-bottom:1px solid var(--outlineVariant);
    padding-bottom:1rem; margin-bottom:1.4rem;
  }
  #handoffs .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted); margin-bottom:.5rem;
  }
  .ho { display:grid; grid-template-columns:7rem 1fr; gap:.2rem 1rem; padding:.45rem 0; }
  .ho .to { font:600 11px/1.6 var(--mono); letter-spacing:.08em; color:var(--accent); text-align:right; }
  .ho .req { font:var(--body-l) var(--display); color:var(--fg); overflow-wrap:anywhere; }
  .ho .ans { grid-column:2; font-size:12px; color:var(--muted); overflow-wrap:anywhere; }
  .ho.working .to { color:var(--primary); }

  /* ── transcript ─────────────────────────────────────────────────────────── */
  /* Monospace throughout: every line here is something the machine said or did, and a serif would
     be dressing up evidence. The editorial part is the rhythm — a wide gutter of small-caps labels
     against a single column of text. */
  #log { max-width:var(--wide); }
  .row { display:grid; grid-template-columns:6.5rem 1fr; gap:1.1rem; align-items:start; padding:.22rem 0; }
  .who {
    font:600 11px/1.9 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); text-align:right; user-select:none; opacity:.8;
  }
  .txt { white-space:pre-wrap; overflow-wrap:anywhere; }

  /* A user turn is the anchor you scan for: set as a lead, with the rule an editorial layout uses
     for a pull quote. */
  .row.user { margin:1.6rem 0 .7rem; }
  .row.user .txt {
    font:16px/1.55 var(--display); color:var(--primary);
    border-left:2px solid var(--primary); padding-left:.9rem; margin-left:-.9rem;
  }
  .row.user .who { color:var(--primary); }
  .row.assistant .txt { color:var(--fg); }
  .row.thinking .txt { color:var(--muted); font-style:italic; opacity:.8; }
  .row.tool .txt { color:var(--accent); }
  .row.tool .who { color:var(--accent); }
  .row.result .txt, .row.failed .txt {
    color:var(--muted); border-left:1px solid var(--outlineVariant);
    padding:.15rem 0 .15rem .8rem; max-height:11rem; overflow:auto;
  }
  .row.failed .who, .row.failed .txt { color:var(--error); border-left-color:var(--error); }

  /* ── the prompt an agent is blocked on, on that agent's own page ─────────── */
  /* Without this, opening an agent is the one place you CANNOT see that it is waiting for you: the
     prompt is not in the log — it is a question about what should happen, not a record of what did
     — so the transcript shows a run that has simply stopped. It sat above the composer because
     that is where the answer goes. */
  /* One dock, two rows: the prompt above the composer rather than on top of it. Fixed separately
     they both sat at the bottom, and the prompt — being the later one — hid the composer entirely,
     so a blocked agent could be answered and not steered. Which is the wrong way round: "do
     something else instead" is a legitimate reply to being asked. */
  #dock {
    position:fixed; left:0; right:0; bottom:0; z-index:2;
    background:linear-gradient(to top, var(--bg) 88%, transparent);
    padding-bottom:env(safe-area-inset-bottom);
  }
  #prompt {
    background:var(--bg); border-top:2px solid var(--warn);
    padding:.9rem 1.4rem .8rem;
  }
  #prompt .inner { max-width:var(--wide); margin:0 auto; }
  #prompt .asking { font:600 14px/1.5 var(--mono); color:var(--warn); overflow-wrap:anywhere; }

  /* ── composer ───────────────────────────────────────────────────────────── */
  form {
    padding:1rem 1.4rem; display:flex; justify-content:center;
  }
  .composer {
    display:flex; gap:.9rem; width:100%; max-width:var(--wide); align-items:flex-end;
    border-top:1px solid var(--fg); padding-top:.8rem;
  }
  textarea {
    flex:1; background:transparent; color:var(--fg);
    border:0; border-bottom:1px solid var(--outline); border-radius:0;
    padding:.5rem .1rem; font:16px/1.6 var(--mono); resize:none;
    min-height:2.6rem; max-height:12rem; overflow-y:auto;
  }
  textarea:focus { outline:none; border-bottom-color:var(--primary); }
  textarea:focus-visible { outline:2px solid var(--primary); outline-offset:3px; }
  textarea::placeholder { color:var(--muted); opacity:.8; }
  /* The composer's two are real buttons and are shaped like M3's: full-round, filled for the one
     that does the thing and tonal for the one beside it. Everything else on this page that looks
     like a link IS one — see .textbtn. */
  .composer button {
    border:0; border-radius:var(--shape-full);
    font:600 var(--label-l) var(--mono); letter-spacing:.14em; text-transform:uppercase;
    padding:0 1.2rem; cursor:pointer; white-space:nowrap; overflow:hidden;
  }
  .composer button[type=submit] { background:var(--primary); color:var(--md-on-primary); }
  #stop { background:var(--md-surface-container-high); color:var(--md-on-surface); }
  /* State layers, not colour swaps: M3 puts the on- colour over the surface at a fixed opacity.
     Doing it with a pseudo-element keeps the label's own contrast untouched, which dimming or
     recolouring the text does not. */
  .composer button:focus-visible { outline:3px solid var(--primary); outline-offset:2px; }

  @media (max-width:640px) {
    /* The two buttons and a text box do not fit across 390px: measured, the box was left with
       about a third of the row and the placeholder was cut mid-sentence. They take their own line,
       which also puts them under the thumb rather than beside it. */
    .composer { flex-wrap:wrap; }
    .composer input#to {
    flex:0 0 13rem; min-width:8rem; background:var(--surfaceContainer); color:var(--fg);
    border:1px solid var(--outlineVariant); border-radius:var(--shape-xs); padding:.55rem .7rem;
    font:600 12px/1.4 var(--mono); letter-spacing:.04em;
  }
  .composer input#to:focus-visible { outline:2px solid var(--primary); outline-offset:1px; }
  .composer textarea { flex:1 0 100%; }
    .composer button { flex:1; }
    header { padding-left:1rem; padding-right:1rem; }
    main { padding:1.2rem 1rem calc(var(--dock, 8rem) + 1.5rem); }
    .card .name { font:600 var(--title-l) var(--display); }
    .row { grid-template-columns:1fr; gap:.2rem; }
    .who { text-align:left; }
    .row.user .txt { font-size:16px; }
    form { padding-left:1rem; padding-right:1rem; }
    #tabs { gap:.4rem 1.1rem; }
  }
</style>

<header>
  <span class="mark">magi</span>
  <!-- Where you are, always, in both views: magi / fleet, or magi / fleet / <agent>. The middle
       crumb is the way back, which is the same element that says where back goes. -->
  <nav id="crumbs"><a href="/" id="back">companions</a><span id="crumbSep" hidden>/</span><span id="crumbHere"></span></nav>
  <span class="sid" id="sid"></span>
  <span id="state"></span>
</header>

<main>
  <nav id="tabs" hidden>
    <a href="/" id="tabFleet" class="state">companions</a>
    <a href="/?v=interventions" id="tabIv" class="state">corrections</a>
    <a href="/?v=skills" id="tabSkills" class="state">lessons</a>
    <a href="/?v=mcp" id="tabMcp" class="state">connections</a>
  </nav>
  <div id="summary"></div>
  <div id="ivs" hidden></div>
  <div id="skills" hidden></div>
  <div id="mcp" hidden></div>
  <div id="fleet"></div>
  <div id="detail" hidden></div>
  <div id="plan" hidden></div>
  <div id="handoffs" hidden></div>
  <div id="log"></div>
</main>

<footer id="dock">
  <div id="prompt" hidden></div>
  <form id="f" hidden><div class="composer">
    <!-- On the fleet view the composer is addressed: the work goes to whoever does that, and which
         machine they are on is not the asker's problem. On one companion's page it is hidden,
         because the address is the page you are standing on. -->
    <input id="to" hidden placeholder="to: a name, or what they do" autocomplete="off" list="roles">
    <datalist id="roles"></datalist>
    <textarea id="t" rows="1" placeholder="Ask magi to do something…"></textarea>
    <button type="submit" id="send" class="state">send</button>
    <button type="button" id="stop" class="state">interrupt</button>
  </div></form>
</footer>

<script type="module">
// RxJS, from this binary (vendor/README.md says how it was built). The console has three streams
// that used to be hand-rolled: the language pack every label reads, the fleet poll, and the
// transcript. Hand-rolling them is what produced the races this page has already been caught by —
// a slow answer landing on a panel that had been redrawn, a poll that kept firing after you left.
import { BehaviorSubject, timer, from, of, EMPTY,
         switchMap, catchError, map, distinctUntilChanged, shareReplay,
         filter as onlyWhen } from '/vendor/rxjs.js';

// ── labels ───────────────────────────────────────────────────────────────────
// The same shape the handbook uses: a flat dot-keyed pack per locale, chosen by localStorage then
// the browser, falling back to English when the pack cannot be read. Published as a stream so a
// label change reaches everything that draws one, rather than being read once at startup by
// whichever function happened to run first.
// Seeded, not empty. The page is served with its English pack inlined ahead of this module, so the
// FIRST paint already has words — without it every label would show its dotted key until a fetch
// came back, which is a flash of debug output on somebody's dashboard.
const labels$ = new BehaviorSubject(globalThis.__LANG || {});
let L = {};
labels$.subscribe(v => { L = v; });
// t('nav.lessons') — the key IS the fallback, so a missing entry shows the key rather than a blank
// space, which is the difference between "somebody forgot to translate this" and "this is empty".
const tr = (key, vars) => {
  let out = L[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) out = out.replace('{' + k + '}', v);
  return out;
};
const locale = () => (localStorage.getItem('lang') || navigator.language || 'en').slice(0, 2);
// The locale's pack, then English, then the keys. The last step is not really reachable — the pack
// is served by the same process as the page — but a screen full of dotted keys is a better failure
// than a screen full of blanks, because it says what went wrong.
const pack$ = url => from(fetch(url)).pipe(
  switchMap(r => r.ok ? from(r.json()) : EMPTY),
  // A pack is an object of strings. Anything else — a list, a null, an error page that happened to
  // parse — is not one, and letting it through would blank every label on the page and repaint the
  // screen to say so.
  onlyWhen(pack => !!pack && typeof pack === 'object' && !Array.isArray(pack)),
  catchError(() => EMPTY),
);
pack$('/i18n/language.' + locale() + '.json')
  .pipe(catchError(() => EMPTY))
  .subscribe({
    next: pack => labels$.next(pack),
    complete: () => {
      if (Object.keys(L).length || locale() === 'en') return;
      pack$('/i18n/language.en.json').subscribe(pack => labels$.next(pack));
    },
  });
// Anything already on screen is repainted when a pack lands. Guarded on the first paint having
// happened: this subscribes before the page's own elements are declared, and a BehaviorSubject
// hands its current value to a new subscriber immediately — painting there would reach for
// constants that do not exist yet.
let painted = false;
labels$.pipe(distinctUntilChanged()).subscribe(() => { if (painted) paint(); });

const fleetEl = document.getElementById('fleet'), log = document.getElementById('log');
const state = document.getElementById('state'), sidEl = document.getElementById('sid');
const back = document.getElementById('back'), f = document.getElementById('f');
const summaryEl = document.getElementById('summary');
const ivsEl = document.getElementById('ivs'), tabsEl = document.getElementById('tabs');
const skillsEl = document.getElementById('skills'), tabSkills = document.getElementById('tabSkills');
const mcpEl = document.getElementById('mcp'), tabMcp = document.getElementById('tabMcp');
// The last fleet answer, so the "which companion" picker names them without a second fetch.
let fleetSeen = [];
const tabFleet = document.getElementById('tabFleet'), tabIv = document.getElementById('tabIv');
// Which resource this console is showing. A companion's own page is neither — it is one level in.
const view = () => new URLSearchParams(location.search).get('v') || 'fleet';
const crumbSep = document.getElementById('crumbSep'), crumbHere = document.getElementById('crumbHere');
// The four sections, named as nouns: a tab is a place you are, and "what I had to say" is a
// sentence about it. The same words do three jobs — the tab, the crumb, and the browser title —
// so they are written once.
const SECTION_KEY = {fleet: 'nav.companions', interventions: 'nav.corrections',
                     skills: 'nav.lessons', mcp: 'nav.connections'};
const SECTION = new Proxy({}, {get: (_, v) => tr(SECTION_KEY[v] || 'nav.companions')});

// Where this page is mounted. The binary serves it at the root, so BASE is '/' and every url the
// router builds has looked like '/?v=skills' for that reason. A static copy of the same page lives
// under /<repo>/ on a project site, where those escape to the domain root: the clicks still work
// because they are intercepted, but the address pushed is wrong and a reload lands nowhere.
//
// Read from the document rather than configured, so nothing has to be told where it was put.
const BASE = location.pathname.replace(/[^/]*$/, '');
const at = query => BASE + (query || '');
const HREF = {fleet: '', interventions: '?v=interventions', skills: '?v=skills', mcp: '?v=mcp'};

const sock = () => new URLSearchParams(location.search).get('d');
const peerOf = () => new URLSearchParams(location.search).get('p') || '';
// The pair (peer, socket) identifies a companion once more than one console is in the list: a
// socket path is only meaningful on the machine that owns it.
const q = () => sock() ? '?d=' + encodeURIComponent(sock()) + (peerOf() ? '&p=' + encodeURIComponent(peerOf()) : '') : '';

// ── the fleet ────────────────────────────────────────────────────────────────
// A span of seconds in the largest unit that still says something. Two readings of it: how long
// ago something happened, and how long into a turn somebody waited before stepping in — the second
// is not an "ago" and reads as nonsense with the suffix on it.
const dur = s => s < 60 ? s + 's' : s < 3600 ? Math.round(s/60) + 'm'
               : s < 86400 ? Math.round(s/3600) + 'h' : Math.round(s/86400) + 'd';
const ago = s => s < 0 ? '' : dur(s) + ' ago';

// The order the eye should travel: what needs somebody, what is moving, what is asleep, what is
// gone. Kubernetes consoles sort trouble to the top for the same reason — a list you have to read
// to find the problem is a list that hides it.
const ORDER = {waiting: 0, working: 1, idle: 2, abandoned: 3, stopped: 4};
const GROUP = {waiting: 'waiting', working: 'working', idle: 'idle', abandoned: 'gone', stopped: 'gone'};
let filter = null;   // one of the summary keys, or null for everything

// href is where a companion lives in this console's URL space.
function href(a) {
  return '/?d=' + encodeURIComponent(a.socket) + (a.peer ? '&p=' + encodeURIComponent(a.peer) : '');
}

function cell(cls, text) {
  const d = document.createElement('div');
  d.className = cls;
  if (text !== undefined) d.textContent = text;
  return d;
}

// card is one row of the table. The class list is the state, so the left rule and the status colour
// come from one place, and the row is a link because opening it is the common case.
function card(a) {
  const el = document.createElement('a');
  el.className = 'card ' + a.state + ' state' + (a.here ? ' here' : '');
  el.href = href(a);
  el.onclick = e => { e.preventDefault(); go(a.socket, a.peer); };

  el.append(cell('badge', a.state));
  // How far through its own plan. Not a progress bar: a todo list is not a schedule, and a bar
  // would promise a completion time nobody can honour.
  if (a.planTotal) el.append(cell('plan', a.planDone + '/' + a.planTotal));

  const who = cell('who-col');
  who.append(cell('name', a.name));
  // What it is for, when it says so. The path stays: a role is how you pick a companion and a
  // path is how you go and look at it, and neither answers the other's question.
  if (a.role) who.append(cell('role', a.role));
  // The team, and whether this is the one that answers for it. Addressing a team reaches its hub,
  // so which row that is decides where work goes — it is not decoration.
  if (a.team) who.append(cell('team', a.team + (a.hub ? ' · speaks for it' : '')));
  who.append(cell('path', a.workdir));
  el.append(who);

  // What it is doing. A blocked agent shows the question instead — that is the thing to know — and
  // the buttons that answer it sit under the question rather than in the actions column, because
  // they belong to the prompt and not to the row.
  const doing = cell('doing');
  if (a.asking) {
    doing.append(cell('asking', '⏸ ' + a.asking), answerBox(a));
  } else if (a.task) {
    doing.append(cell('last', a.task));
  }
  el.append(doing);

  el.append(cell('num r', a.steps ? a.steps + '' : '—'));
  el.append(cell('num r', ago(a.idle)));

  const host = cell('host');
  const name = document.createElement('b');
  // The console it came from first when there is one: with three machines federated, that is the
  // thing the eye needs before the hostname.
  name.textContent = a.peer ? a.peer : (a.host || 'this machine');
  host.append(name);
  if (a.addr) host.append(document.createElement('br'), document.createTextNode(a.addr));
  if (a.here) host.append(document.createElement('br'), document.createTextNode('this directory'));
  el.append(host);

  el.append(rowActions(a));
  return el;
}

// rowActions: enter, and stop. Stopping must not require entering first — on a console the row you
// want to halt is the one you are already looking at, and making somebody open it to reach the
// button is how a runaway turn gets another thirty seconds.
function rowActions(a) {
  const box = cell('actions');
  const open = document.createElement('a');
  open.className = 'open'; open.textContent = 'open ›';
  open.href = href(a);
  open.onclick = e => { e.preventDefault(); e.stopPropagation(); go(a.socket, a.peer); };
  box.append(open);
  if (a.live && (a.state === 'working' || a.state === 'waiting')) {
    const stop = document.createElement('button');
    stop.className = 'stop'; stop.type = 'button'; stop.textContent = 'stop';
    stop.title = 'interrupt the turn this agent is running';
    stop.onclick = e => {
      e.preventDefault(); e.stopPropagation();
      post('/interrupt', null, a.socket, a.peer).then(loadFleet);
    };
    box.append(stop);
  }
  return box;
}

function tableHead() {
  const h = cell('thead');
  for (const [c, t] of [['', 'status'], ['', 'agent'], ['', 'doing'], ['r', 'steps'], ['r', 'age'],
                        ['', 'host'], ['r', '']]) {
    h.append(cell(c, t));
  }
  return h;
}

// The summary is four numbers and a filter. Counting rows to find out whether anything needs you is
// the work this removes, and it is the first thing a console shows.
function summarise(list) {
  const box = document.getElementById('summary');
  const counts = {waiting: 0, working: 0, idle: 0, gone: 0};
  for (const a of list) counts[GROUP[a.state] || 'idle']++;
  box.replaceChildren(...Object.entries(counts).map(([k, n]) => {
    const b = document.createElement('button');
    b.className = 'tile ' + k; b.type = 'button';
    b.disabled = n === 0;
    b.setAttribute('aria-pressed', String(filter === k));
    b.append(cell('n', n + ''), cell('k', k));
    b.onclick = () => { filter = filter === k ? null : k; render(); };
    return b;
  }));
}

// answerBox is the reply to a blocked agent, next to the question it answers.
//
// The buttons stop the click from opening the agent (the row is a link) — reading and answering are
// different intentions and the same tap must not do both.
function answerBox(a) {
  const box = document.createElement('div'); box.className = 'answer';
  // The socket is passed, not spliced into the path: post() adds the target itself, and doing it
  // in both places produced /answer?d=X?d=X — invisible on the fleet, where post()'s own target is
  // empty, and broken on an agent's page, where it is not.
  const send = (text) => post('/answer', new URLSearchParams({call: a.askId, kind: a.askKind, text}),
                              a.socket, a.peer).then(loadFleet);
  if (a.askKind === 'question') {
    const i = document.createElement('input'); i.placeholder = 'your answer…';
    const b = document.createElement('button'); b.textContent = 'answer';
    const go = e => { e.preventDefault(); e.stopPropagation(); if (i.value.trim()) send(i.value.trim()); };
    b.onclick = go;
    i.onclick = e => { e.preventDefault(); e.stopPropagation(); };
    i.onkeydown = e => { if (e.key === 'Enter') go(e); };
    box.append(i, b);
  } else {
    for (const [label, decision] of [['allow', 'allow'], ['always', 'always'], ['deny', 'deny']]) {
      const b = document.createElement('button'); b.textContent = label;
      b.onclick = e => { e.preventDefault(); e.stopPropagation(); send(decision); };
      box.append(b);
    }
  }
  return box;
}

// The tab's own title carries the count. A dashboard is a page you leave open in a background tab
// or behind an app switcher, and an agent that starts waiting there is invisible until you look —
// the title is the one channel that reaches a page nobody is watching, without asking for
// notification permission or shipping a service worker to deliver them.
function retitle(waiting) {
  // Where you are goes in the title as well: with four sections and a page you leave open, the tab
  // strip in a browser is the outermost breadcrumb somebody actually reads.
  const s = sock();
  const where = s ? nameOf(s) : SECTION[view()] || 'companions';
  const name = 'magi · ' + where;
  document.title = waiting ? '(' + waiting + ') ' + name : name;
}

// drawPrompt puts what an agent is blocked on above its own composer.
//
// An agent's page was the one place this could not be seen: the prompt is not in the log — it is a
// question about what should happen, not a record of what did — so the transcript showed a run that
// had simply stopped, and the only way to find out was to go back to the fleet.
function drawPrompt(a) {
  const box = document.getElementById('prompt');
  if (!a || a.state !== 'waiting') { box.hidden = true; box.replaceChildren(); measureDock(); return; }
  const inner = document.createElement('div'); inner.className = 'inner';
  const k = document.createElement('div'); k.className = 'asking'; k.textContent = '⏸ ' + a.asking;
  inner.append(k, answerBox(a));
  box.replaceChildren(inner);
  box.hidden = false;
  measureDock();
}

// A list from this console, or null when the console itself cannot be reached.
//
// The three loaders had this same try/catch, and the distinction it draws is the one thing they
// must not get differently: "magi-web is not answering" is a fact about the page you are looking
// at, and it is not the same as a companion being quiet. Null, so a caller cannot mistake the
// failure for an empty list and draw "nothing here" over a screen that simply lost its server.
async function fetchList(path) {
  try { return await (await fetch(path)).json(); }
  catch { state.className = 'lost'; state.textContent = 'cannot reach magi-web'; return null; }
}

async function loadFleet() {
  const list = await fetchList('/fleet');
  if (!list) return;
  state.className = '';
  const waiting = list.filter(a => a.state === 'waiting').length;
  retitle(waiting);

  // Who can be addressed, offered as you type. Names and roles both, because the address field
  // takes either and a person should not have to remember which one this companion declared.
  const addresses = new Set();
  for (const a of list) {
    addresses.add(a.name);
    if (a.role) addresses.add(a.role);
    // A team is an address in its own right — it reaches the hub — so it belongs in the list you
    // are offered, once rather than once per member.
    if (a.team) addresses.add(a.team);
  }
  rolesEl.replaceChildren(...[...addresses].map(v =>
    Object.assign(document.createElement('option'), {value: v})));

  // On an agent's page the fleet is polled for this one entry: the prompt it is blocked on and the
  // facts in its header reach the browser no other way.
  fleetSeen = list;
  const here = sock();
  if (here) {
    const mine = list.find(a => a.socket === here && (a.peer || '') === peerOf());
    drawPrompt(mine);
    drawDetail(mine);
    return;
  }

  state.textContent = list.length + (list.length === 1 ? ' agent' : ' agents') +
                      (waiting ? ' · ' + waiting + ' waiting on you' : '');
  state.className = waiting ? 'lost' : '';
  summarise(list);

  if (!list.length) {
    fleetEl.replaceChildren();
    const e = document.createElement('div'); e.className = 'empty';
    e.innerHTML = 'No magi daemons under this config directory.<br>' +
                  'Start one with <code>magi --daemon</code> in a workspace.';
    fleetEl.append(e);
    return;
  }
  // Trouble first, then movement, then quiet, then gone; most recently active within each. A list
  // you have to read to find the problem is a list that hides it.
  const rows = list
    .filter(a => !filter || GROUP[a.state] === filter)
    .sort((x, y) => (ORDER[x.state] - ORDER[y.state]) || (x.idle - y.idle));
  fleetEl.replaceChildren(tableHead(), ...rows.map(card));
}

// drawDetail is the agent page's own header: what this is, where it runs, how far it has got.
// A detail view that does not say which resource it is showing is the one place a console cannot
// afford to be quiet, and a transcript does not say it.
function drawDetail(a) {
  const box = document.getElementById('detail');
  if (!a) { box.hidden = true; box.replaceChildren(); return; }
  const field = (k, v, cls) => {
    const f = cell('f'); f.append(cell('k', k), cell('v ' + (cls || ''), v)); return f;
  };
  box.replaceChildren(
    field('status', a.state, 'state ' + a.state),
    field('workspace', a.workdir),
    ...(a.role ? [field('role', a.role)] : []),
    ...(a.team ? [field('team', a.team + (a.hub ? ' · speaks for it' : ''))] : []),
    field('host', (a.host || 'this machine') + (a.addr ? ' · ' + a.addr : '') +
                  (a.pid ? ' · pid ' + a.pid : '')),
    field('steps', a.steps ? a.steps + '' : '—'),
    field('last activity', ago(a.idle)),
    field('session', a.session),
  );
  box.hidden = false;
  drawPlan(a);
  drawHandoffs(a);
  // Returned rather than dropped: the caller does not wait for it, but a caller that WANTS to —
  // a test, or a later screen that needs the whole panel settled — has no other way to know when
  // the slow half landed, and a promise nobody can await is a promise nobody can check.
  return drawContext(a, box, field);
}

// ── the plan it is working through ───────────────────────────────────────────
// The agent's own todo list, as it last recorded it. Shown as it is: an item it dropped is gone,
// because the record is the whole plan each time and merging would resurrect what it decided
// against.
async function drawPlan(a) {
  const box = document.getElementById('plan');
  const todos = await fetchList('/plan' + qFor(a));
  if (!todos || !todos.length) { box.hidden = true; box.replaceChildren(); return; }
  const mark = t => t.status === 'completed' || t.status === 'done' ? '✓'
                  : t.status === 'in_progress' ? '▸' : '·';
  box.replaceChildren(cell('k', 'plan'), ...todos.map(t => {
    const el = cell('td ' + (t.status || ''));
    el.append(cell('mark', mark(t)), cell('what', t.content));
    return el;
  }));
  box.hidden = false;
}

// ── what it handed to the others ─────────────────────────────────────────────
// A companion answers in its own transcript and the asker reads it — cheap and honest, and it
// leaves somebody clicking through five pages to find out whether the work is done. This is that
// walk, done once, under the transcript of whoever handed it out.
async function drawHandoffs(a) {
  const box = document.getElementById('handoffs');
  const list = await fetchList('/handoffs' + qFor(a));
  if (!list || !list.length) { box.hidden = true; box.replaceChildren(); return; }
  const rows = list.map(h => {
    const el = cell('ho ' + h.state);
    el.append(cell('to', h.to), cell('req', h.request));
    // The answer only when the work is over. Anything else would be reporting a sentence
    // mid-thought as a conclusion.
    el.append(cell('ans', h.answer ? h.answer : 'still ' + h.state));
    return el;
  });
  box.replaceChildren(cell('k', 'handed out'), ...rows);
  box.hidden = false;
}

// ── what is in its head ──────────────────────────────────────────────────────
// Fetched after the rest of the detail is already on screen, and appended when it lands: it costs a
// replay of the whole log, and the six facts above it are the ones somebody opened this page for.
//
// # Asked again only when the answer can have changed
//
// The detail panel is redrawn by every fleet poll — every three seconds, for as long as the tab is
// open. Asking for the context each time would replay the entire log each time, which is precisely
// the cost fleet.Cache exists to avoid paying per row per poll, reintroduced one layer up. The
// answer can only have moved if the transcript did, and the fleet row already carries that: steps.
// So the fetch is keyed on (companion, steps) and an idle companion is asked exactly once.
// Held rather than re-fetched: the panel is rebuilt from scratch on every redraw, so a cached
// answer has to be re-RENDERED even when it is not re-asked for. Skipping the render as well was
// the first version of this and it left the panel a field short every three seconds.
let ctxHeld = {key: '', data: null};
let ctxDraw = 0; // which redraw a pending answer belongs to
// Steps AND state. Steps alone misses the end of a turn: the finish writes the provider's real
// prompt count into the log without adding a tool call, so a panel keyed on steps alone would go
// on showing its estimate — labelled "estimated" — for as long as the companion then sat idle.
function contextKey(a) {
  return (a.peer || '') + '\u0000' + a.socket + '\u0000' + (a.steps || 0) + '\u0000' + a.state;
}

async function drawContext(a, box, field) {
  const key = contextKey(a);
  let c = ctxHeld.key === key ? ctxHeld.data : null;
  if (!c) {
    const mine = ++ctxDraw;
    try { c = await (await fetch('/context' + qFor(a))).json(); }
    catch { return; } // the page is still correct without it; a failed extra must not blank the rest
    // A slower answer must not land on a panel that has since been redrawn: two polls overlap the
    // moment one of them is slow, and the late one would append a second copy of every field below
    // — or worse, older numbers under newer ones.
    if (mine !== ctxDraw) return;
    ctxHeld = {key: key, data: c};
  }
  if (!c || box.hidden) return;

  // Which model, because the window below is that model's and a companion can be on one you did
  // not put it on — /route changes it mid-session and nothing else on this page would say so.
  if (c.model) box.append(field('model', c.model));
  // Said once, where somebody would otherwise wonder why there is no cache figure at all.
  if (!c.cacheReported && !c.estimated) {
    box.append(field('cache', 'this backend does not report it'));
  }

  const size = cell('v', '');
  size.append(document.createTextNode(
    (c.estimated ? '~' : '') + (c.used || 0).toLocaleString() +
    (c.window ? ' / ' + c.window.toLocaleString() : '') + ' tokens'));
  const note = document.createElement('small');
  // Said plainly, because the difference decides what the number is worth: one is the provider's
  // own count from the last turn, the other is arithmetic over the transcript.
  note.textContent = ' ' + (c.estimated ? 'estimated' : 'measured') +
                     (c.messages ? ' · ' + c.messages + ' messages' : '');
  // What the backend served from its own prompt cache — and only when it said. A backend that
  // reports nothing about a cache is not a backend whose cache never hits, and drawing 0% for both
  // would report a working one as broken. Measured on the default local backend: it says nothing.
  if (c.cacheReported) {
    const share = c.used ? Math.round((c.cached || 0) * 100 / c.used) : 0;
    note.textContent += ' · ' + share + '% of it cached';
  }
  size.append(note);
  const f = cell('f');
  f.append(cell('k', 'context'), size);
  // The lever beside the reading. magi folds by itself when the window fills past its ratio; this
  // is for the case that rule does not cover — somebody who can see the run is about to need room
  // and would rather it happened now, between turns, than in the middle of the next one.
  const fold = document.createElement('button');
  fold.className = 'fold state'; fold.type = 'button'; fold.textContent = 'compact now';
  fold.title = 'summarise the older turns — the detail stays on disk and can be recalled, but the ' +
               'live window loses the original wording';
  // Returns its promise, for the same reason drawDetail does: a caller that wants to know when the
  // fold has landed — a test, or a later screen — has no other way, and the held reading must be
  // dropped before anything redraws or the panel keeps showing pre-fold numbers.
  fold.onclick = () => {
    fold.disabled = true;
    return post('/compact', null, a.socket, a.peer).then(() => {
      ctxHeld = {key: '', data: null};
      return loadFleet();
    });
  };
  f.append(fold);
  if (c.window) {
    const pct = Math.min(100, Math.round((c.used || 0) * 100 / c.window));
    const bar = cell('bar' + (pct >= 80 ? ' tight' : ''));
    const fill = document.createElement('i');
    fill.style.width = pct + '%';
    bar.append(fill);
    f.append(bar);
  }
  box.append(f);

  // A compaction is the one moment a companion silently stops knowing something. Four of them in
  // one session is the reason its earlier reasoning cannot be assumed still there.
  if (c.compactions) {
    const v = cell('v', c.compactions + (c.compactions === 1 ? ' fold' : ' folds'));
    const s2 = document.createElement('small');
    s2.textContent = ' · ' + (c.shed || 0).toLocaleString() + ' tokens shed' +
                     (c.lastBefore ? ' · last ' + c.lastBefore.toLocaleString() + '→' + c.lastAfter.toLocaleString() : '') +
                     (c.lastAt ? ' at ' + c.lastAt.slice(11, 16) + 'Z' : '');
    v.append(s2);
    const cf = cell('f');
    cf.append(cell('k', 'summarised away'), v);
    if (c.topics && c.topics.length) {
      // Naming them is the difference between "the detail is not lost" as a claim and as a fact:
      // these are the subjects the companion can pull back in full.
      cf.append(cell('v', c.topics.slice(0, 6).join(' · ') +
                          (c.topics.length > 6 ? ' +' + (c.topics.length - 6) : '')));
    }
    box.append(cf);
  }
}

// qFor is the query that names one companion: its socket, and the console it lives on.
function qFor(a) {
  const parts = ['d=' + encodeURIComponent(a.socket)];
  if (a.peer) parts.push('p=' + encodeURIComponent(a.peer));
  return '?' + parts.join('&');
}

// ── what I had to say ────────────────────────────────────────────────────────
// The supervisor's evening pass. Grouped by the words, because one correction is a remark and the
// same one to three companions is a rule waiting to be written — and counting them by hand across
// five transcripts is exactly the work nobody does.
async function loadInterventions() {
  const list = await fetchList('/interventions');
  if (!list) return;
  const groups = new Map();
  for (const m of list) {
    // Grouped on the words themselves, normalised only for case and spacing. Anything cleverer
    // would merge two different corrections and put a rule in somebody's mouth.
    const key = m.kind + '\u0000' + m.text.toLowerCase().replace(/\s+/g, ' ').trim();
    const g = groups.get(key) || {kind: m.kind, text: m.text, where: new Set(), targets: [], n: 0,
                                  at: m.at, early: Infinity, late: 0};
    g.n++;
    // How far into the turn the person stepped in. The engine has always derived it and nothing
    // has ever shown it, which wasted the distinction it was derived for: a steer eight seconds in
    // is a correction to the INSTRUCTION — say it better next time, or write it into the project's
    // rules — and one twenty minutes in is a correction to the WORK, which no standing rule would
    // have prevented. They promote to different things, so the page has to say which this was.
    if (typeof m.afterSec === 'number') {
      g.early = Math.min(g.early, m.afterSec);
      g.late = Math.max(g.late, m.afterSec);
    }
    const label = (m.peer ? m.peer + '/' : '') + m.companion;
    if (!g.where.has(label)) {
      g.where.add(label);
      // Kept beside the label so a promotion can be aimed: the socket is what identifies the
      // companion, and the console name is what says whose machine it is on.
      g.targets.push({name: m.companion, socket: m.socket, peer: m.peer || ''});
    }
    if (m.at > g.at) g.at = m.at;
    groups.set(key, g);
  }
  const rows = [...groups.values()].sort((a, b) => (b.n - a.n) || (a.at < b.at ? 1 : -1));
  state.className = '';
  state.textContent = list.length + (list.length === 1 ? ' intervention' : ' interventions') +
                      ' · ' + rows.length + ' distinct';
  if (!rows.length) {
    const e = document.createElement('div'); e.className = 'empty';
    e.innerHTML = 'Nothing to promote yet.<br>' +
      'This fills as you steer a companion mid-turn or refuse a tool — the moments worth turning into a rule.';
    ivsEl.replaceChildren(e);
    return;
  }
  ivsEl.replaceChildren(...rows.map(g => {
    const el = cell('iv ' + g.kind);
    el.append(cell('times', g.n + '×'));
    const body = cell('body');
    body.append(cell('said', g.kind === 'denied' ? 'refused ' + g.text : g.text));
    const when = g.early === Infinity ? '' :
      ' · stepped in ' + (g.early === g.late ? dur(g.early) : dur(g.early) + '–' + dur(g.late)) +
      ' into the turn';
    body.append(cell('where', [...g.where].join(' · ') + when +
                              ' · last ' + g.at.replace('T', ' ').replace('Z', '')));
    body.append(promoteBox(g));
    el.append(body);
    return el;
  }));
}

// promoteBox turns a correction into a standing rule, in the tier the person picks.
//
// The tier is the companion boundary and magi does not choose it: a project fact promoted to global
// leaks one project's truth into another's prompts, quietly, and nobody finds the cause weeks
// later. The person knows which kind they just said.
//
// "this companion" only appears when there IS one — a correction given to three of them has no
// single project to belong to, and offering the button anyway would make somebody guess.
function promoteBox(g) {
  const box = cell('promote');
  const done = (word) => {
    box.replaceChildren(cell('done', '✓ ' + word));
  };
  const send = (scope, target) => {
    const body = new URLSearchParams({text: g.text, scope});
    post('/promote', body, scope === 'project' ? target.socket : null, target && target.peer)
      .then(() => done(scope === 'global' ? 'everywhere' : 'for ' + target.name));
  };
  const only = g.targets.length === 1 ? g.targets[0] : null;
  if (only) {
    const b = document.createElement('button'); b.type = 'button';
    b.textContent = 'rule for ' + only.name;
    b.onclick = () => send('project', only);
    box.append(b);
  }
  const g2 = document.createElement('button'); g2.type = 'button';
  g2.textContent = 'rule everywhere';
  g2.onclick = () => send('global', null);
  box.append(g2);
  if (!only) {
    box.append(cell('where', 'said to ' + g.targets.length + ' companions, so there is no single project to put it in'));
  }
  return box;
}

// ── what they have learned ───────────────────────────────────────────────────
async function loadSkills() {
  const list = await fetchList('/skills');
  if (!list) return;
  const crossing = list.filter(s => s.tier === 'global').length;
  const rules = list.filter(s => s.kind !== 'memory').length;
  state.className = '';
  state.textContent = rules + (rules === 1 ? ' rule' : ' rules') + ' · ' +
                      (list.length - rules) + ' remembered · ' +
                      crossing + ' crossing every companion';
  if (!list.length) {
    const e = document.createElement('div'); e.className = 'empty';
    e.innerHTML = 'Nothing learned yet.<br>' +
      'A rule lands here when you promote something you had to say; a fact lands here when a ' +
      'companion decides one is worth keeping.';
    skillsEl.replaceChildren(e);
    return;
  }
  skillsEl.replaceChildren(...list.map(sk => {
    const el = cell('sk ' + sk.tier + (sk.kind === 'memory' ? ' fact' : ''));
    const top = cell('top');
    top.append(cell('tier',
      (sk.tier === 'global' ? 'every companion' : 'only ' + sk.companion) +
      (sk.peer ? ' on ' + sk.peer : '')));
    top.append(cell('what', sk.description || sk.name));
    const drop = document.createElement('button');
    drop.className = 'drop state'; drop.type = 'button'; drop.textContent = 'forget';
    drop.title = 'remove this rule from the store';
    drop.onclick = () => {
      // A rule on another console is forgotten THERE. The socket is that machine's path and the
      // peer name is how this one knows which machine to ask; a global rule has no socket and the
      // peer name alone routes it.
      post('/forget', new URLSearchParams({name: sk.name, tier: sk.tier}),
           sk.tier === 'project' ? sk.socket : null, sk.peer).then(loadSkills);
    };
    top.append(drop);
    el.append(top);
    // A rule tells the companion what to do and a fact tells it what is true. Governed the same
    // way and worth reading differently — a stale fact is wrong, a stale rule is an instruction
    // still being followed.
    const bits = [sk.kind === 'memory' ? 'remembered' : 'rule', sk.name];
    // How settled it is and when it was last seen: the two facts a decision about a rule is made
    // on, and neither is visible anywhere else once the day it was written has passed.
    if (sk.kind !== 'memory' && sk.observed > 1) bits.push('seen ' + sk.observed + '×');
    // Both ends when they differ: "learned three weeks ago and still turning up" is a settled
    // rule, and "learned and last seen the same day" is a one-off that never recurred.
    if (sk.lastSeen && sk.firstSeen && sk.firstSeen !== sk.lastSeen) {
      bits.push(sk.firstSeen + ' → ' + sk.lastSeen);
    } else if (sk.lastSeen) {
      bits.push('last ' + sk.lastSeen);
    }
    if (sk.groups && sk.groups.length) bits.push('only agents in ' + sk.groups.join(', '));
    if (sk.tags && sk.tags.length) bits.push('tagged ' + sk.tags.join(', '));
    el.append(cell('meta', bits.join(' · ')));
    return el;
  }));
}

// ── what they can reach ──────────────────────────────────────────────────────
// The MCP servers each companion has, and the form to add one. Not polled: a config file does not
// change while you are looking at it, and this page is read to decide something.
async function loadMCP() {
  const list = await fetchList('/mcp');
  if (!list) return;
  const reach = new Set(list.map(s => s.companion || 'every companion here'));
  state.className = '';
  state.textContent = list.length + (list.length === 1 ? ' server' : ' servers') +
                      ' · ' + reach.size + ' reached from';

  const rows = list.map(sv => {
    const el = cell('srv ' + sv.tier);
    const top = cell('top');
    top.append(cell('tier', sv.tier === 'global' ? 'every companion here' : 'only ' + sv.companion));
    top.append(cell('what', sv.name));
    const drop = document.createElement('button');
    drop.className = 'drop state'; drop.type = 'button'; drop.textContent = 'remove';
    drop.title = 'delete this definition from ' + sv.file;
    drop.onclick = () => {
      const body = new URLSearchParams({name: sv.name, delete: '1'});
      if (!sv.socket) body.set('tier', 'global');
      post('/mcp', body, sv.socket || null).then(loadMCP);
    };
    top.append(drop);
    el.append(top);
    // The transport, complete and unprettified: this line is the answer to "what actually runs".
    const how = sv.url ? sv.url : [sv.command, ...(sv.args || [])].join(' ');
    el.append(cell('how', how));
    const bits = [];
    // Names only. magi never sends the values, and saying which are needed is what a person needs
    // to set them up on their own machine.
    if (sv.envNames && sv.envNames.length) bits.push('needs ' + sv.envNames.join(', '));
    bits.push(sv.file);
    el.append(cell('where', bits.join(' · ')));
    return el;
  });

  const form = document.createElement('form');
  form.id = 'mcpAdd';
  const field = (name, placeholder, val) => {
    const i = document.createElement('input');
    i.name = name; i.placeholder = placeholder; if (val) i.value = val;
    i.autocomplete = 'off';
    return i;
  };
  const who = document.createElement('select');
  who.name = 'who';
  const opts = [['', 'every companion here (global)']].concat(
    (fleetSeen || []).filter(a => !a.peer).map(a => [a.socket, 'only ' + a.name]));
  for (const [v, label] of opts) {
    const o = document.createElement('option');
    o.value = v; o.textContent = label;
    who.append(o);
  }
  form.append(who, field('name', 'name — becomes [mcp.<name>] in the config'),
    field('command', 'command, e.g. npx   (leave empty for an HTTP server)'),
    field('args', 'arguments, one per line or space-separated'),
    field('url', 'or a url for an HTTP server, scheme and all'),
    field('env', 'environment, NAME=value — values are written to the config file'));
  const go = document.createElement('button');
  go.type = 'submit'; go.textContent = 'add or replace';
  form.append(go);
  const note = cell('note',
    'Written to that companion\'s config file. It attaches when that daemon next starts — ' +
    'this changes the file, not a running process.');
  form.append(note);
  form.onsubmit = async e => {
    e.preventDefault();
    const body = new URLSearchParams();
    for (const el of [...form.find('input')]) if (el.value.trim()) body.set(el.name, el.value.trim());
    if (!who.value) body.set('tier', 'global');
    await post('/mcp', body, who.value || null);
    loadMCP();
  };

  if (!list.length) {
    const e = document.createElement('div'); e.className = 'empty';
    e.innerHTML = 'No external tool servers.<br>' +
      'A companion can read and run things here; an MCP server is how it reaches anything else.';
    mcpEl.replaceChildren(e, form);
    return;
  }
  mcpEl.replaceChildren(...rows, form);
}

// ── one agent ────────────────────────────────────────────────────────────────
// Follow the tail only while the reader is already at the bottom. Yanking the view down while
// somebody reads the middle of a long run is how a live page becomes unreadable.
const atBottom = () => window.innerHeight + window.scrollY >= document.body.offsetHeight - 48;

function draw(rows) {
  const stick = atBottom();
  log.replaceChildren(...(rows || []).map(r => {
    const d = document.createElement('div'); d.className = 'row ' + r.who;
    const w = document.createElement('div'); w.className = 'who'; w.textContent = r.who;
    const t = document.createElement('div'); t.className = 'txt'; t.textContent = r.text;
    d.append(w, t); return d;
  }));
  if (stick) window.scrollTo(0, document.body.scrollHeight);
}

let es, fleetTimer;
function connect() {
  es = new EventSource('/events' + q());
  es.onopen = () => { state.className = 'live'; state.textContent = 'live'; };
  es.onmessage = e => draw(JSON.parse(e.data));
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { state.className = 'lost'; state.textContent = 'reconnecting…';
                       es.close(); if (sock()) setTimeout(connect, 1500); };
}

// ── routing ──────────────────────────────────────────────────────────────────
// Same document either way: entering an agent is a pushState, and the browser's back button lands
// on the fleet without a reload. Every view switch tears the other one's polling down — two of
// them running at once is how a page keeps costing after you leave it.
// paint puts the labels on the parts of the page that are written in the markup rather than built
// by a function. Called once at startup and again whenever the pack changes.
function paint() {
  painted = true;
  tabFleet.textContent = tr('nav.companions');
  tabIv.textContent = tr('nav.corrections');
  tabSkills.textContent = tr('nav.lessons');
  tabMcp.textContent = tr('nav.connections');
  t.setAttribute('placeholder', tr('placeholder.ask'));
  toEl.setAttribute('placeholder', tr('placeholder.address'));
  document.getElementById('send').textContent = tr('action.send');
  document.getElementById('stop').textContent = tr('action.interrupt');
}

// paint does NOT redraw the view, and that is the whole point. A pack can land at any moment — mid
// fetch, mid interaction — and re-rendering there wipes whatever was on screen: caught here with a
// detail panel that lost its context block because the language arrived during the await. The
// labels written in the markup are repainted; everything drawn by a function reads tr() when it
// next draws, which is soon enough for a word that just changed.

function render() {
  if (es) { es.close(); es = null; }
  if (fleetTimer) { clearInterval(fleetTimer); fleetTimer = null; }
  const s = sock();
  const v = s ? '' : view();
  // Where you are, in the masthead: magi / lessons, or magi / companions / design. The crumb that
  // names the section IS the way back to it, so "where am I" and the way out are one thing.
  //
  // It names the SECTION, not always the fleet. A crumb that read "fleet" while you stood in the
  // connections tab answered a question nobody asked and offered a way back to somewhere you had
  // not been.
  const section = s ? 'companions' : SECTION[v] || 'companions';
  retitle(0);
  back.textContent = section;
  back.setAttribute('href', at(s ? '' : HREF[v] || ''));
  crumbSep.hidden = !s;
  crumbHere.textContent = s ? nameOf(s) : '';
  back.className = s ? '' : 'here';
  tabsEl.hidden = !!s;
  tabFleet.className = v === 'fleet' ? 'on' : '';
  tabIv.className = v === 'interventions' ? 'on' : '';
  tabSkills.className = v === 'skills' ? 'on' : '';
  tabMcp.className = v === 'mcp' ? 'on' : '';
  fleetEl.hidden = !!s || v !== 'fleet';
  summaryEl.hidden = !!s || v !== 'fleet';
  ivsEl.hidden = !!s || v !== 'interventions';
  skillsEl.hidden = !!s || v !== 'skills';
  mcpEl.hidden = !!s || v !== 'mcp';
  log.hidden = !s;
  // The composer is on both views now. On a companion's page it steers that companion; on the
  // fleet it dispatches, and the address field is the difference.
  f.hidden = !s && v !== 'fleet';
  toEl.hidden = !!s;
  document.getElementById('stop').hidden = !s; // nothing to interrupt from the fleet view
  document.getElementById('detail').hidden = !s;
  document.getElementById('handoffs').hidden = true;
  document.getElementById('plan').hidden = true;
  document.getElementById('prompt').hidden = true;
  sidEl.textContent = '';
  measureDock();
  if (s) { draw([]); connect(); }
  else { state.className = ''; state.textContent = ''; }
  if (v === 'mcp') {
    // The picker names companions, so the fleet is read once first.
    fetchList('/fleet').then(list => { if (list) fleetSeen = list; loadMCP(); });
    return;
  }
  if (v === 'skills') {
    // Not polled, for the same reason the corrections page is not: this is read and thought about.
    loadSkills();
    return;
  }
  if (v === 'interventions') {
    // Not polled: this is a page somebody reads and thinks about, and a list that reorders itself
    // under the cursor while they are deciding what to promote is worse than one that is a minute
    // old. It reloads when they come back to it.
    loadInterventions();
    return;
  }
  // The other two poll: the fleet for its rows, a companion's page for the facts about itself that
  // its log cannot carry.
  loadFleet();
  fleetTimer = setInterval(loadFleet, 3000);
}

// nameOf is the crumb for a socket before the fleet has been fetched — the file name carries the
// workspace's base name, which is what a person calls the agent.
function nameOf(socket) {
  const base = socket.replace(/^.*\//, '').replace(/^daemon-/, '').replace(/\.sock$/, '');
  return base.replace(/-[a-z0-9]{8}$/, '');
}

function go(s, peer) {
  history.pushState({}, '', at(s ? '?d=' + encodeURIComponent(s) + (peer ? '&p=' + encodeURIComponent(peer) : '') : ''));
  render();
}
// The crumb goes where it SAYS it goes. It names the section you are standing in, so sending it to
// the fleet regardless made the label and the click disagree — you would read "lessons" and land on
// companions. render() keeps its href current; this just follows it.
back.onclick = e => {
  e.preventDefault();
  const url = back.getAttribute('href') || '/';
  history.pushState({}, '', url);
  render();
};
for (const [el, key] of [[tabFleet, 'fleet'], [tabIv, 'interventions'], [tabSkills, 'skills'],
                         [tabMcp, 'mcp']]) {
  // The href is set as well as the click: a middle-click or a copied link has to reach the same
  // place, and on a project site an absolute one does not.
  el.setAttribute('href', at(HREF[key]));
  el.onclick = e => { e.preventDefault(); history.pushState({}, '', at(HREF[key])); render(); };
}
addEventListener('popstate', render);

async function post(path, body, socket, peer) {
  // Either half can stand alone: a companion is named by its socket, a console by its peer name,
  // and a global rule on another console has only the second. With neither, the action is about
  // whatever the page is already looking at.
  const parts = [];
  if (socket) parts.push('d=' + encodeURIComponent(socket));
  if (peer) parts.push('p=' + encodeURIComponent(peer));
  const target = parts.length ? '?' + parts.join('&') : q();
  const r = await fetch(path + target, {method:'POST', body});
  if (!r.ok) { state.className = 'lost'; state.textContent = (await r.text()).trim().slice(0, 80); }
}

const t = document.getElementById('t'), toEl = document.getElementById('to');
const rolesEl = document.getElementById('roles');
const grow = () => { t.style.height = 'auto'; t.style.height = Math.min(t.scrollHeight, 192) + 'px'; measureDock(); };

// The transcript reserves whatever the dock is actually occupying. Its height changes with the
// composer as you type and with the prompt bar appearing, and a guessed constant either wastes a
// screen or hides the last thing the agent said — on a phone, the second.
const dock = document.getElementById('dock');
function measureDock() {
  document.documentElement.style.setProperty('--dock', (dock.offsetHeight || 0) + 'px');
}
if (typeof ResizeObserver === 'function') new ResizeObserver(measureDock).observe(dock);
t.addEventListener('input', grow);
f.onsubmit = e => {
  e.preventDefault();
  const v = t.value.trim(); if (!v) return;
  if (sock()) { t.value = ''; grow(); post('/submit', new URLSearchParams({text: v})); return; }
  // From the fleet: addressed work. An empty address would have to guess who it is for, and
  // guessing sends somebody's turn into the wrong workspace — so it asks instead.
  const to = toEl.value.trim();
  if (!to) { state.className = 'lost'; state.textContent = 'say who it is for'; toEl.focus(); return; }
  t.value = ''; grow();
  post('/dispatch', new URLSearchParams({to: to, text: v})).then(loadFleet);
};
// Enter sends on a keyboard and inserts a newline on a phone: a soft keyboard's return key is the
// only way to break a line there, and hijacking it leaves no way to write a second paragraph.
const touch = matchMedia('(hover: none)').matches;
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !touch) { e.preventDefault(); f.requestSubmit(); } };
document.getElementById('stop').onclick = () => post('/interrupt', null);

paint();
render();
</script>
`
