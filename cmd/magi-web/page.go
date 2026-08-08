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
<!-- Before the stylesheet, and deliberately not in the module at the end of the page: a theme
     applied after first paint is a flash of the other one, and on a dark-preferring machine that
     picked light it is a white flash in a dark room. Four lines, so it stays here rather than
     becoming a file the page has to wait for. The module reads the same key. -->
<script>
  try {
    var want = localStorage.getItem('theme');
    if (want === 'light' || want === 'dark') document.documentElement.setAttribute('color-theme', want);
  } catch (e) { /* storage can be denied; the machine's own preference still answers */ }
</script>
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

    /* ── the same roles under the names Material Web reads ────────────────── */
    /* The components are themed by these and nothing else. Setting a few of them per component —
       which is what this page did first — leaves every role it did not mention drawn in the
       library's baseline purple, which is what "the colours are the default ones" looks like.
       Declared once, at the root, so a component added later is magi-coloured by existing. */
    --md-sys-color-primary:var(--primary);
    --md-sys-color-on-primary:var(--md-on-primary);
    --md-sys-color-primary-container:var(--primaryContainer);
    --md-sys-color-on-primary-container:var(--md-on-primary-container);
    --md-sys-color-secondary:var(--accent);
    --md-sys-color-on-secondary:var(--md-on-primary);
    --md-sys-color-secondary-container:var(--md-surface-container-high);
    --md-sys-color-on-secondary-container:var(--md-on-surface);
    --md-sys-color-tertiary:var(--accent);
    --md-sys-color-on-tertiary:var(--md-on-primary);
    --md-sys-color-error:var(--error);
    --md-sys-color-on-error:var(--md-on-error);
    --md-sys-color-error-container:var(--md-surface-container-high);
    --md-sys-color-on-error-container:var(--error);
    --md-sys-color-background:var(--bg);
    --md-sys-color-on-background:var(--fg);
    --md-sys-color-surface:var(--bg);
    --md-sys-color-on-surface:var(--md-on-surface);
    --md-sys-color-surface-variant:var(--surface);
    --md-sys-color-on-surface-variant:var(--md-on-surface-variant);
    --md-sys-color-surface-container-lowest:var(--md-surface-container-lowest);
    --md-sys-color-surface-container-low:var(--md-surface-container-low);
    --md-sys-color-surface-container:var(--md-surface-container);
    --md-sys-color-surface-container-high:var(--md-surface-container-high);
    --md-sys-color-surface-container-highest:var(--md-surface-container-highest);
    --md-sys-color-outline:var(--outline);
    --md-sys-color-outline-variant:var(--outlineVariant);
    --md-sys-color-inverse-surface:var(--fg);
    --md-sys-color-inverse-on-surface:var(--bg);
    --md-sys-color-shadow:#000000;
    --md-sys-color-scrim:#000000;

    /* ── and the type, under the names Material Web reads ─────────────────── */
    /* A component takes its font from --md-sys-typescale-<role>-font, not from the ref typeface
       alone, so setting only the latter leaves every label in the library's fallback. Declared
       across the roles at the root, the way the handbook project does it: one place, and a
       component added later is already in magi's face.
       Sizes are the M3 scale (see the type tokens above); the faces are ours, which M3 allows —
       the scale is what it asks you to keep. */
    --md-ref-typeface-plain:var(--mono);
    --md-ref-typeface-brand:var(--display);
    --md-sys-typescale-label-small-font:var(--mono);
    --md-sys-typescale-label-small-size:11px;
    --md-sys-typescale-label-small-line-height:16px;
    --md-sys-typescale-label-medium-font:var(--mono);
    --md-sys-typescale-label-medium-size:12px;
    --md-sys-typescale-label-medium-line-height:16px;
    --md-sys-typescale-label-large-font:var(--mono);
    --md-sys-typescale-label-large-size:14px;
    --md-sys-typescale-label-large-line-height:20px;
    --md-sys-typescale-body-small-font:var(--mono);
    --md-sys-typescale-body-small-size:12px;
    --md-sys-typescale-body-small-line-height:16px;
    --md-sys-typescale-body-medium-font:var(--mono);
    --md-sys-typescale-body-medium-size:14px;
    --md-sys-typescale-body-medium-line-height:20px;
    --md-sys-typescale-body-large-font:var(--mono);
    --md-sys-typescale-body-large-size:16px;
    --md-sys-typescale-body-large-line-height:24px;
    --md-sys-typescale-title-small-font:var(--display);
    --md-sys-typescale-title-small-size:14px;
    --md-sys-typescale-title-small-line-height:20px;
    --md-sys-typescale-title-medium-font:var(--display);
    --md-sys-typescale-title-medium-size:16px;
    --md-sys-typescale-title-medium-line-height:24px;
    --md-sys-typescale-title-large-font:var(--display);
    --md-sys-typescale-title-large-size:22px;
    --md-sys-typescale-title-large-line-height:28px;
    --md-sys-typescale-headline-small-font:var(--display);
    --md-sys-typescale-headline-small-size:24px;
    --md-sys-typescale-headline-small-line-height:32px;
  }
  /* Light, said twice on purpose.

     The media query is the machine's answer and the attribute is the reader's, and the reader has
     to be able to override the machine in BOTH directions — a dark-at-night machine chosen light,
     and a light machine chosen dark. A page with only the query can be moved one way and not back.
     The :not([color-theme]) is what makes the query yield the moment somebody chooses.

     The two blocks carry the SAME declarations and a test in page_test.go fails if they stop
     doing so — CSS has no way to give one ruleset two selectors across a media query, so the copy
     is unavoidable and the drift is not.

     The attribute is written by the inline script in the markup, before this stylesheet paints, so
     a chosen theme does not flash the other one first. */
  @media (prefers-color-scheme: light) {
    :root:not([color-theme]) {
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
  :root[color-theme="light"] {
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
    /* Read out of the shipped bundle rather than a document: it is what md-tabs animates its own
       indicator with, so a container that opens beside them moves on the same curve. */
    --ease-emphasized:cubic-bezier(0.3, 0, 0, 1);

    /* How much room the rail takes. Declared HERE and not only on #rail, because the page's own
       left offset is computed from it — and a var() that resolves to nothing does not fall back to
       the shorthand underneath it. The declaration becomes invalid at computed-value time and the
       property takes its initial value, which for padding is 0: the offset silently vanished and
       the rail stood on top of the page at every width. */
    --rail-w:4.5rem;
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
    /* What a whole screen of console may take. Wider than the transcript's measure on purpose: the
       fleet is a table and a table uses room, while prose inside it keeps --measure. Capped rather
       than unbounded so an ultrawide monitor does not stretch a row to a metre. */
    --page: 150ch;
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
    /* A column, so the rail can be moved to the end of a narrow page with an order property while
       staying BEFORE the page in the markup. Tab order follows the document, not the layout:
       with the rail written after main a keyboard reached the navigation only after every row
       of the fleet — measured on the deployed page, 41 focusable things with the nav last. */
    display:flex; flex-direction:column; min-height:100vh;
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
    max-width:var(--page); padding-right:2.4rem;
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
  /* The count is a readout; this part of it is a control, and it says so by being one. */
  #state .jump {
    --md-text-button-label-text-color:var(--warn);
    --md-text-button-hover-label-text-color:var(--warn);
    --md-text-button-label-text-size:11px;
    margin-left:-.3rem;
  }
  #state.live::before { background:var(--success); box-shadow:0 0 0 3px color-mix(in srgb, var(--success) 20%, transparent); }
  #state.lost::before { background:var(--error); }
  #back {
    color:var(--muted); text-decoration:none; font-size:11px; letter-spacing:.12em;
    text-transform:uppercase; border-bottom:1px solid var(--outlineVariant); padding-bottom:2px;
  }
  #back:hover { color:var(--primary); border-bottom-color:var(--primary); }

  /* ── the rail: navigation on a wide screen, settings on a narrow one ────── */
  /* One element in two modes rather than two that have to agree. Wide: it stands beside the page
     as a rail and the hamburger widens it into a drawer. Narrow: it is off-screen and the same
     button slides it in over the page, with the tabs still doing the navigating — which is the
     handbook's arrangement and the one M3 describes for these two widths.

     The breakpoint is 768/769px, the handbook's, so the two products break in the same place. */
  #rail {
    position:fixed; top:0; bottom:0; left:0; z-index:3;
    width:var(--rail-w); box-sizing:border-box;
    padding:calc(.7rem + env(safe-area-inset-top)) .5rem 1.2rem;
    background:var(--md-surface-container-low); border-right:1px solid var(--outlineVariant);
    display:flex; flex-direction:column; gap:1rem; overflow:hidden auto;
    /* Same curve and duration as the components use for a container that changes size, so the rail
       and the page's own margin arrive together rather than one chasing the other. */
    transition:width 250ms var(--ease-emphasized), transform 250ms var(--ease-emphasized);
  }
  body[nav="open"] { --rail-w:16rem; }
  /* Collapsed, the rail is 4.5rem and a word like "connections" is not — so collapsed shows the
     icon and nothing else. Clipping the label instead would put half a word on screen, which reads
     as a bug rather than as a choice. The label still exists for a screen reader: it is the
     item's aria-label, set beside the text in paint(). */
  #rail .lbl { white-space:nowrap; }
  body:not([nav="open"]) #rail .lbl { display:none; }
  body:not([nav="open"]) #rail md-list-item { --md-list-item-leading-space:14px; }
  #rail .ic { flex:none; display:block; }
  #rail md-list {
    --md-list-container-color:transparent;
    --md-list-item-label-text-font:var(--mono);
    --md-list-item-label-text-size:12px;
    --md-list-item-label-text-weight:600;
    --md-list-item-label-text-color:var(--muted);
    --md-list-item-container-shape:var(--shape-full);
    --md-sys-color-primary:var(--primary);
    --md-sys-color-on-surface:var(--md-on-surface);
    --md-sys-color-on-surface-variant:var(--md-on-surface-variant);
    letter-spacing:.1em; text-transform:uppercase;
  }
  /* Where you are, in the colour the rest of the page uses for it. A list item has no selected
     state of its own — it is a list, not a set of choices — so this is ours, and it is a filled
     shape rather than a colour change alone. */
  #rail md-list-item[selected] {
    --md-list-item-label-text-color:var(--primary);
    --md-list-item-container-color:color-mix(in srgb, var(--primary) 14%, transparent);
  }
  /* The badge, corrected. Its inner box is position:absolute at top / 50% across, which is right
     when the badge is laid over an icon and wrong everywhere else — dropped into a flow it anchors
     to whatever ancestor happens to be positioned and lands somewhere unrelated. Giving the host a
     size and a position makes the host the thing it anchors to, which is what a caller has to do
     for a component still in the library's unstable half. */
  md-badge {
    position:relative; display:inline-block; vertical-align:middle;
    width:18px; height:18px; flex:none;
    --md-badge-color:var(--warn);
    --md-badge-large-color:var(--warn);
    --md-badge-large-label-text-color:var(--bg);
    --md-badge-large-label-text-font:var(--mono);
  }
  /* On a tab the badge sits beside the word rather than over it: these labels are words, not icons,
     and a count parked on top of "companions" lands on a letter. */
  /* On a tab the host is NOT positioned: a tab lays its label out itself, and giving the badge a
     relative box of its own dropped it onto a line below the word. Absolute inside the tab instead,
     riding the label's top-right the way it rides the icon in the rail. */
  /* In the flow, in a wrapper of its own, exactly like the rail's. Absolutely placed over the tab
     it clipped the word it was counting for — "컴패니⓶" — and a count that eats its own label is
     worse than no count. */
  .tablbl { display:inline-flex; align-items:center; }
  .badgewrap { width:16px; height:16px; margin-left:.35rem; }
  .badgewrap md-badge { position:absolute; inset:0; width:16px; height:16px; }
  .badgewrap[hidden], .badgewrap:has(md-badge[hidden]) { display:none; }
  /* In the rail it rides the icon, which is what a badge is for — and when the rail is collapsed
     the icon is the only thing there. */
  .icwrap { position:relative; display:inline-flex; width:20px; height:20px; }
  .icwrap md-badge { position:absolute; top:-5px; right:-7px; width:16px; height:16px; }
  #prefsForm { display:flex; flex-direction:column; gap:1rem; min-width:16rem; }
  #prefsForm .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
  }
  #console { font:12px/1.6 var(--mono); color:var(--muted); overflow-wrap:anywhere; }
  #console b { color:var(--fg); font-weight:600; }
  #railMenu, #themeToggle, #prefs {
    --md-icon-button-icon-color:var(--muted); color:var(--muted);
  }
  #railMenu { align-self:center; margin-bottom:.2rem; }
  #themeToggle { margin-left:.2rem; }
  /* One of the two is always hidden, and which one follows the theme in force — including when that
     theme is the machine's, so the query appears here as well as the attribute. Same pairing the
     palette uses, for the same reason: a reader can override the machine in both directions. */
  #themeToggle .sun { display:none; }
  @media (prefers-color-scheme: dark) {
    :root:not([color-theme]) #themeToggle .sun { display:block; }
    :root:not([color-theme]) #themeToggle .moon { display:none; }
  }
  :root[color-theme="dark"] #themeToggle .sun { display:block; }
  :root[color-theme="dark"] #themeToggle .moon { display:none; }

  /* The dock is fixed and its height changes — a prompt bar appears above the composer, and the
     composer itself grows with what you type. A constant padding here either wastes a screen of
     space or hides the last thing the agent said behind the controls, and on a phone it is the
     second one. The page measures the dock and reserves exactly that. */
  /* Left-aligned beside the rail rather than centred in what is left of the window. Centred, the
     page moved by HALF the rail's growth and lost the other half off its own width, so opening the
     drawer re-wrapped every column instead of sliding the page across. A block that keeps its width
     and moves by exactly the distance the rail took is the one a reader can follow.
     
     No auto margin. Body is a flex column, and an auto margin on the cross axis makes a flex item
     size to its CONTENT instead of stretching — so this was 720px wide inside a 1264px cap on a
     2497px screen, which is what "the right margin disappeared" and "a wide screen is not used"
     both were. A stretched item with a max-width already sits at the start of a column. */
  main {
    padding:1.6rem 2.4rem calc(var(--dock, 8rem) + 2rem) 1.4rem;
    max-width:var(--page);
  }

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
  /* Every section is the same width. They were not: this one was 74ch while lessons and MCP were
     108ch and the fleet filled the page, so changing menus moved the left edge and re-set the line
     length — three different pages rather than four views of one. The prose inside keeps its own
     measure, which is where a reading width belongs. */
  /* Both halves of one story, on one page: what has been said often enough to become a rule, and
     the rules. They were two destinations, and a reader had to know that promoting on one made
     something appear on the other. Each half says which it is. */
  #ivs { display:block; max-width:var(--page); }
  .sectionhead {
    display:flex; align-items:baseline; gap:.7rem;
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    border-bottom:1px solid var(--fg); padding-bottom:.35rem; margin:0 0 .8rem;
  }
  .sectionhead .n { margin-left:auto; }

  #ivs .said { max-width:var(--measure); }
  .iv {
    display:grid; grid-template-columns:3.5rem 1fr; gap:1rem; align-items:baseline;
    border-bottom:1px solid var(--outlineVariant); padding:1.1rem 0;
  }
  .iv .times { font:600 var(--title-l) var(--display); color:var(--primary); text-align:right; }
  .iv .said { font:italic 16px/1.5 var(--display); color:var(--fg); overflow-wrap:anywhere; }
  .iv .where {
    margin-top:.35rem; font-size:11px; letter-spacing:.05em; color:var(--muted);
  }
  .iv.denied .times { color:var(--error); }
  /* What to do about this correction, set apart from the correction itself by a rule. It sat in the
     middle of the text block with nothing separating them, so it read as floating rather than as
     belonging to the words above it.
     
     No rules for a bare "button" here any more: these have been md-text-button since the
     migration, and a rule naming the old element reaches nothing — the third such this week. */
  .iv .promote {
    display:flex; gap:.4rem; margin-top:.7rem; padding-top:.5rem; flex-wrap:wrap;
    align-items:center; border-top:1px solid var(--outlineVariant);
  }
  .iv .promote .done {
    color:var(--success); font:600 11px/1 var(--mono); letter-spacing:.14em; text-transform:uppercase;
  }

  /* ── what they have learned ─────────────────────────────────────────────── */
  /* Two tiers on one page, the crossing one first. The boundary between them is the whole of
     context hygiene, and it is only as good as somebody's ability to see it: a rule in the global
     tier reaches every prompt on every project, and after the day it was written nothing else in
     the system mentions it again. */
  /* Wider than the prose measure: a rule's description reads like prose but the line under it
     carries a name, a date range and sometimes a file path, and 74ch put those on three lines. */
  #skills { display:block; max-width:var(--page); }
  .sk { border-bottom:1px solid var(--outlineVariant); padding:1.1rem 0; }
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
  .sk .drop { margin-left:auto; }
  .sk .fold { margin-left:auto; }
  .sk .fold + .drop { margin-left:0; }
  /* The rule as written. A reading measure, because it is prose and the row is not. */
  .sk .body {
    margin:.5rem 0 .1rem; padding:.6rem 0 0; max-width:var(--measure);
    border-top:1px solid var(--outlineVariant);
    font:13px/1.65 var(--mono); color:var(--fg); white-space:pre-wrap; overflow-wrap:anywhere;
  }

  /* ── what they can reach ────────────────────────────────────────────────── */
  /* An MCP server is where a companion's reach leaves this machine's file system. The list is
     read to answer one question — which of them can see that thing — so the transport line is
     monospace and complete rather than tidied. */
  /* Not prose at all: the transport line is a command with arguments and the line under it is an
     absolute path. Clipping either to a reading measure hides the part being read for. */
  #mcp { display:block; max-width:var(--page); }
  .srv { border-bottom:1px solid var(--outlineVariant); padding:1.1rem 0; }
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
  .srv .drop { margin-left:auto; }
  /* Nothing here draws a box or a border: the field and the select bring their own outline, their
     own shape and their own 48dp target, and a second set drawn over them was two descriptions of
     one control that could only ever agree by accident. The form says how the controls are
     arranged and stops. */
  #mcpAdd { display:grid; gap:.9rem; margin:1.4rem 0; max-width:var(--measure); }
  #mcpAdd md-filled-button { justify-self:start; }
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
  /* Filter chips, because that is what these are: four selectable filters over one list, and a
     chip already knows what selected looks like, how to be reached with arrow keys, and how to
     draw a state layer. Written as buttons here before, they knew none of it. The chip renders a
     slot when it has no label attribute, so the count and the word stay ours. */
  /* Clear of whatever is above it. The chips sat straight under the tab row with nothing between,
     so the row of filters read as part of the tabs — two different kinds of control touching. */
  #summary { display:flex; flex-wrap:wrap; gap:.5rem; padding-bottom:.9rem;
             margin:1.4rem 0 .2rem; border-bottom:1px solid var(--outlineVariant); }
  .tile { --md-filter-chip-container-height:40px; --md-filter-chip-label-text-font:var(--mono); }
  .tile .n { font:600 var(--title-m) var(--display); color:var(--fg); margin-right:.45rem; }
  .tile .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    display:inline-flex; align-items:center; gap:.35rem;
  }
  /* A status dot AND the word — the colour is never the only thing carrying the state. */
  .tile .k::before { content:""; width:7px; height:7px; border-radius:50%; background:currentColor; }
  .tile.waiting .k { color:var(--warn); }
  .tile.working .k { color:var(--success); }
  .tile.idle    .k { color:var(--accent); }
  .tile.gone    .k { color:var(--error); }
  /* A count of zero reads as zero; it does not need to be faint as well, and dimming it put the
     label under AA in both themes (2.25:1 in light — measured by the contrast check). */
  .tile[disabled] .n, .tile[disabled] .k { color:var(--muted); }

  #fleet { display:block; }
  /* One grid for the header and every row, so the columns line up without a table element and
     collapse to two lines on a phone. */
  .thead, .card {
    display:grid; align-items:baseline;
    /* Sized to what actually arrives. The doing column is the widest because the server clips a
       task at 160 characters and that is what has to fit — at ~0.5ch per character in this face,
       160 wants about 40rem before it wraps to a third line, and it gets that whenever there is
       room. The fixed columns are sized to their content and not to a guess: a state word, a step
       count, an age, a host and an address.

       These are MINIMA and their sum is what has to fit. The previous set added up to 1086px with
       the gaps, which is wider than this page's own measure — so every width tested scrolled
       sideways, including a 1100px desktop. Sizing each column to what it holds is only half the
       job; the other half is checking the total against the space there is. */
    grid-template-columns: 7rem minmax(8rem, 1fr) minmax(12rem, 2.6fr) 3.5rem 4rem 7rem 6rem;
    gap:.9rem;
  }
  .thead {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--muted);
    padding:.9rem 0 .5rem; border-bottom:1px solid var(--fg);
  }
  .thead .r, .card .r { text-align:right; }

  /* A row, and it must not read as a card. It carried a coloured left edge and a rounded corner
     while sitting flush against the next one — the two devices belong to different things: a card
     is a bounded surface with space around it, a row is a line in a table separated by a rule.
     Having both, with no gap, asked the reader to see cards that had been stacked without margins.
     The state is already said in the badge, twice over, as a word and a coloured dot. */
  .card {
    text-decoration:none; color:var(--md-on-surface); border-bottom:1px solid var(--outlineVariant);
    padding:.75rem .8rem .8rem; margin-left:-.8rem; position:relative;
  }
  .card:hover { background:color-mix(in srgb, var(--primary) 5%, transparent); }
  .card.stopped { opacity:.8; }

  /* A team's heading. Set as a rule with a name on it rather than as a bar: this page separates
     with lines, and a filled band per team would turn a table into a stack of boxes. */
  .teamhead {
    display:flex; align-items:baseline; gap:.7rem; flex-wrap:wrap;
    margin:1.6rem 0 .2rem; padding:0 0 .35rem;
    border-bottom:1px solid var(--fg);
  }
  .teamhead:first-of-type { margin-top:.6rem; }
  .teamhead .tname {
    font:600 12px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase; color:var(--fg);
  }
  .teamhead .thub { font:11px/1.5 var(--mono); color:var(--accent); }
  .teamhead .tn { margin-left:auto; font:11px/1.5 var(--mono); color:var(--muted); }
  .teamhead md-badge { position:static; }

  /* status */
  /* The column's word, for the width where the column heads are not drawn. */
  .colk { display:none; }
  @media (max-width:1000px) {
    .colk {
      display:inline; margin-left:.35rem;
      font:600 10px/1.4 var(--mono); letter-spacing:.14em; text-transform:uppercase; color:var(--muted);
    }
  }
  .card .badge {
    font:600 11px/1.6 var(--mono); letter-spacing:.14em; text-transform:uppercase; color:var(--muted);
    display:flex; align-items:center; gap:.4rem; flex-wrap:wrap;
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
  /* One icon, inside the column. "open" was a word for something the whole row already does — the
     row is a link — and the pair of labelled buttons was wider than the 6rem column they sat in, so
     they hung off the right edge of the table. */
  .actions { display:flex; gap:.2rem; justify-content:flex-end; align-items:center; }
  .actions md-icon-button {
    --md-icon-button-icon-color:var(--muted);
    --md-icon-button-state-layer-width:36px; --md-icon-button-state-layer-height:36px;
    color:var(--muted);
  }
  .actions md-icon-button:hover { --md-icon-button-icon-color:var(--error); color:var(--error); }
  /* open is a link and stays one — it has an address, and a companion's page has to be reachable
     with a middle click and a copied url. It is dressed to match the button beside it. */
  /* One icon, inside the column. "open" was a word for something the whole row already does — the
     row is a link — and the pair of labelled buttons was wider than the 6rem column they sat in, so
     they hung off the right edge of the table. */
  .actions { display:flex; gap:.2rem; justify-content:flex-end; align-items:center; }
  .actions md-icon-button {
    --md-icon-button-icon-color:var(--muted);
    --md-icon-button-state-layer-width:36px; --md-icon-button-state-layer-height:36px;
    color:var(--muted);
  }
  .actions md-icon-button:hover { --md-icon-button-icon-color:var(--error); color:var(--error); }

  /* The grounds a decision is put on: a key-and-value block, set like the rest of this page's
     labelled readings. Two columns on anything but a phone, because the keys are short words and
     giving each its own line would push the reasoning off the screen it exists to be read on. */
  /* No surface of its own. M3 expresses height with TONE, so a tinted box inside a row is a second
     layer, and a second layer inside a region that is already one asks the reader to work out a
     hierarchy that means nothing — the grounds are not deeper than the row, they are part of it.
     Separated by the device this page already uses everywhere else: a rule and a gutter of
     small-caps labels. */
  .grounds {
    grid-column:1 / -1;
    display:grid; grid-template-columns:6.5rem minmax(0, 1fr); gap:.25rem .9rem;
    margin:.7rem 0 .1rem; padding:.7rem 0 0; max-width:var(--measure);
    border-top:1px solid var(--outlineVariant);
  }
  .grounds .gk {
    font:600 10px/1.6 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); text-align:right;
  }
  .grounds .gv { font:12px/1.55 var(--mono); color:var(--fg); overflow-wrap:anywhere; }
  @media (max-width:640px) {
    .grounds { grid-template-columns:1fr; gap:.1rem; }
    .grounds .gk { text-align:left; margin-top:.4rem; }
  }

  /* answering, inline in the row that is asking */
  /* Answering is the one place on the fleet where a person types, so it is the library's field and
     the library's buttons: focus ring, state layers and a 48dp target all come with them. What is
     said here is that the choice is the warning colour, because the agent is stopped until it. */
  .answer { display:flex; gap:.6rem; margin-top:.5rem; flex-wrap:wrap; align-items:center; }
  .answer md-filled-tonal-button {
    --md-filled-tonal-button-container-color:var(--md-surface-container-high);
    --md-filled-tonal-button-label-text-color:var(--fg);
    --md-filled-tonal-button-label-text-font:var(--mono);
    --md-filled-tonal-button-label-text-size:12px;
    letter-spacing:.1em; text-transform:uppercase;
  }
  .answer md-outlined-text-field { flex:1; min-width:11rem; }

  .empty { font:16px/1.7 var(--display); color:var(--muted); padding:2.5rem 0; max-width:52ch; }
  .empty code { font:14px/1 var(--mono); color:var(--accent); }

  /* ── the agent's own header, so a detail page says what it is looking at ──── */
  /* The three panels on a companion's page are md-outlined-card: each one groups what is true
     about a single subject, which is what a card is for, and the outline replaces the hairline
     rule that used to separate them. The rows in the fleet keep their rule and are NOT cards —
     they are links, and this component has no ripple, no focus ring and no role, so making one a
     card would trade the keyboard for a box.

     A card lays its slotted children out itself (:host is flex, and a slot is display:contents),
     so a display of ours on the host wins and the children stay grid items. */
  md-outlined-card { padding:1.1rem 1.2rem; margin-bottom:1.4rem; }
  #detail {
    /* auto-fit at 9rem packed a 60-character workspace path into the same cell as a four-letter
       step count. 14rem is the width of the longest SHORT field (the context reading), so the long
       ones take a whole row of their own instead of squeezing the rest. */
    display:grid; grid-template-columns:repeat(auto-fit, minmax(14rem, auto)); gap:1.2rem 1.6rem;
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
  /* Disabled is the component's own fade now, not a rule here. The contrast check reads this
     stylesheet and cannot see into a shadow root, so that opacity is not covered — which is the
     right answer rather than a gap: WCAG exempts inactive controls, and the repo's own rule
     against dimming was about text somebody still has to read. */
  #detail .f .fold { justify-self:start; margin-top:.4rem; }

  /* The conversation and the facts about it, side by side where there is room. The transcript is
     the wider of the two because its lines are code; the aside is a reading column of short
     labelled facts and does not want more than it needs. */
  #agentview { display:grid; grid-template-columns:minmax(0, 1fr); gap:1.6rem; }
  @media (min-width:1100px) {
    #agentview { grid-template-columns:minmax(0, 1fr) 22rem; align-items:start; }
    /* The facts stay put while the conversation scrolls: on this page they are the thing you keep
       glancing back at, and a plan that scrolls away is one you re-find rather than read. */
    #side { position:sticky; top:5.5rem; }
  }
  #stream, #side { min-width:0; display:flex; flex-direction:column; gap:1.4rem; }
  #side md-outlined-card { margin-bottom:0; }
  #side #plan, #side #handoffs, #side #history { max-width:none; }

  /* ── the agent's own plan ───────────────────────────────────────────────── */
  #plan { max-width:var(--measure); }
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
  #handoffs { max-width:var(--measure); }

  /* ── the board: a column per companion, a card per piece of work ────────── */
  #board { display:block; max-width:var(--page); }
  .boardhead { display:flex; gap:.9rem; align-items:end; margin:0 0 1.2rem; }
  /* Scrolls sideways, and ONLY here. The page must never do it, but a board of lanes is the one
     shape where sideways is the reading direction, and clipping a lane would hide a companion. */
  .lanes { display:flex; gap:1.4rem; align-items:flex-start; overflow-x:auto; padding-bottom:.6rem; }
  /* A fixed lane width, and it has to be spelled out three ways: flex-basis alone loses to the
     content's own minimum, so a long title widened its lane and a short one narrowed it, and the
     columns stopped lining up — which is the one thing a board is for. */
  .lane { flex:0 0 15rem; width:15rem; min-width:15rem; }
  /* Only when there is more board than room. Without this a single lane still drew a scrollbar,
     which reads as "there is more over there" when there is not. */
  /* The lanes scroll, so the strip must reach the page's edge rather than stopping at the reading
     column's — a lane half off the right of a 150ch box, with room to its right, reads as clipped
     rather than as scrollable. */
  .lanes { scrollbar-width:thin; }
  #board { padding-right:0; }
  .lanes::after { content:""; flex:0 0 1.4rem; }   /* the last lane gets a right edge too */
  .lanehead {
    display:flex; gap:.6rem; align-items:baseline;
    border-bottom:1px solid var(--fg); padding-bottom:.35rem; margin-bottom:.6rem;
  }
  .lanehead .lname {
    font:600 12px/1.4 var(--mono); letter-spacing:.14em; text-transform:uppercase; color:var(--fg);
  }
  .lanehead .lcount { margin-left:auto; font:11px/1.5 var(--mono); color:var(--muted); }
  .wcard {
    border:1px solid var(--outlineVariant); border-radius:var(--shape-s);
    padding:.6rem .7rem; margin-bottom:.6rem; background:var(--md-surface-container-low);
  }
  .wcard .wwhen { font:11px/1.5 var(--mono); color:var(--muted); }
  .wcard .wwhat { font-size:13px; line-height:1.5; color:var(--fg); overflow-wrap:anywhere; }
  /* The one running now, in the colour the rest of the page uses for that. */
  .wcard.now { border-color:var(--success); }
  .wcard.now .wwhen { color:var(--success); font-weight:600; }

  /* ── what this companion did before now ─────────────────────────────────── */
  #history { max-width:var(--measure); }
  #history .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted); margin-bottom:.5rem;
  }
  .hs { display:grid; grid-template-columns:5.5rem 1fr; gap:.3rem 1.2rem; padding:.35rem 0; }
  .hs + .hs { border-top:1px solid var(--outlineVariant); }
  .hs .when { font:11px/1.6 var(--mono); color:var(--muted); text-align:right; }
  .hs .what { font-size:13px; color:var(--fg); overflow-wrap:anywhere; }
  /* The one it is in now is work too, and it is the newest row. Marked rather than left off. */
  .hs.now .when { color:var(--success); font-weight:600; }
  #handoffs .k {
    font:600 11px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted); margin-bottom:.5rem;
  }
    .ho { display:grid; grid-template-columns:8rem 1fr; gap:.3rem 1.2rem; padding:.6rem 0; }
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
  #prompt .inner { max-width:var(--wide); }
  #prompt .asking { font:600 14px/1.5 var(--mono); color:var(--warn); overflow-wrap:anywhere; }

  /* ── composer ───────────────────────────────────────────────────────────── */
  form {
    padding:1rem 1.4rem; display:flex; justify-content:center;
  }
  .composer {
    display:flex; gap:.9rem; width:100%; max-width:var(--wide); align-items:flex-end;
    border-top:1px solid var(--fg); padding-top:.8rem;
  }
  /* The composer's field is an M3 outlined text field. Its outline, focus behaviour, floating
     label slot and 56dp height are the component's; what is said here is that it takes the row and
     which colours magi uses. Its own scrolling replaces the auto-grow this page used to do by
     measuring scrollHeight — a measurement that stopped being possible when the textarea moved
     into a shadow root, and one the component already does. */
  /* The small acting words on a row — open a companion, stop a turn, drop a lesson, remove a
     server, fold a transcript. Every one of them is md-text-button now, and it is told what to be
     through the component's tokens: a rule on the host can only draw a second box AROUND the
     button (the border-bottom that used to be here did exactly that), and the font it set never
     reached the label, which lives in a shadow root. letter-spacing and text-transform are the
     two that do cross the boundary, being inherited properties, so those stay as they are. */
  /* An armed control asks rather than warns, and it is the asking that is red. Before this the
     error colour was on :hover, which a touch screen does not have — so the surface where a
     misplaced thumb is likeliest carried no signal at all. */
  md-text-button.armed {
    --md-text-button-label-text-color:var(--error);
    --md-text-button-hover-label-text-color:var(--error);
    --md-text-button-hover-state-layer-color:var(--error);
  }
  md-text-button {
    --md-text-button-label-text-font: var(--mono);
    /* label-large, the role M3 assigns to a button. It was 11px — label-SMALL, a scale value in the
       wrong role — on eight of the page's twelve buttons. The editorial identity is the face and
       the letterspacing, both of which stay; M3 asks for a different typeface to keep the scale,
       not for the scale to be shrunk to fit a look. */
    --md-text-button-label-text-size: 14px;
    --md-text-button-label-text-line-height: 20px;
    --md-text-button-label-text-weight: 600;
    --md-text-button-label-text-color: var(--muted);
    --md-text-button-hover-label-text-color: var(--primary);
    --md-text-button-focus-label-text-color: var(--primary);
    --md-text-button-pressed-label-text-color: var(--primary);
    --md-text-button-hover-state-layer-color: var(--primary);
    --md-text-button-pressed-state-layer-color: var(--primary);
    letter-spacing:.14em; text-transform:uppercase;
  }
  /* Removing something reads in the error colour on the way to being pressed, and only there: a
     control that is red at rest is a warning, and these are ordinary. */
  md-text-button.drop, md-text-button.stop {
    --md-text-button-hover-label-text-color: var(--error);
    --md-text-button-focus-label-text-color: var(--error);
    --md-text-button-pressed-label-text-color: var(--error);
    --md-text-button-hover-state-layer-color: var(--error);
    --md-text-button-pressed-state-layer-color: var(--error);
  }
  md-outlined-text-field#t { flex:1; }
  md-outlined-text-field {
    --md-sys-color-primary: var(--primary);
    --md-sys-color-on-surface: var(--md-on-surface);
    --md-sys-color-on-surface-variant: var(--md-on-surface-variant);
    --md-sys-color-outline: var(--outline);
    --md-sys-color-surface: transparent;
    --md-outlined-text-field-input-text-font: var(--mono);
    /* 16px, and not because the scale says so: under 16 iOS Safari zooms the page when a field
       takes focus and does not zoom back. The component's own default is smaller. */
    --md-outlined-text-field-input-text-size: 16px;
    --md-outlined-text-field-label-text-font: var(--mono);
  }
  /* The select is a text field wearing a menu, and it reads its own copy of these. */
  md-outlined-select {
    --md-sys-color-primary: var(--primary);
    --md-sys-color-on-surface: var(--md-on-surface);
    --md-sys-color-on-surface-variant: var(--md-on-surface-variant);
    --md-sys-color-outline: var(--outline);
    --md-sys-color-surface-container: var(--md-surface-container);
    --md-outlined-select-text-field-input-text-font: var(--mono);
    --md-outlined-select-text-field-input-text-size: 16px;
    --md-outlined-select-text-field-label-text-font: var(--mono);
  }
  /* The composer's two are Material Web buttons. Their shape, state layers, ripple and touch
     target come from the component — this page only tells them which colours magi uses, through
     the --md-sys-* properties the library reads. Writing any of the rest here again is how two
     descriptions of one button start to disagree. */

  /* State layers, not colour swaps: M3 puts the on- colour over the surface at a fixed opacity.
     Doing it with a pseudo-element keeps the label's own contrast untouched, which dimming or
     recolouring the text does not. */
  /* The components bring their own focus ring; this stays for anything in the composer that is
     still a plain control. */
  .composer button:focus-visible { outline:3px solid var(--primary); outline-offset:2px; }

  /* ── the table, when the table does not fit ──────────────────────────────
     A separate breakpoint from the navigation's, because it answers a different question. 768px is
     where a rail stops being worth its width; this is where seven columns stop fitting, which is a
     fact about the columns. Tying the two together would mean moving one every time the other's
     reason changed.

     The row's own comment used to say it "collapses to two lines on a phone". Nothing collapsed it
     — the comment described a mechanism that was never written, and the page scrolled sideways at
     every width instead. */
  @media (max-width:1000px) {
    .thead { display:none; }   /* no columns left to label */
    .card {
      grid-template-columns:auto auto 1fr;
      gap:.3rem .9rem; padding:1rem .8rem 1.1rem;
    }
    /* Everything takes the full width unless it is placed. The cells stay exactly as they are —
       a row still has as many cells as the head has columns — and only where they sit changes. */
    .card > * { grid-column:1 / -1; }
    .card .badge { grid-column:1 / 3; grid-row:1; }
    .card .actions { grid-column:3; grid-row:1; justify-content:flex-end; }
    /* The three short readings share one line at the foot of the row rather than taking three. */
    .card .num, .card .host { grid-column:auto; }
    .card .num.r { text-align:left; }
  }

  @media (max-width:640px) {
    /* The two buttons and a text box do not fit across 390px: measured, the box was left with
       about a third of the row and the placeholder was cut mid-sentence. They take their own line,
       which also puts them under the thumb rather than beside it. */
    /* Two rows, not three. Measured at 390px: the dock was 261px, 36% of the screen, because the
       address, the box and the button each took a line of their own. The box gets its row and the
       other two share the next one — the send button is a word, and a word does not need a row.
       Ordered rather than reordered in the markup: the tab order that reads to → message → send is
       the right one, and only where they SIT changes. */
    .composer { flex-wrap:wrap; align-items:end; }
    .composer md-outlined-text-field#t { order:1; }
    .composer md-outlined-text-field#to { order:2; flex:1 1 8rem; min-width:7rem; }
    .composer md-filled-button, .composer md-filled-tonal-button { order:3; flex:0 0 auto; }
    /* The component, not the element it replaced. These two rules named "textarea" and "button",
       which the composer has not held since it became Material Web — dead selectors, so on a phone
       the field never took its own row and was squeezed to a third of one. The same slip as the
       host rules that could not reach a label: a migration that leaves the old CSS behind leaves
       it pointing at nothing. */
    .composer md-outlined-text-field#t { flex:1 0 100%; }
    header { padding-left:1rem; padding-right:1rem; }
    main { padding:1.2rem 1rem calc(var(--dock, 8rem) + 1.5rem); }
    .card .name { font:600 var(--title-l) var(--display); }
    .row { grid-template-columns:1fr; gap:.2rem; }
    .who { text-align:left; }
    .row.user .txt { font-size:16px; }
    form { padding-left:1rem; padding-right:1rem; }

  }

  /* ── the two widths ─────────────────────────────────────────────────────
     The breakpoint is 600px, which is M3's own: compact is below 600, medium is 600–839, expanded is
     840 and up, and the guide puts a rail on medium and above. It was 768 — a number taken from the
     handbook — and measuring against the guide showed the cost: a 700px window is medium, the guide
     asks for a rail there, and this page was still drawing tabs.

     LAST in the stylesheet, and that is load-bearing. A media query adds no specificity, so these
     rules only win by coming after the ones they override — and they did not. Written above the
     sections they contradict, the wide rule lost to a later "#tabs { display:flex }" and the page
     offset lost to a later "padding:" shorthand, which resets the padding-left this sets. The
     result was both navigations at once on a desktop and, on a narrow screen, a fixed rail sitting
     on top of the page it was supposed to stand beside. */

  /* Wide: the rail IS the navigation, so the tabs go — two of them for one set of four sections is
     one too many. The page starts to the right of the rail, by exactly its width. */
  @media (min-width:600px) {
    header, main { padding-left:calc(var(--rail-w) + 1.9rem); }
    #dock { padding-left:var(--rail-w); }
    #tabs, #themeToggle { display:none; }
    }
  /* Compact (below 600px, M3's own boundary): the tabs navigate, so there is no hamburger — a menu button next to a row of tabs is
     an invitation to look in the wrong place. What is left behind the button is the preferences,
     so on this width it says so and wears a gear. */
  /* Narrow: no drawer at all. The tabs navigate, the theme has its own toggle in the masthead, and
     what is left — language, and which machine this is — is a plain section at the foot of the
     page. A drawer for two selects is a door in front of a cupboard, and on a phone it covered the
     thing the reader had just navigated to. */
  /* ── the phone's first screen ────────────────────────────────────────────
     Measured on a 730px screen: the masthead, the tabs and the filters took 455px before the first
     agent, and a fleet page that shows one and a half rows is a list you scroll to read rather than
     one you glance at. Everything below is that 455 coming down. */
  @media (max-width:599px) {
    /* The masthead on one line. It wrapped, so the brand and the count each took a row. */
    header { padding-top:calc(.5rem + env(safe-area-inset-top)); padding-bottom:.4rem; gap:.6rem; }
    header .mark { font-size:20px; }
    /* The count on the SAME line as the brand. Given its own row it cost 40px of the first screen
       to say something that fits beside a five-letter word. It is allowed to shrink and to clip:
       "5 agents · 2 waiting" is legible at any truncation that keeps the number. */
    /* One line, clipped at the end rather than wrapped. Squeezed between the brand and two icons it
       broke "5 AGENTS ·" across three rows, which is taller than the two-row masthead it replaced. */
    #state {
      font-size:10px; letter-spacing:.08em; margin-left:auto; min-width:0;
      white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
    }
    /* The two icon buttons sit at the end of the same line, not on one of their own. Adding the
       gear pushed the masthead back onto two rows and the first agent from 337px to 393px —
       everything gained on this width, given straight back. */
    header { flex-wrap:nowrap; }
    #prefs, #themeToggle {
      flex:none;
      --md-icon-button-state-layer-width:36px; --md-icon-button-state-layer-height:36px;
    }
    #state .jump { --md-text-button-label-text-size:10px; }
    /* The crumb is hidden only where the tab strip below already says the same thing. On a
       companion's page there are no tabs, and hiding it left a masthead reading "magi" with no
       word anywhere for WHICH companion — the one question that page exists to answer. */
    body:not([at="agent"]) #crumbs { display:none; }
    #crumbs { font-size:11px; }
    /* The filters as one scrolling row rather than three stacked ones. Four chips do not fit across
       390px and never will; a row that scrolls keeps them one line high and keeps the fourth
       reachable, which stacking also did but at three times the cost. */
    #summary {
      flex-wrap:nowrap; overflow-x:auto; scrollbar-width:none;
      margin:.9rem 0 .2rem; padding-bottom:.7rem;
    }
    #summary::-webkit-scrollbar { display:none; }
    .tile { flex:0 0 auto; }
    /* The tab strip is a navigation, not a heading: it does not need the room a heading takes. */
    #tabs { --md-primary-tab-container-height:44px; }
  }
  @media (max-width:599px) {
    #rail {
      position:static; transform:none; width:auto; overflow:visible;
      border-right:0; border-top:1px solid var(--outlineVariant);
      background:none; padding:1.4rem 1.4rem 2rem; margin-top:1.5rem;
      order:9;   /* last on the page, first-but-one in the tab order it is written in */
    }
    /* Nothing but navigation, and on this width the tabs do that — so the rail is not drawn at
       all. The preferences it used to carry are in the dialog now. */
    #rail { display:none; }
    }

</style>

<header>
  <!-- The hamburger is on both sizes and means two different things, which is the point: on a wide
       screen it widens the rail into a drawer, on a narrow one it slides that drawer in over the
       page. Either way it is the way to the settings, which is why a phone has it too even though
       a phone navigates with the tabs. -->
  <span class="mark">magi</span>
  <!-- Where you are, always, in both views: magi / fleet, or magi / fleet / <agent>. The middle
       crumb is the way back, which is the same element that says where back goes. -->
  <nav id="crumbs"><a href="/" id="back">companions</a><span id="crumbSep" hidden>/</span><span id="crumbHere"></span></nav>
  <span class="sid" id="sid"></span>
  <span id="state"></span>
  <!-- Narrow only, and top right where a thumb reaches. It shows what pressing it GIVES you — a sun
       while the page is dark — because a control showing its current state leaves you working out
       what it does. Two shapes with one hidden, rather than a morphing path: an icon that changes
       shape is an icon that has to be got right twice. -->
  <!-- Sliders, not a cog. A cog beside the theme toggle is a circle with spokes next to a circle
       with rays: at 21px they are the same picture, and the two controls sat side by side. -->
  <md-icon-button id="prefs">
    <svg viewBox="0 0 24 24" width="21" height="21" aria-hidden="true">
      <path d="M4 7h9M17 7h3M4 12h3M11 12h9M4 17h9M17 17h3"
            stroke="currentColor" stroke-width="1.8" stroke-linecap="round" fill="none"/>
      <circle cx="15" cy="7" r="2" stroke="currentColor" stroke-width="1.8" fill="none"/>
      <circle cx="9" cy="12" r="2" stroke="currentColor" stroke-width="1.8" fill="none"/>
      <circle cx="15" cy="17" r="2" stroke="currentColor" stroke-width="1.8" fill="none"/>
    </svg>
  </md-icon-button>
  <md-icon-button id="themeToggle">
    <svg class="sun" viewBox="0 0 24 24" width="21" height="21" aria-hidden="true">
      <circle cx="12" cy="12" r="4.2" stroke="currentColor" stroke-width="1.8" fill="none"/>
      <path d="M12 2.4v2.8M12 18.8v2.8M21.6 12h-2.8M5.2 12H2.4M18.8 5.2l-2 2M7.2 16.8l-2 2M18.8 18.8l-2-2M7.2 7.2l-2-2"
            stroke="currentColor" stroke-width="1.8" stroke-linecap="round" fill="none"/>
    </svg>
    <svg class="moon" viewBox="0 0 24 24" width="21" height="21" aria-hidden="true">
      <path d="M20 14.2A8.4 8.4 0 0 1 9.8 4a8.4 8.4 0 1 0 10.2 10.2z"
            stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" fill="none"/>
    </svg>
  </md-icon-button>
</header>

<!-- The rail is the wide screen's navigation and the narrow screen's settings drawer — one element
     in two modes rather than two that have to agree. Its items are md-list-item with an href, so
     they are real links with the component's ripple and focus ring: a rail you cannot middle-click
     is a navigation that forgot it was made of addresses. -->

<nav id="rail">
  <!-- The button that widens the rail lives IN the rail, beside what it moves. In the masthead's
       far corner it did not look like it belonged to the column across the page. -->
  <md-icon-button id="railMenu" aria-expanded="false">
    <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <path d="M3 6h18M3 12h18M3 18h18" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
    </svg>
  </md-icon-button>
  <!-- The icons are drawn here rather than pulled from Material Symbols: that is a whole font to
       embed for four glyphs, and this page already refuses a font CDN for the typeface it reads
       with. Stroked in currentColor, so the selected state colours the shape with its label. -->
  <md-list id="railNav">
    <md-list-item id="railFleet" type="link">
      <!-- The badge rides the icon, which is what a badge is for and the only place it can be when
           the rail is collapsed to icons. In the end slot it sat at the right edge of the item,
           46px from the shape it was counting for. -->
      <span slot="start" class="icwrap">
        <svg class="ic" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path
          d="M4 19v-1.6a3.4 3.4 0 0 1 3.4-3.4h2.2a3.4 3.4 0 0 1 3.4 3.4V19M8.5 6.2a2.6 2.6 0 1 1 0 5.2 2.6 2.6 0 0 1 0-5.2M15.5 19v-1.6a3.4 3.4 0 0 0-1.2-2.6M15 6.4a2.6 2.6 0 0 1 0 5"
          fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
        <md-badge id="railBadge" hidden></md-badge>
      </span>
      <span class="lbl"></span>
    </md-list-item>
    <md-list-item id="railSkills" type="link">
      <svg slot="start" class="ic" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path
        d="M5 4.5h9.5A2.5 2.5 0 0 1 17 7v12.5H7.5A2.5 2.5 0 0 1 5 17zM19 6.5v13M8.5 8.5h5M8.5 11.5h5"
        fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
      <span class="lbl"></span>
    </md-list-item>
    <md-list-item id="railBoard" type="link">
      <svg slot="start" class="ic" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path
        d="M4 5.5h5v13H4zM9.5 5.5h5v8h-5zM15 5.5h5v10.5h-5z"
        fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
      <span class="lbl"></span>
    </md-list-item>
    <md-list-item id="railMcp" type="link">
      <svg slot="start" class="ic" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path
        d="M9.5 14.5 5.8 18.2M14.5 9.5l3.7-3.7M7.4 11.1 5.6 12.9a3.2 3.2 0 0 0 4.5 4.5l1.8-1.8M12.1 6.6l1.8-1.8a3.2 3.2 0 0 1 4.5 4.5l-1.8 1.8M9.8 14.2l4.4-4.4"
        fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
      <span class="lbl"></span>
    </md-list-item>
  </md-list>

</nav>

<main>
  <md-tabs id="tabs" hidden>
    <!-- The label is its own element because the badge is a sibling: writing the word with
         textContent would take the badge with it, the way setting textContent replaces everything
         a node holds. -->
    <!-- Label and badge in ONE slotted element. A tab stacks what is slotted into it — it is built
         for an icon above a word — so two siblings put the count on a line below the label. -->
    <md-primary-tab id="tabFleet"><span class="tablbl"><span class="lbl"></span><span class="icwrap badgewrap"><md-badge id="tabBadge" hidden></md-badge></span></span></md-primary-tab>
    <md-primary-tab id="tabSkills">lessons</md-primary-tab>
    <md-primary-tab id="tabBoard"></md-primary-tab>
    <md-primary-tab id="tabMcp"></md-primary-tab>
  </md-tabs>
  <md-chip-set id="summary"></md-chip-set>
  <div id="skills" hidden></div>
  <div id="board" hidden></div>
  <div id="mcp" hidden></div>
  <div id="fleet"></div>
  <!-- Two reasons to be on a companion's page, and they want different things. Checking on it wants
       the facts: state, workspace, model, context, plan. Reading what it is doing and steering it
       wants the transcript and the box under it.
       
       Stacked, the transcript was 1335px down a 1267px screen — off it — behind five cards of which
       three are history. So the conversation is the page and the facts stand beside it, and on a
       narrow screen the conversation comes first with the rest underneath. -->
  <div id="agentview">
    <div id="stream">
      <md-outlined-card id="detail" hidden></md-outlined-card>
      <div id="log"></div>
    </div>
    <aside id="side">
      <md-outlined-card id="plan" hidden></md-outlined-card>
      <md-outlined-card id="handoffs" hidden></md-outlined-card>
      <md-outlined-card id="intervened" hidden></md-outlined-card>
      <md-outlined-card id="history" hidden></md-outlined-card>
    </aside>
  </div>
</main>


<!-- Preferences, in one place instead of four. As a section at the foot of every page these were
     the same three controls repeated under lists somebody was reading for something else; as a
     panel in the rail they were a second copy of that. A dialog is what M3 uses for a short task
     that interrupts nothing, and it is the same dialog at every width. -->
<md-dialog id="prefsDialog">
  <div slot="headline" id="prefsK"></div>
  <!-- No theme here. It has a toggle in the masthead — one tap for the setting that gets changed
       most — and a select saying the same thing three feet away was the same preference twice, with
       two ways to be wrong about it. What is left is what a toggle cannot carry: a choice of three
       languages, and which machine this is. -->
  <form slot="content" method="dialog" id="prefsForm">
    <md-outlined-select id="lang"></md-outlined-select>
    <div class="k" id="consoleK"></div>
    <div id="console"></div>
  </form>
  <div slot="actions">
    <md-text-button id="prefsClose" form="prefsForm" value="close"></md-text-button>
  </div>
</md-dialog>

<footer id="dock">
  <div id="prompt" hidden></div>
  <form id="f" hidden><div class="composer">
    <!-- On the fleet view the composer is addressed: the work goes to whoever does that, and which
         machine they are on is not the asker's problem. On one companion's page it is hidden,
         because the address is the page you are standing on. -->
    <md-outlined-text-field id="to" hidden list="roles"></md-outlined-text-field>
    <datalist id="roles"></datalist>
    <md-outlined-text-field id="t" type="textarea" rows="1"
      ></md-outlined-text-field>
    <md-filled-button type="submit" id="send">send</md-filled-button>
    <md-filled-tonal-button type="button" id="stop">interrupt</md-filled-tonal-button>
  </div></form>
</footer>

<script type="module">
// RxJS, from this binary (vendor/README.md says how it was built). The console has three streams
// that used to be hand-rolled: the language pack every label reads, the fleet poll, and the
// transcript. Hand-rolling them is what produced the races this page has already been caught by —
// a slow answer landing on a panel that had been redrawn, a poll that kept firing after you left.
// The Material Web components. Imported for the side effect — each module registers its custom
// element — so the M3 design comes from the system instead of being written here a second time.
import '/vendor/material.js';   // buttons, text fields, tabs
import { BehaviorSubject, timer, from, of, EMPTY,
         switchMap, catchError, map, distinctUntilChanged, shareReplay,
         filter as onlyWhen } from '/vendor/rxjs.js';

// ── labels ───────────────────────────────────────────────────────────────────
// The same shape the handbook uses: a flat dot-keyed pack per locale, chosen by localStorage then
// the browser, falling back to English when the pack cannot be read. Published as a stream so a
// label change reaches everything that draws one, rather than being read once at startup by
// whichever function happened to run first.
// Where this page is mounted. The binary serves it at the root, so BASE is '/' and every url the
// router builds has looked like '/?v=skills' for that reason. A static copy of the same page lives
// under /<repo>/ on a project site, where those escape to the domain root: the clicks still work
// because they are intercepted, but the address pushed is wrong and a reload lands nowhere.
//
// Read from the document rather than configured, so nothing has to be told where it was put.
const BASE = location.pathname.replace(/[^/]*$/, '');
const at = query => BASE + (query || '');

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
// What the reader most likely reads, in the order that respects a stated choice over a guess: this
// console's own setting, then every language the browser lists (navigator.languages is ordered by
// preference, and reading only navigator.language ignores the rest of it), then English.
//
// Matched against the packs that exist, so a browser asking for a language nothing is written in
// falls through to the next one it asked for rather than to a 404.
const PACKS = ['en', 'ko'];
// The two preferences, written out. Every key is a literal because the check that a label exists in
// both language packs reads them out of this file — a key assembled at runtime ('pref.' + kind) is
// one the check cannot see, and an unseen key renders as its own dotted name on somebody's screen.
const CHOICES = {
  theme: {
    label: 'pref.theme',
    options: [['system', 'pref.theme.system'], ['light', 'pref.theme.light'], ['dark', 'pref.theme.dark']],
  },
  lang: {
    // Kept in step with PACKS by a test, not by care.
    label: 'pref.lang',
    options: [['system', 'pref.lang.system'], ['en', 'pref.lang.en'], ['ko', 'pref.lang.ko']],
  },
};
// A preference is what somebody chose, and 'system' is a choice too — the choice to keep asking the
// machine. Stored as that word rather than as the resolved value, so a reader who picks 'system' on
// a light morning is still following the machine that evening.
const prefOf = kind => localStorage.getItem(kind) || 'system';
// The theme is an attribute on the root, which is what the stylesheet's two override blocks read.
// Nothing is written for 'system': the absence of the attribute IS the deferral, and the
// prefers-color-scheme query underneath answers.
function applyTheme() {
  const want = prefOf('theme');
  if (want === 'system') document.documentElement.removeAttribute('color-theme');
  else document.documentElement.setAttribute('color-theme', want);
}
applyTheme();
// What the page is actually showing, which is not the same as what was chosen: 'system' resolves
// through the machine. The toggle needs the resolved answer to know which way to flip.
const showing = () => {
  const want = prefOf('theme');
  if (want !== 'system') return want;
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
};
const locale = () => {
  const chosen = prefOf('lang');
  const asked = [chosen === 'system' ? null : chosen, ...(navigator.languages || [navigator.language])];
  for (const tag of asked) {
    const code = (tag || '').slice(0, 2).toLowerCase();
    if (PACKS.includes(code)) return code;
  }
  return 'en';
};
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
// Named, because it runs again when somebody picks a language rather than only at startup. A
// reload would do it too and would throw away the transcript to change one word.
function loadPack() {
  const want = locale();
  pack$(at('i18n/language.' + want + '.json'))
    .pipe(catchError(() => EMPTY))
    .subscribe({
      next: pack => labels$.next(pack),
      complete: () => {
        if (Object.keys(L).length || want === 'en') return;
        pack$(at('i18n/language.en.json')).subscribe(pack => labels$.next(pack));
      },
    });
}
loadPack();
// Anything already on screen is repainted when a pack lands. Guarded on the first paint having
// happened: this subscribes before the page's own elements are declared, and a BehaviorSubject
// hands its current value to a new subscriber immediately — painting there would reach for
// constants that do not exist yet.
let painted = false;

// Whose console this is. Not an account — magi has no users to log in — but the two facts that
// answer "am I looking at the right machine": the host it runs on and the config directory it
// reads. A supervisor with three of these open in three tabs has asked that question.
function loadConsole() {
  fetchList('/console').then(c => {
    if (!c) return;
    consoleEl.replaceChildren();
    for (const [k, val] of [['field.host', c.host], ['field.config', c.configDir]]) {
      if (!val) continue;
      const line = cell('');
      const b = document.createElement('b');
      b.textContent = tr(k) + ' ';
      line.append(b, document.createTextNode(val));
      consoleEl.append(line);
    }
  });
}
labels$.pipe(distinctUntilChanged()).subscribe(() => { if (painted) paint(); });

const fleetEl = document.getElementById('fleet'), log = document.getElementById('log');
const state = document.getElementById('state'), sidEl = document.getElementById('sid');
const back = document.getElementById('back'), f = document.getElementById('f');
const summaryEl = document.getElementById('summary');
const tabsEl = document.getElementById('tabs');
const intervenedEl = document.getElementById('intervened');
const skillsEl = document.getElementById('skills'), tabSkills = document.getElementById('tabSkills');
const boardEl = document.getElementById('board'), tabBoard = document.getElementById('tabBoard');
const railBoard = document.getElementById('railBoard');
const mcpEl = document.getElementById('mcp'), tabMcp = document.getElementById('tabMcp');
// The last fleet answer, so the "which companion" picker names them without a second fetch.
let fleetSeen = [];
const tabFleet = document.getElementById('tabFleet');
const railEl = document.getElementById('rail');
const langEl = document.getElementById('lang');
const prefsK = document.getElementById('prefsK'), consoleK = document.getElementById('consoleK');
const prefsEl = document.getElementById('prefs');
const prefsDialog = document.getElementById('prefsDialog');
const prefsClose = document.getElementById('prefsClose');
const railMenu = document.getElementById('railMenu');
const railBadge = document.getElementById('railBadge'), tabBadge = document.getElementById('tabBadge');
const themeToggle = document.getElementById('themeToggle');
const consoleEl = document.getElementById('console');
const historyEl = document.getElementById('history');
const railFleet = document.getElementById('railFleet');
const railSkills = document.getElementById('railSkills'), railMcp = document.getElementById('railMcp');
// Which resource this console is showing. A companion's own page is neither — it is one level in.
// Corrections used to be a destination of its own and is now the first half of the experience
// page. An address somebody kept still lands on the thing it was pointing at.
const RENAMED = {interventions: 'skills'};
const view = () => {
  const v = new URLSearchParams(location.search).get('v') || 'fleet';
  return RENAMED[v] || v;
};
const crumbSep = document.getElementById('crumbSep'), crumbHere = document.getElementById('crumbHere');
// The four sections, named as nouns: a tab is a place you are, and "what I had to say" is a
// sentence about it. The same words do three jobs — the tab, the crumb, and the browser title —
// so they are written once.
const SECTION_KEY = {fleet: 'nav.companions', skills: 'nav.lessons',
                     board: 'nav.board', mcp: 'nav.connections'};
const SECTION = new Proxy({}, {get: (_, v) => tr(SECTION_KEY[v] || 'nav.companions')});

const HREF = {fleet: '', skills: '?v=skills', board: '?v=board', mcp: '?v=mcp'};
// In the order they are written in the markup, because md-tabs addresses its tabs by index.
const TABS = ['fleet', 'skills', 'board', 'mcp'];

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

  const badge = cell('badge', a.state);
  // How far through its own plan, INSIDE the status cell. Not a progress bar: a todo list is not a
  // schedule, and a bar would promise a completion time nobody can honour.
  //
  // Inside, because it is conditional and the row is a grid of seven fixed columns. As a sibling it
  // was an eighth item on the rows that had a plan: every cell after it shifted one column right,
  // the task landed in the step count's 72px, and the actions wrapped onto a line of their own.
  // A conditional cell in a fixed template is a row that reads differently depending on its data.
  if (a.planTotal) badge.append(cell('plan', a.planDone + '/' + a.planTotal));
  el.append(badge);

  const who = cell('who-col');
  who.append(cell('name', a.name));
  // What it is for, when it says so. The path stays: a role is how you pick a companion and a
  // path is how you go and look at it, and neither answers the other's question.
  if (a.role) who.append(cell('role', a.role));
  // The team is NOT repeated here. It is on the group's heading, which is where it belongs — and
  // with grouping on whenever any companion declares one, a team cell in the row could only ever
  // say again what the line above it just said. Unreachable the rest of the time, because "no
  // grouping" and "no teams" are the same condition.
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

  // The step count carries its own word on a phone. The column heads are hidden there, so this and
  // the two beside it were three unlabelled readings — "3  4s ago  studio" — and only the age says
  // what it is on its own.
  const steps = cell('num r', a.steps ? a.steps + '' : '—');
  steps.append(cell('colk', tr('field.steps')));
  el.append(steps);
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
  // The grounds go last and take the whole row. They are paragraphs, and a paragraph in a table
  // column 12rem wide comes out one word per line — measured, and it looked like a bug rather than
  // like reasoning. Marked "span" so the check that a row has as many cells as the head has
  // columns still counts columns: this is not one.
  const why = grounds(a);
  if (why) el.append(why);
  return el;
}

// rowActions: stopping, and only that.
//
// "open" is gone. The whole row is a link to the companion, so a button beside it saying "open" was
// a second way to do what a click anywhere already did — and the pair of labelled buttons was wider
// than the column they sit in, which is why they hung off the right edge of the table.
//
// Stopping stays, and it stays HERE rather than one level in: the row you want to halt is the one
// you are already looking at, and making somebody open it first is how a runaway turn gets another
// thirty seconds. As an icon, because it is one action in a 6rem column and the word did not fit.
function rowActions(a) {
  const box = cell('actions');
  if (!(a.live && (a.state === 'working' || a.state === 'waiting'))) return box;
  const stop = document.createElement('md-icon-button');
  stop.className = 'stop';
  stop.setAttribute('aria-label', tr('action.interrupt'));
  stop.title = tr('action.interrupt');
  stop.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
    '<rect x="7" y="7" width="10" height="10" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>';
  stop.onclick = e => {
    e.preventDefault(); e.stopPropagation();
    post('/interrupt', null, a.socket, a.peer).then(loadFleet);
  };
  box.append(stop);
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
    const b = document.createElement('md-filter-chip');
    b.className = 'tile ' + k;
    b.disabled = n === 0;
    // The chip's own selected state, not an aria attribute of ours. It toggles itself on click and
    // this list is rebuilt from filter on the next render, so the two cannot drift.
    b.selected = filter === k;
    b.append(cell('n', n + ''), cell('k', k));
    b.onclick = () => {
      filter = filter === k ? null : k;
      render();
      if (filter) jumpToFirstRow();
    };
    return b;
  }));
}

// grounds is the report the agent wrote for whoever has to decide.
//
// Drawn as a labelled block rather than folded into the question, because it is not the question:
// the question is one line and this is the working behind it — what was tried, what each way
// costs, which way the agent leans. Its sections are whatever the decision-report skill asked for,
// so nothing here names them; they arrive in the order that skill put them in, and that order is
// part of the report.
//
// Returns null when there are none. A companion running an older build, or one whose report was
// dropped somewhere between its socket and this page, gets the prompt it always had rather than an
// empty box implying somebody left the grounds blank.
function grounds(a) {
  if (!a.report || !a.report.length) return null;
  const box = cell('grounds span');
  for (const sec of a.report) {
    if (!sec || !sec.text) continue;
    box.append(cell('gk', sec.key), cell('gv', sec.text));
  }
  return box.children.length ? box : null;
}

// markWaiting puts the count on the companions section, in both navigations.
//
// Hidden at zero rather than shown as "0": a badge is there to be noticed, and one that is always
// present is one the eye stops reading. Both surfaces are set from here so the rail and the tabs
// cannot end up claiming different numbers.
function markWaiting(n) {
  for (const b of [railBadge, tabBadge]) {
    b.value = n ? String(n) : '';
    b.hidden = !n;
  }
}

// arm makes a destructive control ask once before it acts.
//
// M3's answer to a destructive action is a confirmation, not a colour, and this page had neither:
// forget and remove posted on the first press, and the only warning was an error colour that
// appeared on HOVER — which does not exist on a phone, so on the surface where a misplaced thumb is
// likeliest there was no warning at all.
//
// Two presses rather than a dialog. A dialog for "forget this lesson" is heavier than the act it
// guards, and it would take a component this page does not carry; the second press is the same
// gesture, one row away from where the eye already is. The error colour arrives with the question,
// so the colour appears exactly when it means something.
//
// It disarms itself after a few seconds. An armed control left sitting is one that will be pressed
// by somebody who has forgotten what it asked.
function arm(btn, label, act) {
  let armed = false, timer = 0;
  btn.textContent = label;
  const reset = () => { armed = false; btn.className = btn.className.replace(' armed', ''); btn.textContent = label; };
  btn.onclick = () => {
    if (armed) { clearTimeout(timer); reset(); act(); return; }
    armed = true;
    btn.className += ' armed';
    btn.textContent = tr('action.confirm');
    timer = setTimeout(reset, 5000);
  };
}

// emptyState is the two lines a screen shows when it has nothing: what is absent, and how it stops
// being absent. Both from the pack — these were the last four sentences on the page written in
// English, and they are the ones a first-time reader meets before anything else.
//
// The second line may carry markup (a command in <code>), which is why it is set as HTML. It comes
// from a pack this binary serves and embeds; nothing a companion or the network says reaches here.
function emptyState(whatKey, howKey) {
  const e = document.createElement('div');
  e.className = 'empty';
  e.innerHTML = tr(whatKey) + '<br>' + tr(howKey);
  return e;
}

// stripSource removes the provenance line the store appends to a body, so the rule reads as the
// rule. Where it came from is drawn as its own field rather than as a sentence at the end of the
// text — see the meta line.
const SOURCE_LINE = /\n*\(source: ([^)]*)\)\s*$/;
function stripSource(body) { return body.replace(SOURCE_LINE, '').trim(); }
function sourceOf(body) { const m = SOURCE_LINE.exec(body || ''); return m ? m[1] : ''; }

// jumpToFirstRow brings the top row of the filtered list into view.
//
// Filtering alone was not enough. On a phone the list starts below a screen of masthead and
// summary, and the report a decision rests on was measured at 989px down a 720px screen — so
// pressing "2 waiting" reordered something the reader could not see. Scrolling is the other half
// of the answer to "show me what needs me".
//
// Deferred a frame: the rows are replaced by the render() that precedes this, and measuring an
// element the layout has not placed yet gives a position it is about to leave.
function jumpToFirstRow() {
  requestAnimationFrame(() => {
    const first = fleetEl.querySelector('.card');
    if (first && first.scrollIntoView) first.scrollIntoView({block: 'start', behavior: 'smooth'});
  });
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
    const i = document.createElement('md-outlined-text-field');
    i.label = tr('label.answer');
    const b = document.createElement('md-filled-button'); b.textContent = tr('action.answer');
    const go = e => { e.preventDefault(); e.stopPropagation(); if (i.value.trim()) send(i.value.trim()); };
    b.onclick = go;
    i.onclick = e => { e.preventDefault(); e.stopPropagation(); };
    i.onkeydown = e => { if (e.key === 'Enter') go(e); };
    box.append(i, b);
  } else {
    for (const [label, decision] of [['allow', 'allow'], ['always', 'always'], ['deny', 'deny']]) {
      // Filled tonal, not text. M3 ranks buttons by emphasis — filled, filled tonal, outlined, text
      // — and these three were at the BOTTOM of it while being the highest-stakes control on the
      // page: the one that approves a "drop table" on a live database. On the same screen, answering
      // "which surface should the empty state sit on?" was drawn as a filled button, so the page
      // was shouting about a design question and whispering about a destructive command.
      //
      // All three at the SAME weight, deliberately. Raising only "allow" would make the page nudge
      // toward approving, and a console that leans on a permission decision is worse than one that
      // draws it quietly. The colour stays neutral for the same reason; the question above them
      // already carries the warning colour.
      const b = document.createElement('md-filled-tonal-button'); b.textContent = label;
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
// How many were waiting when the title was last written, so a repaint can rewrite it without
// re-counting a fleet it has not fetched.
let lastWaiting = 0;
function retitle(waiting) {
  lastWaiting = waiting;
  // Where you are goes in the title as well: with four sections and a page you leave open, the tab
  // strip in a browser is the outermost breadcrumb somebody actually reads.
  const s = sock();
  const where = s ? nameOf(s) : SECTION[view()] || tr('nav.companions');
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
  inner.append(k);
  const why = grounds(a);
  if (why) inner.append(why);
  inner.append(answerBox(a));
  box.replaceChildren(inner);
  box.hidden = false;
  measureDock();
}

// What this companion has done before now.
//
// Every other panel on this page is about the turn it is in. When that turn ends the page shows the
// next one, so "what has this one actually been doing" — the question somebody has after being away
// — had no answer here, while the answer sat in the log store the whole time.
//
// The request as it was made, not a summary of it. A summary would be this page deciding what the
// work was about, which quietly rewrites what somebody asked for.
async function loadHistory() {
  const list = await fetchList('/history' + q());
  if (!list || !list.length) { historyEl.hidden = true; historyEl.replaceChildren(); return; }
  const box = cell('');
  box.append(cell('k', tr('field.history')));
  for (const h of list) {
    const row = cell('hs' + (h.current ? ' now' : ''));
    row.append(cell('when', h.current ? tr('state.working') : ago(h.ago)));
    row.append(cell('what', h.title || tr('history.untitled')));
    box.append(row);
  }
  historyEl.replaceChildren(box);
  historyEl.hidden = false;
  measureDock();
}

// ── the board ────────────────────────────────────────────────────────────────
// Work as cards, a column per companion, and a day you can move.
//
// # Why the columns are companions and not states
//
// A kanban's columns are usually a state, and a state is a fact about NOW: waiting, working, idle.
// There is no such thing as the state a companion was in last Tuesday — the fleet derives state
// from what is open in a log, and nothing is open in a log from last week. Columns of state would
// be a board that could only ever show today, which is the one day the fleet page already covers.
//
// So the column is who did it and the card is the work, which reads the same on any day. The day
// moves; the shape does not.
//
// # One request per companion
//
// The history endpoint answers for one companion because that is the question every other panel
// asks it. Fanning out here rather than adding a fleet-wide endpoint keeps one way to ask, and the
// fan-out is the size of the fleet — a handful, once per visit.
const dayOf = ts => (ts || '').slice(0, 10);
const todayISO = () => new Date(Date.now() - new Date().getTimezoneOffset() * 60000)
  .toISOString().slice(0, 10);
let boardDay = '';

async function loadBoard() {
  const list = await fetchList('/fleet');
  if (!list) return;
  fleetSeen = list;
  if (!boardDay) boardDay = todayISO();

  const head = cell('boardhead');
  const day = document.createElement('md-outlined-text-field');
  day.setAttribute('type', 'date');
  day.setAttribute('label', tr('board.day'));
  day.value = boardDay;
  day.addEventListener('change', () => { boardDay = day.value || todayISO(); loadBoard(); });
  const today = document.createElement('md-text-button');
  today.textContent = tr('board.today');
  today.onclick = () => { boardDay = todayISO(); loadBoard(); };
  head.append(day, today);

  // Ordered the way the fleet is: trouble first. A board that sorted by name would bury the column
  // somebody needs behind the alphabet.
  const cols = [...list].sort((x, y) => (ORDER[x.state] - ORDER[y.state]) || (x.idle - y.idle));
  const runs = await Promise.all(cols.map(a =>
    fetchList('/history?d=' + encodeURIComponent(a.socket) + (a.peer ? '&p=' + encodeURIComponent(a.peer) : ''))
      .then(h => h || [])));

  const lanes = cell('lanes');
  let anything = false;
  cols.forEach((a, i) => {
    // A session counts for the day if it was running at any point in it, not only if it began
    // then: a task started at 23:40 and finished at 01:10 belongs to both days somebody might ask
    // about, and belonging to neither is how a long night disappears from a board.
    const work = runs[i].filter(h => dayOf(h.started) <= boardDay && dayOf(h.ended) >= boardDay);
    if (!work.length) return;
    anything = true;
    const lane = cell('lane');
    const title = cell('lanehead');
    title.append(cell('lname', a.name));
    title.append(cell('lcount', work.length + ''));
    lane.append(title);
    for (const h of work) {
      const card = cell('wcard' + (h.current ? ' now' : ''));
      card.append(cell('wwhen', h.current ? tr('board.now') : (h.started || '').slice(11, 16)));
      card.append(cell('wwhat', h.title || tr('history.untitled')));
      lane.append(card);
    }
    lanes.append(lane);
  });

  boardEl.replaceChildren(head,
    anything ? lanes : emptyState('board.nothing', 'board.nothing_how'));
}

// A list from this console, or null when the console itself cannot be reached.
//
// The three loaders had this same try/catch, and the distinction it draws is the one thing they
// must not get differently: "magi-web is not answering" is a fact about the page you are looking
// at, and it is not the same as a companion being quiet. Null, so a caller cannot mistake the
// failure for an empty list and draw "nothing here" over a screen that simply lost its server.
async function fetchList(path) {
  try { return await (await fetch(path)).json(); }
  catch { state.className = 'lost'; state.textContent = tr('error.unreachable'); return null; }
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
    // Once per visit, not on every poll: a list of finished work does not change while you read it,
    // and re-fetching it four times a minute would be four reads of the whole store for an answer
    // that is the same every time.
    if (!historyEl.children.length) loadHistory();
    loadIntervened(mine);
    return;
  }

  // A badge on the section that holds them, which is what M3 uses one for: a count of things
  // wanting attention, on the navigation item that leads to them. It rides the rail item's end slot
  // so it survives the rail collapsing to icons — the state where a count matters most, because the
  // words are gone and the shape is all there is.
  markWaiting(waiting);

  // The count says somebody is blocked; pressing it goes there. It said so and did nothing before,
  // which is the readout every console has and the reason nobody presses it.
  state.replaceChildren(document.createTextNode(
    list.length + (list.length === 1 ? ' agent' : ' agents') + (waiting ? ' · ' : '')));
  if (waiting) {
    const go = document.createElement('md-text-button');
    go.className = 'jump';
    go.textContent = tr('state.waiting_on_you', {n: waiting});
    go.onclick = () => { filter = 'waiting'; render(); jumpToFirstRow(); };
    state.append(go);
  }
  state.className = waiting ? 'lost' : '';
  summarise(list);

  if (!list.length) {
    fleetEl.replaceChildren();
    fleetEl.append(emptyState('empty.no_agents', 'empty.no_agents_how'));
    return;
  }
  // Trouble first, then movement, then quiet, then gone; most recently active within each. A list
  // you have to read to find the problem is a list that hides it.
  const rows = list
    .filter(a => !filter || GROUP[a.state] === filter)
    .sort((x, y) => (ORDER[x.state] - ORDER[y.state]) || (x.idle - y.idle));
  fleetEl.replaceChildren(tableHead(), ...grouped(rows));
}

// grouped lays the rows out by team, when there are teams.
//
// # Why this does not simply group
//
// The order above exists for a reason written next to it: trouble first, because a list you have to
// read to find the problem is a list that hides it. Grouping by team scatters the blocked agents
// across the page and takes that back. So the ROWS keep their order inside a group, and the GROUPS
// are ordered by the worst state in each — a team with somebody blocked comes first. The rule the
// flat list followed still holds; it now holds twice.
//
// # Why it is conditional
//
// Nothing declares a team on a single-workspace machine, and a header saying so would be a line of
// furniture over every list. Grouping appears when the data has teams in it and not before, which
// is the same test the page applies to everything else it draws.
function grouped(rows) {
  const teams = new Map();
  for (const a of rows) {
    const key = a.team || '';
    if (!teams.has(key)) teams.set(key, []);
    teams.get(key).push(a);
  }
  // One group, and it is the unnamed one: this machine has no teams and there is nothing to say.
  if (teams.size <= 1 && teams.has('')) return rows.map(card);

  const order = [...teams.entries()].sort((x, y) => {
    // The unnamed group last however its members are doing: "these belong to no team" is a remark
    // about the roster, and the roster is not the thing somebody is scanning for.
    if ((x[0] === '') !== (y[0] === '')) return x[0] === '' ? 1 : -1;
    // The most urgent member decides where the group sits. ORDER counts up from waiting, so the
    // smallest number in a group is the loudest thing in it.
    const loudest = g => Math.min(...g.map(a => ORDER[a.state]));
    return (loudest(x[1]) - loudest(y[1])) || x[0].localeCompare(y[0]);
  });
  const out = [];
  for (const [name, members] of order) {
    out.push(teamHead(name, members), ...members.map(card));
  }
  return out;
}

// teamHead names a team and says who answers for it.
//
// The hub is on the header rather than on its own row: which companion speaks for a team is a fact
// about the team, and a badge buried in one row is a fact somebody has to go looking for.
function teamHead(name, members) {
  const h = cell('teamhead');
  h.append(cell('tname', name || tr('team.none')));
  // Every companion claiming to speak for the team, not the first one found. Two is a
  // misconfiguration — a team answers with one voice or the question of who answers is open — and
  // naming one of them would draw a settled team over an unsettled one.
  const hubs = members.filter(a => a.hub).map(a => a.name);
  if (hubs.length) h.append(cell('thub', tr('team.spoken_for', {name: hubs.join(', ')})));
  const waiting = members.filter(a => a.state === 'waiting').length;
  if (waiting) {
    const b = document.createElement('md-badge');
    b.value = String(waiting);
    h.append(b);
  }
  h.append(cell('tn', members.length + ''));
  return h;
}

// drawDetail is the agent page's own header: what this is, where it runs, how far it has got.
// A detail view that does not say which resource it is showing is the one place a console cannot
// afford to be quiet, and a transcript does not say it.
function drawDetail(a) {
  const box = document.getElementById('detail');
  if (!a) { box.hidden = true; box.replaceChildren(); return; }
  // Takes a KEY, not a word. Every label in this panel was written in English here while the pack
  // carried a translation for it, and the panel is the one screen that answers "what am I looking
  // at" — the last place that should be answering it in a language the reader did not pick.
  const field = (key, v, cls) => {
    const f = cell('f'); f.append(cell('k', tr(key)), cell('v ' + (cls || ''), v)); return f;
  };
  box.replaceChildren(
    field('field.status', a.state, 'state ' + a.state),
    field('field.workspace', a.workdir),
    ...(a.role ? [field('field.role', a.role)] : []),
    ...(a.team ? [field('field.team', a.team + (a.hub ? ' · ' + tr('team.speaks') : ''))] : []),
    field('field.host', (a.host || 'this machine') + (a.addr ? ' · ' + a.addr : '') +
                  (a.pid ? ' · pid ' + a.pid : '')),
    field('field.steps', a.steps ? a.steps + '' : '—'),
    field('field.last_activity', ago(a.idle)),
    field('field.session', a.session),
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
  box.replaceChildren(cell('k', tr('field.handed_out')), ...rows);
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
  if (c.model) box.append(field('field.model', c.model));
  // Said once, where somebody would otherwise wonder why there is no cache figure at all.
  if (!c.cacheReported && !c.estimated) {
    box.append(field('field.cache', tr('context.no_cache_report')));
  }

  const size = cell('v', '');
  size.append(document.createTextNode(
    (c.estimated ? '~' : '') + (c.used || 0).toLocaleString() +
    (c.window ? ' / ' + c.window.toLocaleString() : '') + ' tokens'));
  const note = document.createElement('small');
  // Said plainly, because the difference decides what the number is worth: one is the provider's
  // own count from the last turn, the other is arithmetic over the transcript.
  note.textContent = ' ' + tr(c.estimated ? 'context.estimated' : 'context.measured') +
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
  f.append(cell('k', tr('field.context')), size);
  // The lever beside the reading. magi folds by itself when the window fills past its ratio; this
  // is for the case that rule does not cover — somebody who can see the run is about to need room
  // and would rather it happened now, between turns, than in the middle of the next one.
  const fold = document.createElement('md-text-button');
  fold.className = 'fold'; fold.textContent = tr('action.compact_now');
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
    cf.append(cell('k', tr('field.summarised_away')), v);
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

// ── what I had to step in and say ─────────────────────────────────────────────
// On the companion it is about, and no longer a factory for rules.
//
// This began as a promotion pipeline: group what a person said mid-turn by the words, count the
// repeats, offer to promote the repeated ones into the experience store. The premise does not hold.
// What somebody says mid-turn is nearly always about THAT task — "no, the other file" is not a
// rule — the few that generalise are rare, and the grouping only ever matched identical wording,
// which people do not produce. Above all it needed somebody to visit a screen and curate, and the
// agent's own remember tool already reaches the store without that.
//
// What survives is the part that was always true and never needed the words to match: a count of
// how often this companion had to be corrected, and what was refused. That is a fact about the
// companion, so it belongs on the companion's page.
async function loadIntervened(a) {
  if (!a) { intervenedEl.hidden = true; intervenedEl.replaceChildren(); return; }
  const list = await fetchList('/interventions');
  if (!list) return;
  const mine = list.filter(m => m.socket === a.socket && (m.peer || '') === (a.peer || ''));
  if (!mine.length) { intervenedEl.hidden = true; intervenedEl.replaceChildren(); return; }

  const box = cell('');
  const steers = mine.filter(m => m.kind !== 'denied').length;
  const refused = mine.length - steers;
  const head = cell('k');
  head.textContent = tr('field.intervened') + ' · ' +
    (steers ? tr('iv.steers', {n: steers}) : '') +
    (steers && refused ? ' · ' : '') +
    (refused ? tr('iv.refused', {n: refused}) : '');
  box.append(head);
  for (const m of mine.slice(0, 12)) {
    const row = cell('iv2' + (m.kind === 'denied' ? ' denied' : ''));
    row.append(cell('when', (m.at || '').slice(0, 10)));
    row.append(cell('said', m.kind === 'denied' ? tr('iv.refused_call', {what: m.text}) : m.text));
    box.append(row);
  }
  intervenedEl.replaceChildren(box);
  intervenedEl.hidden = false;
  measureDock();
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
    skillsEl.replaceChildren(emptyState('empty.nothing_learned', 'empty.nothing_learned_how'));
    return;
  }
  skillsEl.replaceChildren(...list.map(sk => {
    const el = cell('sk ' + sk.tier + (sk.kind === 'memory' ? ' fact' : ''));
    const top = cell('top');
    top.append(cell('tier',
      (sk.tier === 'global' ? tr('reach.every_companion') : tr('reach.only', {name: sk.companion})) +
      (sk.peer ? tr('reach.on_peer', {peer: sk.peer}) : '')));
    top.append(cell('what', sk.description || sk.name));
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    drop.title = 'remove this rule from the store';
    arm(drop, tr('action.forget'), () => {
      // A rule on another console is forgotten THERE. The socket is that machine's path and the
      // peer name is how this one knows which machine to ask; a global rule has no socket and the
      // peer name alone routes it.
      post('/forget', new URLSearchParams({name: sk.name, tier: sk.tier}),
           sk.tier === 'project' ? sk.socket : null, sk.peer).then(loadSkills);
    });
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
    // Where it came from, which the store keeps as a line at the end of the body.
    const src = sourceOf(sk.body);
    if (src) bits.push(tr('skill.learned_from', {src}));
    el.append(cell('meta', bits.join(' · ')));

    // The rule itself. The list showed a one-line description and governed something nobody could
    // read — a screen for deciding whether a rule should exist, with the rule not on it. The body
    // is already in the answer; it was only never drawn.
    //
    // Behind a toggle rather than always open: these are paragraphs, and twenty of them expanded
    // is a page you scroll past rather than one you audit.
    const body = (sk.body || '').trim();
    if (!body) return el;
    const text = cell('body');
    text.textContent = stripSource(body);
    text.hidden = true;
    const more = document.createElement('md-text-button');
    more.className = 'fold';
    let open = false;
    more.textContent = tr('action.read');
    more.onclick = () => {
      open = !open;
      text.hidden = !open;
      more.textContent = tr(open ? 'action.collapse' : 'action.read');
    };
    top.insertBefore(more, drop);
    el.append(text);
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
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    drop.title = 'delete this definition from ' + sv.file;
    arm(drop, tr('action.remove'), () => {
      const body = new URLSearchParams({name: sv.name, delete: '1'});
      if (!sv.socket) body.set('tier', 'global');
      post('/mcp', body, sv.socket || null).then(loadMCP);
    });
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
  // A short label that can live in the outline's notch, and the explanation underneath it. Both
  // through tr(): these five were hardcoded English while the pack carried translations for them
  // that nothing read.
  //
  // The keys are written out rather than assembled from the field's name. The check that every
  // label exists in both packs reads them out of this file, and a key built at runtime is one it
  // cannot see — which is how a label ends up rendering as its own dotted name on somebody's
  // screen. Same reason the preferences list their keys.
  const MCP_FIELDS = [
    ['name', 'label.mcp_name', 'hint.mcp_name'],
    ['command', 'label.mcp_command', 'hint.mcp_command'],
    ['args', 'label.mcp_args', 'hint.mcp_args'],
    ['url', 'label.mcp_url', 'hint.mcp_url'],
    ['env', 'label.mcp_env', 'hint.mcp_env'],
  ];
  const mcpField = ([name, labelKey, hintKey]) => {
    const i = document.createElement('md-outlined-text-field');
    i.name = name;
    i.setAttribute('label', tr(labelKey));
    i.setAttribute('supporting-text', tr(hintKey));
    return i;
  };
  const who = document.createElement('md-outlined-select');
  who.name = 'who';
  who.setAttribute('label', tr('label.reach'));
  const opts = [['', tr('reach.every_companion')]].concat(
    (fleetSeen || []).filter(a => !a.peer).map(a => [a.socket, tr('reach.only', {name: a.name})]));
  for (const [v, label] of opts) {
    const o = document.createElement('md-select-option');
    o.value = v; o.append(cell('', label));
    who.append(o);
  }
  form.append(who, ...MCP_FIELDS.map(mcpField));
  const go = document.createElement('md-filled-button');
  go.type = 'submit'; go.textContent = tr('action.add_or_replace');
  form.append(go);
  const note = cell('note',
    'Written to that companion\'s config file. It attaches when that daemon next starts — ' +
    'this changes the file, not a running process.');
  form.append(note);
  form.onsubmit = async e => {
    e.preventDefault();
    const body = new URLSearchParams();
    for (const el of [...form.find('md-outlined-text-field')]) {
      if (el.value.trim()) body.set(el.name, el.value.trim());
    }
    if (!who.value) body.set('tier', 'global');
    await post('/mcp', body, who.value || null);
    loadMCP();
  };

  if (!list.length) {
    mcpEl.replaceChildren(emptyState('empty.no_servers', 'empty.no_servers_how'), form);
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
  es.onopen = () => { state.className = 'live'; state.textContent = tr('state.live'); };
  es.onmessage = e => draw(JSON.parse(e.data));
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { state.className = 'lost'; state.textContent = tr('state.reconnecting');
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
  tabFleet.querySelector('.lbl').textContent = tr('nav.companions');
  tabSkills.textContent = tr('nav.lessons');
  tabBoard.textContent = tr('nav.board');
  tabMcp.textContent = tr('nav.connections');
  // label, not placeholder. Material Web floats the LABEL into the outline's notch when the field
  // takes focus or holds a value; a placeholder is the grey hint inside an empty one and never
  // moves. Written as placeholders here, the fields had no notch and nothing to float — which is
  // what "the placeholder looks wrong" was. The longer sentence becomes supporting text, which is
  // where an explanation belongs and where it does not have to fit in a notch.
  t.setAttribute('label', tr('label.ask'));
  toEl.setAttribute('label', tr('label.address'));
  toEl.setAttribute('supporting-text', tr('hint.address'));
  document.getElementById('send').textContent = tr('action.send');
  document.getElementById('stop').textContent = tr('action.interrupt');
  railMenu.setAttribute('aria-label', tr('nav.menu'));
  themeToggle.setAttribute('aria-label', tr('pref.theme'));
  prefsEl.setAttribute('aria-label', tr('nav.preferences'));
  prefsClose.textContent = tr('action.close');
  prefsK.textContent = tr('nav.preferences');
  consoleK.textContent = tr('nav.this_console');
  for (const [el, key] of [[railFleet, 'nav.companions'], [railSkills, 'nav.lessons'],
                           [railBoard, 'nav.board'], [railMcp, 'nav.connections']]) {
    // The word is on the item whether or not it is drawn: collapsed, the icon is all there is to
    // see, and a rail nobody can read aloud is not a navigation. The icon itself is markup and is
    // not touched here — a shape does not need translating, and rebuilding it on every language
    // change would throw away four elements to replace them with the same four.
    el.setAttribute('aria-label', tr(key));
    el.querySelector('.lbl').textContent = tr(key);
  }
  paintChoice(langEl, 'lang');
  if (consoleEl.children.length) loadConsole();   // its two labels are words too

  // The lists are drawn by functions, and a function's words are read at draw time. A pack that
  // lands after the list did leaves it in the old language until somebody navigates — measured on
  // the lessons page, which showed an English "forget" beside a Korean "정말?" on the same control.
  //
  // Only the LISTS. The transcript and the detail panel are not repainted, because a pack can land
  // mid-interaction and re-rendering there is what once wiped a panel somebody was reading. A list
  // replaces its own container and nothing else, so redrawing it costs the reader nothing.
  //
  // Guarded on a first paint having happened, or this would run before the loaders are declared.
  if (!repaintable) return;
  if (view() === 'skills') loadSkills();

  else if (view() === 'mcp') loadMCP();
  else if (view() === 'board') loadBoard();
  else if (!sock()) loadFleet();
  // The crumb and the tab title are written by render() and are words too. The title is the one a
  // reader sees without looking at the page at all, which makes it the last place worth leaving in
  // a language they did not pick.
  back.textContent = sock() ? SECTION.fleet : (SECTION[view()] || tr('nav.companions'));
  retitle(lastWaiting);
}
// True once the page has drawn itself at least once. paint() runs before that on the first pass,
// when the loaders exist but the view has not been decided.
let repaintable = false;

// A select whose options are the same list every time and whose words are not. Rebuilt on a pack
// change rather than translated in place: an md-select-option holds its label as slotted content,
// so there is nothing to reach in and rewrite.
function paintChoice(el, kind) {
  const c = CHOICES[kind];
  el.setAttribute('label', tr(c.label));
  el.replaceChildren(...c.options.map(([v, key]) => {
    const o = document.createElement('md-select-option');
    o.value = v;
    o.append(cell('', tr(key)));
    return o;
  }));
  // Told through the SELECT, and told again once it has rendered.
  //
  // Two things had to be got right here. Marking the option selected on the way in left the field
  // showing the raw value — "dark" where the option said "다크" — because a select reads its
  // display text from the option it CHOSE, and it had chosen nothing: it had been handed children
  // that already claimed to be chosen. That is the same shape as the tabs, where writing the state
  // onto the child instead of asking the parent cost the animation.
  //
  // And the options are custom elements, which are not upgraded in the tick they are appended. A
  // select told its value in that tick has nothing to resolve it against and renders an empty field
  // over a value it is holding — measured: value "dark", displayText "". So it is told once now,
  // for the case where the elements are already upgraded, and again after the render that upgrades
  // them.
  const want = prefOf(kind);
  el.value = want;
  if (el.updateComplete) el.updateComplete.then(() => { el.value = want; });
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
  const section = s ? tr('nav.companions') : SECTION[v] || tr('nav.companions');
  retitle(0);
  back.textContent = section;
  back.setAttribute('href', at(s ? '' : HREF[v] || ''));
  crumbSep.hidden = !s;
  crumbHere.textContent = s ? nameOf(s) : '';
  back.className = s ? '' : 'here';
  tabsEl.hidden = !!s;
  // Which kind of page this is, for the rules that differ between them. On a companion's page the
  // tabs are gone, so anything that leans on them being there has to know.
  document.body.setAttribute('at', s ? 'agent' : 'list');
  document.body.setAttribute('view', s ? '' : v);
  // Which tab is current is asked of md-tabs, not written onto the tabs. Both leave the right tab
  // selected, but only one of them moves: the indicator is animated inside Tabs.activateTab and
  // nowhere else — it measures the outgoing tab's indicator and slides the incoming one from there
  // — and activateTab runs only when the component is the one changing the selection. Setting
  // each tab's active property, which this page did, arrived at the same picture with no motion
  // between the two. (No backticks in this file — it is one Go raw string, and one ends it.)
  tabsEl.activeTabIndex = Math.max(0, TABS.indexOf(v));
  // The rail says the same thing the tabs do. A list item has no selected state of its own, so
  // this is an attribute of ours and the stylesheet draws it — said once here rather than at each
  // of the four click handlers, which is how the two used to fall out of step.
  for (const [el, key] of RAILS) el.toggleAttribute('selected', !s && v === key);
  fleetEl.hidden = !!s || v !== 'fleet';
  summaryEl.hidden = !!s || v !== 'fleet';
  skillsEl.hidden = !!s || v !== 'skills';
  boardEl.hidden = !!s || v !== 'board';
  mcpEl.hidden = !!s || v !== 'mcp';
  log.hidden = !s;
  // The composer is on both views now. On a companion's page it steers that companion; on the
  // fleet it dispatches, and the address field is the difference.
  // Only on a companion's own page. Addressing one by typing its name into a box, from a list where
  // it is already on screen and one click away, is a second way to do the thing the list does — and
  // the harder one: it asks somebody to spell a name they can see.
  f.hidden = !s;
  toEl.hidden = true;
  document.getElementById('stop').hidden = !s; // nothing to interrupt from the fleet view
  document.getElementById('detail').hidden = !s;
  document.getElementById('handoffs').hidden = true;
  historyEl.hidden = true;
  intervenedEl.hidden = true;
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
  if (v === 'board') {
    loadBoard();
    return;
  }
  if (v === 'skills') {
    // Both halves of the same story, in the order it happens: what has been said often enough to
    // become a rule, then the rules. Not polled — this is read and thought about, and a list that
    // reorders itself under the cursor while somebody decides what to promote is worse than one a
    // minute old.
    loadSkills();
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
// The rail's four, addressed the same way the tabs are. They are md-list-item with an href, so the
// component draws a real anchor: the click is intercepted like every other in-page link, and a
// middle click or a copied address still lands.
const RAILS = [[railFleet, 'fleet'], [railSkills, 'skills'],
               [railBoard, 'board'], [railMcp, 'mcp']];
for (const [el, key] of RAILS) {
  el.href = at(HREF[key]);
  el.onclick = e => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button) return;  // let the browser have it
    e.preventDefault();
    history.pushState({}, '', at(HREF[key]));
    render();
  };
}

// Widening the rail is a wide-screen idea only. On a phone the rail is not a drawer — it is a
// section at the foot of the page — so there is nothing to open and nothing to close.
const closeNav = () => {
  document.body.removeAttribute('nav');
  railMenu.setAttribute('aria-expanded', 'false');
};
railMenu.onclick = () => {
  if (document.body.getAttribute('nav') === 'open') { closeNav(); return; }
  document.body.setAttribute('nav', 'open');
  railMenu.setAttribute('aria-expanded', 'true');
};
// One door to the preferences, at every width. The rail's hamburger is a different thing: it
// widens the navigation, and it no longer opens anything.
prefsEl.onclick = () => prefsDialog.show();
// Painted when it OPENS, not before. A dialog does not render what is slotted into it until then,
// so a select told its value while the dialog was closed had no options to resolve it against and
// showed an empty field over a value it was holding.
prefsDialog.addEventListener('opened', () => { if (painted) paint(); });
// The toggle writes the SAME preference the select does, so the two are one setting with two
// controls rather than two settings. Pressing it leaves 'system' behind on purpose: asking for the
// other theme is a choice, and pretending it was still deferring to the machine would mean the
// next OS change silently undid it.
themeToggle.onclick = () => {
  localStorage.setItem('theme', showing() === 'dark' ? 'light' : 'dark');
  applyTheme();
};
// Escape narrows it again, the way Escape leaves anything that has been opened.
addEventListener('keydown', e => { if (e.key === 'Escape') closeNav(); });

// A preference is written down and acted on immediately. Language re-fetches rather than reloads:
// the pack is the only thing that changes, and a reload would throw away the transcript.
langEl.addEventListener('change', () => {
  localStorage.setItem('lang', langEl.value);
  loadPack();
});

for (const [el, key] of [[tabFleet, 'fleet'], [tabSkills, 'skills'],
                         [tabBoard, 'board'], [tabMcp, 'mcp']]) {
  // The href is set as well as the click: a middle-click or a copied link has to reach the same
  // place, and on a project site an absolute one does not.
  el.setAttribute('href', at(HREF[key]));
  // Nothing is prevented here. A tab is a custom element and not a link, so there is no navigation
  // to stop — and md-tabs skips its indicator animation on any click whose default was prevented,
  // which is the second way this page had of standing still.
  el.onclick = () => { history.pushState({}, '', at(HREF[key])); render(); };
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
// The field grows itself: it is a component with its own textarea in a shadow root, so measuring
// scrollHeight from out here reads the host and not the text. All that is left to do is re-measure
// the dock, because the transcript reserves whatever the dock is actually occupying.
const grow = () => measureDock();

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
  if (!to) { state.className = 'lost'; state.textContent = tr('error.say_who'); toEl.focus(); return; }
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
repaintable = true;
loadConsole();
</script>
`
