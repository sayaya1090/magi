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
    --magi-ref-primary:#FF7A1A; --magi-ref-accent:#5CD8E6; --magi-ref-muted:#C9C2B8; --magi-ref-outline:#72675C;
    /* Secondary takes primary's hue at a third of its chroma, which is the rule the guide
       states for it: "secondary, neutral variant, and neutral colors match primary in hue but
       are progressively less chromatic". It pointed at --magi-ref-accent, and so did tertiary, so the
       two roles were one colour and the scheme had no secondary at all. The cyan is right
       WHERE IT IS — tertiary is the complement, arrived at by changing the hue — and it is
       still the councillor Balthasar's. Only the role that was borrowing it changes. */
    --magi-ref-secondary:#E8B89F;
    --magi-ref-error:#F2B8B5; --magi-ref-success:#86EFAC; --magi-ref-surface:#211B14;
    --magi-ref-primaryContainer:#4A2E0B; --magi-ref-outlineVariant:#463E34; --magi-ref-warn:#FFD479;
    /* The three council members' colours. Declared and unused HERE: the palette is the terminal's
       and a test requires this page to carry every role of it, so that retuning one surface can
       never leave the two disagreeing. The console shows no council, so nothing paints with them —
       which is a contract kept, not a leftover. */
    --magi-ref-melchior:#FFB454; --magi-ref-balthasar:#5CD8E6; --magi-ref-casper:#FF8A8A;
    --magi-ref-bg:#14110d; --magi-ref-fg:#E8E2D8;
    --magi-ref-shadow:#000000; --magi-ref-scrim:#000000;

    /* ── M3, dark ─────────────────────────────────────────────────────────── */
    /* The roles above are the terminal's, verbatim (a test pins them). These are the Material 3
       roles the terminal has no use for: a TUI paints on a background it does not own, so it
       cannot have tonal surfaces, and it never needed an on- pair because it draws text on one
       background. A browser has both, and without them "Material 3" would be a set of borrowed
       names — which is exactly what this page was until it was measured. */
    --magi-ref-on-primary:#2A1500;              /* on #FF7A1A */
    --magi-ref-on-primary-container:#FFD9B8;    /* on #4A2E0B */
    --magi-ref-on-error:#3A0A08;
    --magi-ref-on-surface:#E8E2D8;
    --magi-ref-on-surface-variant:#C9C2B8;
    /* Tonal layers, low → high. Dark themes get LIGHTER as they rise. */
    --magi-ref-surface-dim:#14110d;
    --magi-ref-surface-container-lowest:#0F0D0A;
    --magi-ref-surface-container-low:#1B1712;
    --magi-ref-surface-container:#211B14;
    --magi-ref-surface-container-high:#2B251C;
    --magi-ref-surface-container-highest:#352E24;

    /* ── the same roles under the names Material Web reads ────────────────── */
    /* The components are themed by these and nothing else. Setting a few of them per component —
       which is what this page did first — leaves every role it did not mention drawn in the
       library's baseline purple, which is what "the colours are the default ones" looks like.
       Declared once, at the root, so a component added later is magi-coloured by existing. */
    --md-sys-color-primary:var(--magi-ref-primary);
    --md-sys-color-on-primary:var(--magi-ref-on-primary);
    --md-sys-color-primary-container:var(--magi-ref-primaryContainer);
    --md-sys-color-on-primary-container:var(--magi-ref-on-primary-container);
    --md-sys-color-secondary:var(--magi-ref-secondary);
    --md-sys-color-on-secondary:var(--magi-ref-on-primary);
    --md-sys-color-secondary-container:var(--magi-ref-surface-container-high);
    --md-sys-color-on-secondary-container:var(--magi-ref-on-surface);
    --md-sys-color-tertiary:var(--magi-ref-accent);
    --md-sys-color-on-tertiary:var(--magi-ref-on-primary);
    --md-sys-color-error:var(--magi-ref-error);
    --md-sys-color-on-error:var(--magi-ref-on-error);
    --md-sys-color-error-container:var(--magi-ref-surface-container-high);
    --md-sys-color-on-error-container:var(--magi-ref-error);
    --md-sys-color-background:var(--magi-ref-bg);
    --md-sys-color-on-background:var(--magi-ref-fg);
    --md-sys-color-surface:var(--magi-ref-bg);
    --md-sys-color-on-surface:var(--magi-ref-on-surface);
    --md-sys-color-surface-variant:var(--magi-ref-surface);
    --md-sys-color-on-surface-variant:var(--magi-ref-on-surface-variant);
    --md-sys-color-surface-container-lowest:var(--magi-ref-surface-container-lowest);
    --md-sys-color-surface-container-low:var(--magi-ref-surface-container-low);
    --md-sys-color-surface-container:var(--magi-ref-surface-container);
    --md-sys-color-surface-container-high:var(--magi-ref-surface-container-high);
    --md-sys-color-surface-container-highest:var(--magi-ref-surface-container-highest);
    --md-sys-color-outline:var(--magi-ref-outline);
    --md-sys-color-outline-variant:var(--magi-ref-outlineVariant);
    --md-sys-color-inverse-surface:var(--magi-ref-fg);
    --md-sys-color-inverse-on-surface:var(--magi-ref-bg);
    /* Through the palette layer like every other colour role, rather than as two hex values
       sitting among twenty var()s. The guide's rule for a system token is that it point at a
       reference rather than hold a value, and these two were the only colours here not doing it —
       which also meant styles.go could retune them and this page would not follow. */
    --md-sys-color-shadow:var(--magi-ref-shadow);
    --md-sys-color-scrim:var(--magi-ref-scrim);

    /* ── and the type, under the names Material Web reads ─────────────────── */
    /* A component takes its font from --md-sys-typescale-<role>-font, not from the ref typeface
       alone, so setting only the latter leaves every label in the library's fallback. Declared
       across the roles at the root, the way the handbook project does it: one place, and a
       component added later is already in magi's face.
       Sizes are the M3 scale (see the type tokens above); the faces are ours, which M3 allows —
       the scale is what it asks you to keep. */
    --md-ref-typeface-plain:var(--magi-ref-mono);
    --md-ref-typeface-brand:var(--magi-ref-display);
    --md-sys-typescale-label-small-font:var(--magi-ref-mono);
    --md-sys-typescale-label-small-size:0.6875rem;
    --md-sys-typescale-label-small-line-height:1rem;
    --md-sys-typescale-label-medium-font:var(--magi-ref-mono);
    --md-sys-typescale-label-medium-size:0.75rem;
    --md-sys-typescale-label-medium-line-height:1rem;
    --md-sys-typescale-label-large-font:var(--magi-ref-mono);
    --md-sys-typescale-label-large-size:0.875rem;
    --md-sys-typescale-label-large-line-height:1.25rem;
    --md-sys-typescale-body-small-font:var(--magi-ref-mono);
    --md-sys-typescale-body-small-size:0.75rem;
    --md-sys-typescale-body-small-line-height:1rem;
    --md-sys-typescale-body-medium-font:var(--magi-ref-mono);
    --md-sys-typescale-body-medium-size:0.875rem;
    --md-sys-typescale-body-medium-line-height:1.25rem;
    --md-sys-typescale-body-large-font:var(--magi-ref-mono);
    --md-sys-typescale-body-large-size:1rem;
    --md-sys-typescale-body-large-line-height:1.5rem;
    --md-sys-typescale-title-small-font:var(--magi-ref-mono);
    --md-sys-typescale-title-small-size:0.875rem;
    --md-sys-typescale-title-small-line-height:1.25rem;
    --md-sys-typescale-title-medium-font:var(--magi-ref-mono);
    --md-sys-typescale-title-medium-size:1rem;
    --md-sys-typescale-title-medium-line-height:1.5rem;
    --md-sys-typescale-title-large-font:var(--magi-ref-display);
    --md-sys-typescale-title-large-size:1.375rem;
    --md-sys-typescale-title-large-line-height:1.75rem;
    --md-sys-typescale-headline-small-font:var(--magi-ref-display);
    --md-sys-typescale-headline-small-size:1.5rem;
    --md-sys-typescale-headline-small-line-height:2rem;
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
      --magi-ref-primary:#B45309; --magi-ref-accent:#0E7490; --magi-ref-muted:#4A453C; --magi-ref-outline:#8A7E6E;
      --magi-ref-secondary:#82604F;
      --magi-ref-error:#B3261E; --magi-ref-success:#15803D; --magi-ref-surface:#F5EEE3;
      --magi-ref-primaryContainer:#F8D9A8; --magi-ref-outlineVariant:#D8CFC0; --magi-ref-warn:#92600A;
      --magi-ref-melchior:#B45309; --magi-ref-balthasar:#0E7490; --magi-ref-casper:#B3261E;
      --magi-ref-bg:#FBF8F3; --magi-ref-fg:#221D16;

      /* ── M3, light ─────────────────────────────────────────────────────── */
      /* The layers invert: a light theme gets DARKER as it rises. Built as its own ramp rather
         than by dimming the dark one — a light theme has less headroom, and this page has been
         caught before with eight of thirteen dimmed pairs under AA, the worst at 2.47:1. */
      --magi-ref-on-primary:#FFFFFF;
      --magi-ref-on-primary-container:#3A1B00;
      --magi-ref-on-error:#FFFFFF;
      --magi-ref-on-surface:#221D16;
      --magi-ref-on-surface-variant:#4A453C;
      --magi-ref-surface-dim:#EFE9DF;
      --magi-ref-surface-container-lowest:#FFFFFF;
      --magi-ref-surface-container-low:#F7F3EC;
      --magi-ref-surface-container:#F2ECE2;
      --magi-ref-surface-container-high:#ECE5D9;
      --magi-ref-surface-container-highest:#E6DED1;
    }
  }
  :root[color-theme="light"] {
    --magi-ref-primary:#B45309; --magi-ref-accent:#0E7490; --magi-ref-muted:#4A453C; --magi-ref-outline:#8A7E6E;
    --magi-ref-secondary:#82604F;
    --magi-ref-error:#B3261E; --magi-ref-success:#15803D; --magi-ref-surface:#F5EEE3;
    --magi-ref-primaryContainer:#F8D9A8; --magi-ref-outlineVariant:#D8CFC0; --magi-ref-warn:#92600A;
    --magi-ref-melchior:#B45309; --magi-ref-balthasar:#0E7490; --magi-ref-casper:#B3261E;
    --magi-ref-bg:#FBF8F3; --magi-ref-fg:#221D16;

    /* ── M3, light ─────────────────────────────────────────────────────── */
    /* The layers invert: a light theme gets DARKER as it rises. Built as its own ramp rather
       than by dimming the dark one — a light theme has less headroom, and this page has been
       caught before with eight of thirteen dimmed pairs under AA, the worst at 2.47:1. */
    --magi-ref-on-primary:#FFFFFF;
    --magi-ref-on-primary-container:#3A1B00;
    --magi-ref-on-error:#FFFFFF;
    --magi-ref-on-surface:#221D16;
    --magi-ref-on-surface-variant:#4A453C;
    --magi-ref-surface-dim:#EFE9DF;
    --magi-ref-surface-container-lowest:#FFFFFF;
    --magi-ref-surface-container-low:#F7F3EC;
    --magi-ref-surface-container:#F2ECE2;
    --magi-ref-surface-container-high:#ECE5D9;
    --magi-ref-surface-container-highest:#E6DED1;
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
    --magi-ref-display: "Newsreader", "Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif;
    --magi-ref-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    /* ── state layers ───────────────────────────────────────────────────────── */
  /* M3 does not recolour text on hover; it lays the on- colour over the surface at a fixed
     opacity — 8% hover, 12% focus and press. One recipe, applied by adding the state class to
     anything that responds, so a new control gets the behaviour by being told what it is rather
     than by somebody remembering four rules. */

  /* ── the M3 shape scale, and nothing off it ───────────────────────────── */
    /* 4 · 8 · 12 · 16 · 28 · full. Every radius on this page is one of these; the page used to be
       2px everywhere, which is not a value the scale has. The scale has no 24 — it steps 20 then 28,
       and this page said 24 until it was measured against the token table. */
    --magi-sys-shape-xs:4px; --magi-sys-shape-s:8px; --magi-sys-shape-m:12px; --magi-sys-shape-l:16px; --magi-sys-shape-xl:28px;
    --magi-sys-shape-full:9999px;

    /* ── M3 motion ────────────────────────────────────────────────────────── */
    /* Verified against material-components-android's Motion.md. The .12s ease this page used was a
       number somebody typed; these are the system's, and using them is what makes two surfaces
       feel like one. */
    --magi-sys-ease-standard:cubic-bezier(0.2, 0, 0, 1);
    --magi-sys-ease-decelerate:cubic-bezier(0.05, 0.7, 0.1, 1);
    /* Read out of the shipped bundle rather than a document, and the two disagree. The guide says
       emphasized has NO css form ("N/A — use standard as a fallback"), because the real curve is a
       two-segment path a single cubic-bezier cannot draw. Material Web resolved that its own way:
       EMPHASIZED:"cubic-bezier(.3,0,0,1)" is a literal constant in the bundle. Following the
       bundle over the guide is deliberate — the components on this page move on the library's
       curve whatever we declare, and a container opening beside them has to match what they
       actually do, not what the document wishes they did. */
    --magi-sys-ease-emphasized:cubic-bezier(0.3, 0, 0, 1);

    /* How much room the rail takes. Declared HERE and not only on #rail, because the page's own
       left offset is computed from it — and a var() that resolves to nothing does not fall back to
       the shorthand underneath it. The declaration becomes invalid at computed-value time and the
       property takes its initial value, which for padding is 0: the offset silently vanished and
       the rail stood on top of the page at every width. */
    /* 80px, which is the narrow collapsed rail in the spec. It was 4.5rem — 72px — which is under
       both numbers the spec gives (96dp standard, 80dp narrow), and under the 88dp a vertical item
       needs for a 56dp indicator between two 16dp insets. */
    --magi-comp-rail-w:5rem;
    --magi-sys-dur-short2:100ms; --magi-sys-dur-short4:200ms; --magi-sys-dur-medium2:300ms;

    /* ── the M3 type scale ────────────────────────────────────────────────── */
    /* size/line-height pairs, taken as pairs: matching a size and inventing a line height is how
       the rhythm goes. The page used to carry 9.5 · 10.5 · 11.5 · 12.5 · 13.5 · 15.5 · 17px, none
       of which is on the scale. The typeface is ours — M3 allows that; the scale is not.

       These are shorthands for the font property, not a second scale. They read the same
       --md-sys-typescale-* tokens the components read, so the scale is stated once and a change
       to it reaches the hand-written CSS and the library together. They were px, which is the
       one thing a size may not be: px does not answer the reader who sets a larger default. */
    --magi-sys-headline-s:var(--md-sys-typescale-headline-small-size)/var(--md-sys-typescale-headline-small-line-height);
    --magi-sys-title-l:var(--md-sys-typescale-title-large-size)/var(--md-sys-typescale-title-large-line-height);
    --magi-sys-title-m:var(--md-sys-typescale-title-medium-size)/var(--md-sys-typescale-title-medium-line-height);
    --magi-sys-title-s:var(--md-sys-typescale-title-small-size)/var(--md-sys-typescale-title-small-line-height);
    --magi-sys-body-l:var(--md-sys-typescale-body-large-size)/var(--md-sys-typescale-body-large-line-height);
    --magi-sys-body-m:var(--md-sys-typescale-body-medium-size)/var(--md-sys-typescale-body-medium-line-height);
    --magi-sys-body-s:var(--md-sys-typescale-body-small-size)/var(--md-sys-typescale-body-small-line-height);
    --magi-sys-label-l:var(--md-sys-typescale-label-large-size)/var(--md-sys-typescale-label-large-line-height);
    --magi-sys-label-m:var(--md-sys-typescale-label-medium-size)/var(--md-sys-typescale-label-medium-line-height);
    --magi-sys-label-s:var(--md-sys-typescale-label-small-size)/var(--md-sys-typescale-label-small-line-height);

    /* ── the spacing scale ────────────────────────────────────────────────
       8dp, where space100 = 8dp, with the 4dp half step the dense parts of a terminal need.
       The page carried twenty-six distinct paddings and gaps — 1.6 · 2.4 · 3.2 · 3.5 · 4.8 ·
       5.6 · 6.4 · 7.2 · 9.6 · 11.2 · 12.8 · 13.6 · 14.4 · 17.6 · 19.2 · 22.4 · 25.6 · 38.4dp —
       which is not a rhythm but the absence of one: each was chosen against its neighbour and
       none against a scale. Eight remain, and nothing moved more than 3.2dp getting here.
       Written as tokens so a value off the scale reads as a literal and a test can say so. */
    --magi-sys-space-50:0.25rem;  --magi-sys-space-100:0.5rem; --magi-sys-space-150:0.75rem; --magi-sys-space-200:1rem;
    --magi-sys-space-300:1.5rem;  --magi-sys-space-400:2rem;   --magi-sys-space-500:2.5rem;  --magi-sys-space-600:3rem;
    --magi-sys-space-700:3.5rem;  --magi-sys-space-800:4rem;   --magi-sys-space-1000:5rem;   --magi-sys-space-1200:6rem;
    --magi-sys-space-1600:8rem;

    /* ── the widths where the layout changes ──────────────────────────────
       Written in em, and the em in a media query is the reader's default font size — not this
       page's, which is why it is the one unit that answers them. M3's breakpoints are dp
       (600 · 840 · 1000), and these are those numbers at the 16px default: 37.5 · 52.5 · 62.5.

       A reader who sets their browser default to 32px is asking for text at twice the size, and
       the fleet table cannot honour that and stay a table: its seven columns have rem minima
       summing to 52.9rem, which at that setting wants 1693px of a 1265px window. In px the
       breakpoints sat still while the text grew and the row scrolled off the side. In em the
       window reads as half as wide, the narrow layout arrives, and the table becomes a list —
       which is what "reflow" asks for. Page zoom was never the broken case; it scales px too. */
    --magi-sys-measure: 74ch;   /* prose */
    --magi-sys-wide: 108ch;     /* transcript, where lines are code and wrapping costs more than width */
    /* What a whole screen of console may take. Wider than the transcript's measure on purpose: the
       fleet is a table and a table uses room, while prose inside it keeps --magi-sys-measure. Capped rather
       than unbounded so an ultrawide monitor does not stretch a row to a metre. */
    --magi-sys-page: 170ch;
  }

  * { box-sizing:border-box; }

  /* Keyboard focus, said once and loudly.
     A dashboard is a page of links and buttons, and the fleet is navigated with tab as readily as
     with a mouse — the underline this layout uses for a pressed state is not a focus ring, and a
     border colour that shifts by one step is not one either. :focus-visible so a mouse click does
     not leave a ring behind it, and an offset so the ring is not mistaken for the element's own
     rule. The outline:none below applies to :focus, which this then overrides for the keyboard. */
  :focus-visible {
    outline:2px solid var(--magi-ref-primary); outline-offset:3px; border-radius:var(--magi-sys-shape-xs);
  }
  html { scrollbar-gutter:stable; -webkit-text-size-adjust:100%; }
  body {
    /* Block, again. It was a flex column so the rail could be moved to the foot of a narrow page
       with an order property — and the rail is not drawn at all on that width any more, so the
       reason is gone. It cost something while it lasted: an auto margin on a flex item's cross axis
       sizes it to its CONTENT, which is why centring the page silently pinned it to 720px. */
    min-height:100vh;
    margin:0; background:var(--magi-ref-bg); color:var(--magi-ref-fg);
    font:var(--md-sys-typescale-body-medium-size)/1.65 var(--magi-ref-mono);
    -webkit-font-smoothing:antialiased; text-rendering:optimizeLegibility;
    font-variant-numeric:tabular-nums;  /* ages and step counts line up down the column */
  }
  [hidden] { display:none !important; }
  /* No caps blocks anywhere. The section heads on this page were set in uppercase with wide
     tracking — an editorial look — and the guide forbids it without an exception worth the name:
     "Avoid using caps blocks altogether; they're not accessible" and "use sentence case for all
     product text". It names the replacement too, and this page was already using it: these heads
     carry font-weight 600, so the hierarchy survives the change. The tracking came down with the
     caps, because letter-spacing set for capitals is too loose for lowercase. */
  /* Headings carry the structure, not the styling. Each of these already had its own size, weight
     and colour as a div; becoming h1/h2/h3 must not also drag in the browser's default type scale
     and margins, or the page would resize itself for a change that is meant to be invisible. */
  h1, h2 { font:inherit; margin:0; }
  /* Read, not seen. A live region has to be in the accessibility tree, so it cannot be display:none
     or visibility:hidden — it is clipped to nothing instead. */
  /* Plain tooltip, at the spec's numbers: 24dp tall, 8dp of padding, a 4dp corner, and the
     inverse surface pair so it reads against whatever it covers. Body-small is the type. The one
     number the spec does not give is a max width, so this picks one. */
  #tip {
    position:fixed; z-index:9; pointer-events:none;
    min-height:24px; padding:4px 8px; box-sizing:border-box;
    border-radius:var(--magi-sys-shape-xs); max-width:20rem;
    background:var(--md-sys-color-inverse-surface, var(--magi-ref-fg));
    color:var(--md-sys-color-inverse-on-surface, var(--magi-ref-bg));
    font:var(--md-sys-typescale-body-small-size, .75rem)/var(--md-sys-typescale-body-small-line-height, 1rem) var(--magi-ref-mono);
    letter-spacing:.4px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
  }
  .sr-only { position:absolute; width:1px; height:1px; margin:-1px; padding:0; overflow:hidden;
             clip-path:inset(50%); white-space:nowrap; border:0; }

  /* On opacity: every value below is set so the RESULT clears WCAG AA (4.5:1) against the page in
     BOTH themes, which is checked in page_test.go. Editorial layouts get their hierarchy from
     dimming secondary text, and the arithmetic is easy to get wrong twice over — the muted role is
     already lowered, and light mode has less headroom than dark. Measured before this note: eight
     of thirteen dimmed pairs were under, the worst at 2.47:1. */

  /* ── motion ─────────────────────────────────────────────────────────────
     M3's fade-through: what is leaving goes, then what arrives fades up from 96% rather than
     cutting in. Used where the page swaps one body of content for ANOTHER — a destination, a
     companion's two panels — which is exactly the transition the pattern is for. Not used on the
     three-second poll: the fleet redraws itself constantly and a page that flickered every tick
     would be unreadable, so this fires on navigation only.

     The scale is 96%, not 92%: a table of monospaced text at 92% is visibly the wrong size for a
     tenth of a second, and the point is to say "this is new", not to zoom. */
  @keyframes fadeThrough {
    from { opacity:0; transform:scale(.96); }
    to   { opacity:1; transform:none; }
  }
  /* Lateral: peers slide, they do not fade. The guide is explicit that a tab switch "does not use
     a fade or parallax effect" and says why — fading makes the peer relationship and the swipe
     gesture less obvious, and reads as forward-and-back instead of sideways. Two keyframes because
     the direction has to say which way you moved. */
  @keyframes slideFromRight { from { transform:translateX(12px); } to { transform:none; } }
  @keyframes slideFromLeft  { from { transform:translateX(-12px); } to { transform:none; } }
  @keyframes riseIn {
    from { opacity:0; transform:translateY(10px); }
    to   { opacity:1; transform:none; }
  }
  .enter { animation:fadeThrough 200ms var(--magi-sys-ease-emphasized) both; }
  /* No opacity in these: grouped elements moving in unison is the whole pattern, and a fade on top
     of the slide is the thing being avoided. */
  .slideL { animation:slideFromRight 200ms var(--magi-sys-ease-emphasized) both; }
  .slideR { animation:slideFromLeft 200ms var(--magi-sys-ease-emphasized) both; }
  .rise  { animation:riseIn 250ms var(--magi-sys-ease-emphasized) both; }

  /* Somebody who asked their machine to stop moving things gets a page that does not MOVE.
     Not a page that stops answering. The guide asks for "subtle fades instead of intense sliding
     or scaling", which is a swap and not a deletion, and the blanket 0.01ms was the deletion: a
     panel replaced its contents between two frames with nothing to say it had, which is the
     change a reader most needs told about and the one least likely to make anybody ill. What
     goes is displacement — translate, scale, and any transition that would carry a box across
     the screen. Opacity and colour stay, at a length short enough not to be a performance.
     0.01ms rather than 0 where a duration is still killed: it FIRES, so an animationend that
     something waits on still arrives. */
  @keyframes stillFade { from { opacity:0; } to { opacity:1; } }
  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after {
      animation-duration:0.01ms !important; animation-iteration-count:1 !important;
      transition-property:opacity, color, background-color, border-color, fill, stroke, box-shadow !important;
      transition-duration:120ms !important;
      scroll-behavior:auto !important;
    }
    /* The four the page animates itself. A class beats the universal selector, so these keep a
       duration where everything else loses one — the same fade for all four, because what made
       them different was the direction they moved and none of them moves now. */
    .enter, .rise, .slideL, .slideR { animation:stillFade 120ms var(--magi-sys-ease-standard) both !important; }
  }

  /* ── masthead ───────────────────────────────────────────────────────────── */
  header {
    position:sticky; top:0; z-index:2; background:var(--magi-ref-bg);
    border-bottom:1px solid var(--magi-ref-fg);
    box-shadow:0 3px 0 -2px var(--magi-ref-outlineVariant);   /* the hairline under the rule */
    padding:var(--magi-sys-space-150) var(--magi-sys-space-300) var(--magi-sys-space-100);
    padding-top:calc(var(--magi-sys-space-150) + env(safe-area-inset-top));
    display:flex; gap:var(--magi-sys-space-200); align-items:baseline; flex-wrap:wrap;
    max-width:var(--magi-sys-page); margin-inline:auto; padding-right:var(--magi-sys-space-500);
  }
  .mark {
    font:600 var(--magi-sys-headline-s) var(--magi-ref-display); letter-spacing:.01em; color:var(--magi-ref-primary);
    font-feature-settings:"liga" 1;
  }
  /* The session's own id, in the muted role — a nameplate's standing line. It is NOT the three
     councillors in their hues: this console shows no council, and the comment that said it did
     described a thing forty lines above, where those colours are declared and deliberately unused. */
  .sid { color:var(--magi-ref-muted); font-size:var(--md-sys-typescale-label-small-size); letter-spacing:.04em; opacity:.8; overflow-wrap:anywhere; }
  #state {
    margin-left:auto; font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.0533em;
    color:var(--magi-ref-muted); display:flex; align-items:center; gap:var(--magi-sys-space-100);
  }
  #state::before { content:""; width:6px; height:6px; border-radius:var(--magi-sys-shape-full); background:var(--magi-ref-outline); }
  /* The count is a readout; this part of it is a control, and it says so by being one. */
  #state .jump {
    --md-text-button-label-text-color:var(--magi-ref-warn);
    --md-text-button-hover-label-text-color:var(--magi-ref-warn);
    margin-left:calc(-1 * var(--magi-sys-space-50));
  }
  #state.live::before { background:var(--magi-ref-success); box-shadow:0 0 0 3px color-mix(in srgb, var(--magi-ref-success) 20%, transparent); }
  #state.lost::before { background:var(--magi-ref-error); }
  #back {
    color:var(--magi-ref-muted); text-decoration:none; font-size:var(--md-sys-typescale-label-small-size); letter-spacing:0.04em; border-bottom:1px solid var(--magi-ref-outlineVariant); padding-bottom:2px;
  }
  #back:hover { color:var(--magi-ref-primary); border-bottom-color:var(--magi-ref-primary); }

  /* A press must find 48dp even where the control is drawn smaller.
     The library does this inside its own shadow root with a .touch span; these are hand-built, so
     they say it here. The expander is centred on the visible box, taken out of flow so no row
     grows around it, and it belongs to the control — a press on it is a press on the control.
     ⚠ It is not applied to text links inside running prose: a card's title wraps across lines,
     and a 48dp box on an inline box is either fragmented or a lie. Those are the targets the rule
     exempts, because their size is set by the line height of the text they sit in. */
  .hit48 { position:relative; }
  .hit48::after {
    content:''; position:absolute; left:0; right:0; top:50%; transform:translateY(-50%);
    height:48px; border-radius:inherit;
  }
  /* By id, not by the class: the page assigns back.className outright when the crumb changes,
     so a class put on it in the markup lasts until the first navigation and then is gone. */
  #back { display:inline-block; position:relative; }
  #back::after {
    content:''; position:absolute; left:50%; top:50%; transform:translate(-50%, -50%);
    width:100%; min-width:48px; height:48px;
  }

  /* ── the rail: navigation on a wide screen, settings on a narrow one ────── */
  /* One element in two modes rather than two that have to agree. Wide: it stands beside the page
     as a rail and the hamburger widens it into a drawer. Narrow: it is off-screen and the same
     button slides it in over the page, with the tabs still doing the navigating — which is the
     handbook's arrangement and the one M3 describes for these two widths.

     The breakpoint is 768/769px, the handbook's, so the two products break in the same place. */
  /* The scrim is an ELEMENT, not a shadow on the rail. Drawn as a box-shadow it darkened the page
     without covering it: everything under it stayed clickable, so a page that looked disabled took
     a click and navigated away under an open drawer. It also gives the drawer the dismissal every
     modal surface has — a click on the page you can see is the way out of it. */
  /* Coloured on its background rather than on opacity. Not a style preference: the contrast check
     reads every opacity in this sheet as text being dimmed, because a container's opacity takes
     everything inside it down too — and it cannot tell from CSS that this box never holds text.

     And it does not animate. What is behind the drawer is behind it the moment it opens; a scrim
     that arrives over a quarter of a second is a quarter of a second in which the page is half
     covered and still live-looking. (It used to be a box-shadow on the rail, whose spread grew with
     the rail's width — so the dimming really did sweep across the page.) */
  #scrim {
    position:fixed; inset:0; z-index:3; background-color:transparent; pointer-events:none;
  }
  body[nav="open"] #scrim {
    background-color:color-mix(in srgb, var(--md-sys-color-scrim) 32%, transparent);
    pointer-events:auto;
  }
  #rail {
    position:fixed; top:0; bottom:0; left:0; z-index:3;
    width:var(--magi-comp-rail-now, var(--magi-comp-rail-w)); box-sizing:border-box;
    /* Over the page, not beside it: a floating drawer is the one that can open without moving
       anything. It says that it overlaps with a TONAL difference and an edge, not a shadow — a
       container role one step off the body's, plus a hairline. That is the guide's default way to
       separate two surfaces, and it is the one that survives when the scrim below it does the rest
       of the work. The shadow this comment used to promise went out with the box-shadow scrim. */
    z-index:4;
    padding:calc(var(--magi-sys-space-150) + env(safe-area-inset-top)) var(--magi-sys-space-100) var(--magi-sys-space-200);
    background:var(--magi-ref-surface-container-low); border-right:1px solid var(--magi-ref-outlineVariant);
    display:flex; flex-direction:column; gap:var(--magi-sys-space-200); overflow:hidden auto;
    /* Same curve and duration as the components use for a container that changes size, so the rail
       and the page's own margin arrive together rather than one chasing the other. */
    transition:width 250ms var(--magi-sys-ease-emphasized), transform 250ms var(--magi-sys-ease-emphasized);
  }
  /* Two numbers, not one. --magi-comp-rail-w is the gutter the PAGE reserves and it never changes; --magi-comp-rail-now
     is how wide the rail is drawing itself right now. Widening the rail used to widen the gutter,
     so the table shifted 184px right and lost 184px of width every time the drawer opened. The
     drawer floats over the gutter instead, and nothing on the page moves. */
  body[nav="open"] { --magi-comp-rail-now:16rem; }
  /* Collapsed, the rail is 4.5rem and a word like "connections" is not — so collapsed shows the
     icon and nothing else. Clipping the label instead would put half a word on screen, which reads
     as a bug rather than as a choice. The label still exists for a screen reader: it is the
     item's aria-label, set beside the text in paint(). */
  /* Wrapping, not clipping. At 2x text the guide asks that the whole label stay on screen and
     lets navigation items grow taller to hold it — so the label may take two lines rather than
     lose its tail. Collapsed still shows no label at all, which is the line below and a different
     decision. */
  #rail .lbl { overflow-wrap:anywhere; }
  body:not([nav="open"]) #rail .lbl { display:none; }
  /* Collapsed, the icon belongs on the rail's centre line, and it was 4px to the left of it: the
     item is 63px inside an 80px rail and its leading space put the 24px icon at the item's leading
     edge, not at the middle of either. The menu button was 8px out for the same reason.

     Moved with a transform rather than by changing the leading space, because the space is a
     custom property and a custom property does not transition — the icon would arrive at its new
     column between two frames while the rail beside it took 250ms to get there. Same curve and
     length as the rail's own width, so the two are one movement. */
  #rail [slot="start"], #railMenu { transition:transform 250ms var(--magi-sys-ease-emphasized); }
  body:not([nav="open"]) #rail md-list-item { --md-list-item-leading-space:16px; }
  body:not([nav="open"]) #rail [slot="start"] { transform:translateX(4px); }
  body:not([nav="open"]) #railMenu { transform:translateX(8px); }
  #rail .ic { flex:none; display:block; }
  #rail md-list {
    --md-list-container-color:transparent;
    --md-list-item-label-text-font:var(--magi-ref-mono);
    --md-list-item-label-text-size:var(--md-sys-typescale-label-medium-size);
    --md-list-item-label-text-weight:600;
    --md-list-item-label-text-color:var(--magi-ref-muted);
    --md-list-item-container-shape:var(--magi-sys-shape-full);
    --md-sys-color-primary:var(--magi-ref-primary);
    --md-sys-color-on-surface:var(--magi-ref-on-surface);
    --md-sys-color-on-surface-variant:var(--magi-ref-on-surface-variant);
    letter-spacing:0.05em;
  }
  /* Where you are, in the colour the rest of the page uses for it. A list item has no selected
     state of its own — it is a list, not a set of choices — so this is ours.

     Painted on the HOST, not through the component's tokens. --md-list-item-container-color does
     nothing in the shipped build (measured: the container stays transparent with the token set to
     an opaque colour), so the "filled shape" this comment used to claim was never drawn. And the
     icon is in the leading slot, which the label token does not reach — so with the rail collapsed
     to icons, which is how it stands most of the time, the page gave no sign at all of which
     destination you were on. Slotted content is styled from out here, so the icon takes the colour
     the same way the label does. */
  /* The two layers drawn over the item, which were two other shapes. A press painted a RECTANGLE
     across the pill: md-ripple is border-radius:inherit inside its own shadow root and its host
     carries none. The focus ring drew 8px, a third shape again — and setting the token on the item
     did nothing, because the component assigns --md-focus-ring-shape:8px INSIDE its shadow root,
     where an inherited value loses. Both are exposed as parts, which is the one way in. */
  #rail md-list-item::part(ripple) { border-radius:var(--magi-sys-shape-full); }
  #rail md-list-item::part(focus-ring) { --md-focus-ring-shape:var(--magi-sys-shape-full); }
  #rail md-list-item[selected] {
    background:color-mix(in srgb, var(--magi-ref-primary) 14%, transparent);
    border-radius:var(--magi-sys-shape-full);
    --md-list-item-label-text-color:var(--magi-ref-primary);
  }
  #rail md-list-item[selected] .lbl,
  #rail md-list-item[selected] [slot="start"] { color:var(--magi-ref-primary); }
  /* Three things change, not one. The guide asks a selected destination for an active indicator, a
     FILLED icon, and a heavier label — colour alone is the case it names as insufficient. These
     icons are drawn as strokes and have no filled twin, which is the case the guide answers too:
     "if an icon doesn't have a filled style, use a thicker or heavier version of the icon". */
  #rail md-list-item .lbl { font-weight:500; }
  #rail md-list-item[selected] .lbl { font-weight:700; }
  #rail md-list-item[selected] .ic path { stroke-width:2.4; }
  /* The badge, corrected. Its inner box is position:absolute at top / 50% across, which is right
     when the badge is laid over an icon and wrong everywhere else — dropped into a flow it anchors
     to whatever ancestor happens to be positioned and lands somewhere unrelated. Giving the host a
     size and a position makes the host the thing it anchors to, which is what a caller has to do
     for a component still in the library's unstable half. */
  md-badge {
    position:relative; display:inline-block; vertical-align:middle;
    width:18px; height:18px; flex:none;
    --md-badge-color:var(--magi-ref-warn);
    --md-badge-large-color:var(--magi-ref-warn);
    --md-badge-large-label-text-color:var(--magi-ref-bg);
    --md-badge-large-label-text-font:var(--magi-ref-mono);
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
  .badgewrap { width:16px; height:16px; margin-left:var(--magi-sys-space-50); }
  .badgewrap md-badge { position:absolute; inset:0; width:16px; height:16px; }
  .badgewrap:has(md-badge[hidden]) { display:none; }
  /* In the rail it rides the icon, which is what a badge is for — and when the rail is collapsed
     the icon is the only thing there. */
  .icwrap { position:relative; display:inline-flex; width:24px; height:24px; }
  .icwrap md-badge { position:absolute; top:-5px; right:-7px; width:16px; height:16px; }
  /* Widened, the row has a word in it and the count belongs after the word — riding the icon is
     what a badge does when the icon is all there is. Anchored to the ITEM rather than to the icon,
     so it lands at the end of the row. */
  body[nav="open"] #rail md-list-item { position:relative; }
  body[nav="open"] #rail .icwrap { overflow:visible; }
  /* Expanded, the badge is a child of the list item and lays itself out beside the label; the
     move is placeRailBadge()'s, and this rule only says what it looks like once it is there. */
  body[nav="open"] #rail md-list-item > md-badge { position:static; align-self:center; }
  #prefsForm { display:flex; flex-direction:column; gap:var(--magi-sys-space-200); min-width:16rem; }
  /* Both rows lay their controls out on one line and wrap on a narrow screen. .sktools had no
     display at all — four controls in a block, no gap, no shared baseline — which is what "the
     screen's controls" looked like until somebody measured it. */
  .skfind { display:flex; margin:0 0 var(--magi-sys-space-300); }
  .skfind md-outlined-text-field { flex:1 1 22rem; max-width:34rem; }
  .skwrite {
    display:flex; flex-wrap:wrap; gap:var(--magi-sys-space-200); align-items:flex-end;
    margin:var(--magi-sys-space-400) 0 0; padding-top:var(--magi-sys-space-300); border-top:1px solid var(--magi-ref-outlineVariant);
  }
  .skwrite md-outlined-select { flex:0 0 14rem; }
  .skwrite md-outlined-text-field { flex:1 1 22rem; }
  .skwrite .skmodel {
    flex:1 1 100%; font:var(--magi-sys-body-s) var(--magi-ref-mono); color:var(--magi-ref-muted); overflow-wrap:anywhere;
  }
  #notify { display:flex; flex-direction:column; align-items:flex-start; gap:var(--magi-sys-space-50); }
  #notifyWhy { font:var(--magi-sys-body-s) var(--magi-ref-mono); color:var(--magi-ref-muted); max-width:26rem; overflow-wrap:anywhere; }
  #prefsForm .k {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-muted);
  }
  #console { font:var(--md-sys-typescale-label-medium-size)/1.6 var(--magi-ref-mono); color:var(--magi-ref-muted); overflow-wrap:anywhere; }
  #console b { color:var(--magi-ref-fg); font-weight:600; }
  #railMenu, #themeToggle, #prefs {
    --md-icon-button-icon-color:var(--magi-ref-muted); color:var(--magi-ref-muted);
  }
  /* Where the icons are, not centred on whatever width the rail happens to be. Centred, it slid
     sideways every time the rail widened — the one control that should not move when you press it. */
  #railMenu { align-self:start; margin-left:var(--magi-sys-space-50); }
  #railMenu .ic-close { display:none; }
  body[nav="open"] #railMenu .ic-open { display:none; }
  body[nav="open"] #railMenu .ic-close { display:block; }
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
  /* Centred in what is left beside the rail. Left-aligned it left a 1233px gutter on a 2497px
     screen — the page in one corner of the window and nothing in the rest of it. Centring cannot
     re-wrap anything at that width, because the block is already narrower than the room: the
     re-wrap this was avoiding only happens when the drawer opening takes the available width BELOW
     the cap, and there it still fills and still does not move. */
  main {
    padding:var(--magi-sys-space-300) var(--magi-sys-space-500) calc(var(--dock, var(--magi-sys-space-1600)) + var(--magi-sys-space-400));
    max-width:var(--magi-sys-page); margin-inline:auto;
  }

  /* ── tabs: the resources this console shows ─────────────────────────────── */
  /* Wrapping, because these are sentences and there are three of them now: at 390px the three
     labels are wider than the column, and a nav that overflows takes the whole page sideways with
     it — the one scroll direction a phone should never get. */
  /* The divider under the strip. The spec draws it as part of the tab container — 1dp of
     outline-variant, separating the tabs from what they switch — and neither the bundle nor this
     page had it. Without it the strip and the content below read as one block. */
  #tabs { display:flex; flex-wrap:wrap; gap:var(--magi-sys-space-100) var(--magi-sys-space-300); padding:var(--magi-sys-space-200) 0 0;
          border-bottom:1px solid var(--magi-ref-outlineVariant); }

  /* ── what I had to say ───────────────────────────────────────────────────── */
  /* Grouped by what was said, because the repetition IS the finding: one correction is a remark,
     the same one to three companions is a rule waiting to be written. */
  /* Every section is the same width. They were not: this one was 74ch while lessons and MCP were
     108ch and the fleet filled the page, so changing menus moved the left edge and re-set the line
     length — three different pages rather than four views of one. The prose inside keeps its own
     measure, which is where a reading width belongs. */
  /* Both halves of one story, on one page: what has been said often enough to become a rule, and
     the rules. They were two destinations, and a reader had to know that promoting on one made
     something appear on the other.

     The corrections destination and its promotion pipeline are gone; what was learned is one list
     now. Its rules went with it — #ivs, .sectionhead and the whole .iv family styled a section
     that no longer exists, and a stylesheet that keeps them is one where the next reader cannot
     tell which selectors mean anything. */

  /* ── what they have learned ─────────────────────────────────────────────── */
  /* Two tiers on one page, the crossing one first. The boundary between them is the whole of
     context hygiene, and it is only as good as somebody's ability to see it: a rule in the global
     tier reaches every prompt on every project, and after the day it was written nothing else in
     the system mentions it again. */
  /* Wider than the prose measure: a rule's description reads like prose but the line under it
     carries a name, a date range and sometimes a file path, and 74ch put those on three lines. */
  #skills { display:block; max-width:var(--magi-sys-page); }
  .sk { border-bottom:1px solid var(--magi-ref-outlineVariant); padding:var(--magi-sys-space-200) 0; }
  .sk .top { display:flex; gap:var(--magi-sys-space-150); align-items:baseline; flex-wrap:wrap; }
  .sk .tier {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-muted);
    flex-basis:100%; order:-1;
  }
  .sk.global .tier { color:var(--magi-ref-warn); }
  /* A team's reach sits between the other two, and it gets the third colour rather than sharing
     one: painted --magi-ref-accent it was indistinguishable from a project skill, which is the one thing the
     tier word is on the row to tell you. */
  .sk.team .tier { color:var(--magi-ref-primary); }
  .sk.project .tier { color:var(--magi-ref-accent); }
  .sk .what { font:600 var(--md-sys-typescale-body-large-size)/1.35 var(--magi-ref-display); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  /* A fact is quoted, not instructed: it reads as something the companion believes rather than
     something it was told to do, which is the difference a person is judging on this page. */
  .sk.fact .what { font:italic 400 var(--md-sys-typescale-body-large-size)/1.4 var(--magi-ref-display); }
  .sk .meta { margin-top:var(--magi-sys-space-50); font-size:var(--md-sys-typescale-label-small-size); letter-spacing:.05em; color:var(--magi-ref-muted); }
  .sk .drop { margin-left:auto; }
  .sk .fold { margin-left:auto; }
  .sk .fold + .drop { margin-left:0; }
  /* The rule as written. A reading measure, because it is prose and the row is not. */
  .sk .body {
    margin:var(--magi-sys-space-100) 0 var(--magi-sys-space-50); padding:var(--magi-sys-space-100) 0 0; max-width:var(--magi-sys-measure);
    border-top:1px solid var(--magi-ref-outlineVariant);
    font:var(--md-sys-typescale-body-medium-size)/1.6 var(--magi-ref-mono); color:var(--magi-ref-fg); white-space:pre-wrap; overflow-wrap:anywhere;
  }

  /* ── what they can reach ────────────────────────────────────────────────── */
  /* An MCP server is where a companion's reach leaves this machine's file system. The list is
     read to answer one question — which of them can see that thing — so the transport line is
     monospace and complete rather than tidied. */
  /* Not prose at all: the transport line is a command with arguments and the line under it is an
     absolute path. Clipping either to a reading measure hides the part being read for. */
  #mcp { display:block; max-width:var(--magi-sys-page); }
  .srv { border-bottom:1px solid var(--magi-ref-outlineVariant); padding:var(--magi-sys-space-200) 0; }
  .srv .top { display:flex; gap:var(--magi-sys-space-150); align-items:baseline; flex-wrap:wrap; }
  .srv .tier {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-muted);
    flex-basis:100%; order:-1;
  }
  .srv.global .tier { color:var(--magi-ref-warn); }
  .srv.project .tier { color:var(--magi-ref-accent); }
  .srv .what { font:600 var(--magi-sys-body-l) var(--magi-ref-mono); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  .srv .how { margin-top:var(--magi-sys-space-50); font:var(--md-sys-typescale-label-medium-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); overflow-wrap:anywhere; }
  .srv .where { margin-top:var(--magi-sys-space-50); font-size:var(--md-sys-typescale-label-small-size); color:var(--magi-ref-muted); opacity:.85; overflow-wrap:anywhere; }
  .srv .drop { margin-left:auto; }
  /* Nothing here draws a box or a border: the field and the select bring their own outline, their
     own shape and their own 48dp target, and a second set drawn over them was two descriptions of
     one control that could only ever agree by accident. The form says how the controls are
     arranged and stops. */
  #mcpAdd { display:grid; gap:var(--magi-sys-space-200); margin:var(--magi-sys-space-300) 0; max-width:var(--magi-sys-measure); }
  #mcpAdd md-filled-button { justify-self:start; }
  #mcpAdd .note { font-size:var(--md-sys-typescale-label-small-size); color:var(--magi-ref-muted); }

  /* The recipe. The layer is a pseudo-element so the label's own contrast is never touched, and it
     is inert to the pointer so it can cover the whole control without eating its clicks. */
  .state { position:relative; }
  .state::after {
    content:''; position:absolute; inset:0; border-radius:inherit; pointer-events:none;
    background:currentColor; opacity:0; transition:opacity var(--magi-sys-dur-short2) var(--magi-sys-ease-standard);
  }
  .state:hover::after { opacity:.08; }
  .state:focus-visible::after, .state:active::after { opacity:.12; }
  /* A ring, not only a wash. The state layer is the focus STATE; the guide asks separately for a
     "ring-like keyboard focus indicator" so a keyboard user can see where they are — the library's
     own components draw one with md-focus-ring, and the elements built here had nothing. */
  .state:focus-visible { outline:3px solid var(--md-sys-color-secondary, var(--magi-ref-accent)); outline-offset:2px; }
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
  #summary { display:flex; flex-wrap:wrap; gap:var(--magi-sys-space-100); padding-bottom:var(--magi-sys-space-200);
             margin:var(--magi-sys-space-300) 0 var(--magi-sys-space-50); border-bottom:1px solid var(--magi-ref-outlineVariant); }
  /* Height left to the component. It was 40px, which is neither the 32dp the token asks for nor
     the 48dp target the same page asks for — the target is the bundle's job and it draws a 48px
     .touch regardless of how tall the container is. */
  .tile { --md-filter-chip-label-text-font:var(--magi-ref-mono); }
  /* Label large, which is the chip's own type role — it was title-medium, a heading size inside a
     chip. At 24px of line box in a 32dp container it left 4px above and below and read as cramped;
     the count is still the loud thing here because it is 600 against an 11px word. */
  /* On one line with the word beside it. cell() builds a div, and a div is a BLOCK: the count took
     a line of its own and pushed the word onto a second one, so 20px + 15px of content sat in a
     32dp chip and spilled out of both ends of it. Measured in a browser — the count's box started
     1.7px above the chip and the word's ended 1.7px below. Label large, which is the chip's own
     type role; it was title-medium, a heading size, which made the same overflow worse. */
  .tile .n {
    display:inline-flex; align-items:center;
    font:600 var(--md-sys-typescale-label-large-size, .875rem)/var(--md-sys-typescale-label-large-line-height, 1.25rem) var(--magi-ref-display);
    color:var(--magi-ref-fg); margin-right:var(--magi-sys-space-100);
  }
  .tile .k {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-muted);
    display:inline-flex; align-items:center; gap:var(--magi-sys-space-50);
  }
  /* A status dot AND the word — the colour is never the only thing carrying the state. */
  .tile .k::before { content:""; width:7px; height:7px; border-radius:var(--magi-sys-shape-full); background:currentColor; }
  .tile.waiting .k { color:var(--magi-ref-warn); }
  .tile.working .k { color:var(--magi-ref-success); }
  .tile.idle    .k { color:var(--magi-ref-accent); }
  .tile.gone    .k { color:var(--magi-ref-error); }
  /* A count of zero reads as zero; it does not need to be faint as well, and dimming it put the
     label under AA in both themes (2.25:1 in light — measured by the contrast check). */
  .tile[disabled] .n, .tile[disabled] .k { color:var(--magi-ref-muted); }

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
       sideways, including a narrow desktop. Sizing each column to what it holds is only half the
       job; the other half is checking the total against the space there is. */
    grid-template-columns: 7rem minmax(8rem, 1fr) minmax(12rem, 2.6fr) 3.5rem 4rem 7rem 6rem;
    gap:var(--magi-sys-space-200);
  }
  .thead {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-muted);
    padding:var(--magi-sys-space-200) 0 var(--magi-sys-space-100); border-bottom:1px solid var(--magi-ref-fg);
  }
  .thead .r, .card .r { text-align:right; }

  /* A row, and it must not read as a card. It carried a coloured left edge and a rounded corner
     while sitting flush against the next one — the two devices belong to different things: a card
     is a bounded surface with space around it, a row is a line in a table separated by a rule.
     Having both, with no gap, asked the reader to see cards that had been stacked without margins.
     The state is already said in the badge, twice over, as a word and a coloured dot. */
  .card {
    text-decoration:none; color:var(--magi-ref-on-surface); border-bottom:1px solid var(--magi-ref-outlineVariant);
    padding:var(--magi-sys-space-150) var(--magi-sys-space-150) var(--magi-sys-space-150); margin-left:calc(-1 * var(--magi-sys-space-150)); position:relative;
  }
  .card:hover { background:color-mix(in srgb, var(--magi-ref-primary) 5%, transparent); }
  .card.stopped { opacity:.8; }

  /* A team's heading. Set as a rule with a name on it rather than as a bar: this page separates
     with lines, and a filled band per team would turn a table into a stack of boxes. */
  .teamhead {
    display:flex; align-items:baseline; gap:var(--magi-sys-space-150); flex-wrap:wrap;
    margin:var(--magi-sys-space-300) 0 var(--magi-sys-space-50); padding:0 0 var(--magi-sys-space-50);
    border-bottom:1px solid var(--magi-ref-fg);
  }
  /* The first team sits closer to the table head above it. Written :first-of-type it matched
     nothing — the table head is a div too, and it is the first one — so all three headers carried
     the same 1.6rem and the list opened with a gap that looked like a missing row. */
  .thead + .teamhead { margin-top:var(--magi-sys-space-100); }
  .teamhead .tname {
    font:600 var(--md-sys-typescale-label-medium-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-fg);
  }
  .teamhead .thub { font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-accent); }
  .teamhead .tn { margin-left:auto; font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); }
  /* NOT position:static. The general md-badge rule above makes the host the thing the component's
     absolutely-positioned inner box anchors to, and that is the whole reason it exists — "dropped
     into a flow it anchors to whatever ancestor happens to be positioned and lands somewhere
     unrelated". Static put it back: both team badges rendered at the same point 616px and 498px
     away from their rows, stacked on each other, as a stray number floating over the table head.
     The inner box rides at left:50% of the host, so the host is widened to hold it instead. */
  .teamhead md-badge { position:relative; width:26px; }

  /* status */
  /* The column's word, for the width where the column heads are not drawn. */
  .colk { display:none; }
  @media (max-width:62.5em) {
    .colk {
      display:inline; margin-left:var(--magi-sys-space-50);
      font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.0467em; color:var(--magi-ref-muted);
    }
  }
  .card .badge {
    font:600 var(--md-sys-typescale-label-small-size)/1.6 var(--magi-ref-mono); letter-spacing:0.0467em; color:var(--magi-ref-muted);
    display:flex; align-items:center; gap:var(--magi-sys-space-100); flex-wrap:wrap;
  }
  .card .badge::before { content:""; width:7px; height:7px; border-radius:var(--magi-sys-shape-full); background:currentColor; flex:none; }
  .card.working .badge { color:var(--magi-ref-success); }
  .card.waiting .badge { color:var(--magi-ref-warn); }
  .card.idle .badge { color:var(--magi-ref-accent); }
  .card.abandoned .badge, .card.stopped .badge { color:var(--magi-ref-error); }

  /* name + workspace, the way a console stacks a resource over its namespace */
  .card .name { font:600 var(--md-sys-typescale-body-large-size)/1.3 var(--magi-ref-display); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  .card:hover .name { color:var(--magi-ref-primary); }
  .card .plan {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.05em; color:var(--magi-ref-muted); align-self:center;
  }
  .card .role {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:.04em; color:var(--magi-ref-accent);
    overflow-wrap:anywhere; margin-top:var(--magi-sys-space-50);
  }
  .card .path { font-size:var(--md-sys-typescale-label-small-size); color:var(--magi-ref-muted); opacity:.9; overflow-wrap:anywhere; }

  /* what it is doing: one line, clipped — the detail view is one click away for the rest */
  .card .last {
    font:italic var(--md-sys-typescale-body-medium-size)/1.45 var(--magi-ref-display); color:var(--magi-ref-fg);
    display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden;
  }
  .card .asking { font:600 var(--md-sys-typescale-label-medium-size)/1.45 var(--magi-ref-mono); color:var(--magi-ref-warn); overflow-wrap:anywhere; }
  .card .num { font-size:var(--md-sys-typescale-label-medium-size); color:var(--magi-ref-muted); }
  .card .host { font-size:var(--md-sys-typescale-label-small-size); color:var(--magi-ref-muted); overflow-wrap:anywhere; }
  .card .host b { font-weight:400; color:var(--magi-ref-fg); opacity:.85; }

  /* Row actions. Open is the row itself as well, but a named control is what makes it discoverable
     — and stopping must never require entering first, which is the whole point of a console. */
  /* One icon, inside the column. "open" was a word for something the whole row already does — the
     row is a link — and the pair of labelled buttons was wider than the 6rem column they sat in, so
     they hung off the right edge of the table. */
  .actions { display:flex; gap:var(--magi-sys-space-50); justify-content:flex-end; align-items:center; }
  .actions md-icon-button {
    --md-icon-button-icon-color:var(--magi-ref-muted);
    --md-icon-button-state-layer-width:40px; --md-icon-button-state-layer-height:40px;
    color:var(--magi-ref-muted);
  }
  .actions md-icon-button:hover { --md-icon-button-icon-color:var(--magi-ref-error); color:var(--magi-ref-error); }

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
    display:grid; grid-template-columns:6.5rem minmax(0, 1fr); gap:var(--magi-sys-space-50) var(--magi-sys-space-200);
    margin:var(--magi-sys-space-150) 0 var(--magi-sys-space-50); padding:var(--magi-sys-space-150) 0 0; max-width:var(--magi-sys-measure);
    border-top:1px solid var(--magi-ref-outlineVariant);
  }
  .grounds .gk {
    font:600 var(--md-sys-typescale-label-small-size)/1.6 var(--magi-ref-mono); letter-spacing:0.0533em;
    color:var(--magi-ref-muted); text-align:right;
  }
  .grounds .gv { font:var(--md-sys-typescale-label-medium-size)/1.55 var(--magi-ref-mono); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  @media (max-width:40em) {
    .grounds { grid-template-columns:1fr; gap:var(--magi-sys-space-50); }
    .grounds .gk { text-align:left; margin-top:var(--magi-sys-space-100); }
  }

  /* answering, inline in the row that is asking */
  /* Answering is the one place on the fleet where a person types, so it is the library's field and
     the library's buttons: focus ring, state layers and a 48dp target all come with them. What is
     said here is that the choice is the warning colour, because the agent is stopped until it. */
  .answer { display:flex; gap:var(--magi-sys-space-100); margin-top:var(--magi-sys-space-100); flex-wrap:wrap; align-items:center; }
  .answer md-filled-tonal-button {
    --md-filled-tonal-button-container-color:var(--magi-ref-surface-container-high);
    --md-filled-tonal-button-label-text-color:var(--magi-ref-fg);
    --md-filled-tonal-button-label-text-font:var(--magi-ref-mono);
    letter-spacing:0.05em;
  }
  .answer md-outlined-text-field { flex:1; min-width:11rem; }

  .empty { font:var(--md-sys-typescale-body-large-size)/1.7 var(--magi-ref-display); color:var(--magi-ref-muted); padding:var(--magi-sys-space-500) 0; max-width:52ch; }
  .empty code { font:var(--md-sys-typescale-body-medium-size)/1 var(--magi-ref-mono); color:var(--magi-ref-accent); }

  /* ── the agent's own header, so a detail page says what it is looking at ──── */
  /* The three panels on a companion's page are md-outlined-card: each one groups what is true
     about a single subject, which is what a card is for, and the outline replaces the hairline
     rule that used to separate them. The rows in the fleet keep their rule and are NOT cards —
     they are links, and this component has no ripple, no focus ring and no role, so making one a
     card would trade the keyboard for a box.

     A card lays its slotted children out itself (:host is flex, and a slot is display:contents),
     so a display of ours on the host wins and the children stay grid items. */
  /* No margin. Every place a card is put is a flex column with a gap, so a margin under it was
     that gap twice — 48px below the detail card where 24 was asked for — and #side already
     carried a margin-bottom:0 to undo it in the one place somebody noticed. The container
     spaces its children; the child does not space itself. */
  md-outlined-card { padding:var(--magi-sys-space-200) var(--magi-sys-space-200); }
  #detail { display:flex; flex-direction:column; gap:var(--magi-sys-space-200); }
  #detail .grid {
    /* auto-fit at 9rem packed a 60-character workspace path into the same cell as a four-letter
       step count. 14rem is the width of the longest SHORT field (the context reading), so the long
       ones take a whole row of their own instead of squeezing the rest.

       The column gap is a scale step wider than the row gap on purpose: auto-fit spends every
       pixel it saves on another column, so narrowing this one from 25.6dp to 24dp fitted a
       fourth column, took 119px off each cell and wrapped the long fields it was drawn to keep
       whole. Snapping to the scale is not the same as snapping to the nearest value. */
    display:grid; grid-template-columns:repeat(auto-fit, minmax(14rem, auto)); gap:var(--magi-sys-space-200) var(--magi-sys-space-400);
  }
  #detail .f { display:flex; flex-direction:column; gap:var(--magi-sys-space-50); }
  #detail .f .k {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em; color:var(--magi-ref-muted);
  }
  #detail .f .v { font:var(--magi-sys-body-m) var(--magi-ref-mono); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  #detail .f .v.state { font-weight:600; letter-spacing:0.05em; font-size:var(--md-sys-typescale-label-small-size); }
  /* The window, as a rule under the number rather than a gauge beside it: this is a fill level and
     the page already spends its colour on state. Unknown windows draw no bar at all — an empty
     track reads as "nearly empty", which is the opposite of "we do not know". */
  #detail .f .bar { height:2px; background:var(--magi-ref-outlineVariant); margin-top:var(--magi-sys-space-50); }
  #detail .f .bar i { display:block; height:100%; background:var(--magi-ref-primary); }
  #detail .f .bar.tight i { background:var(--magi-ref-warn); }
  #detail .f .v small { color:var(--magi-ref-muted); font-size:var(--md-sys-typescale-label-small-size); }
  /* Disabled is the component's own fade now, not a rule here. The contrast check reads this
     stylesheet and cannot see into a shadow root, so that opacity is not covered — which is the
     right answer rather than a gap: WCAG exempts inactive controls, and the repo's own rule
     against dimming was about text somebody still has to read. */
  #detail .f .fold { justify-self:start; }

  /* The conversation and the facts about it, side by side where there is room. The transcript is
     the wider of the two because its lines are code; the aside is a reading column of short
     labelled facts and does not want more than it needs. */
  #agentview { display:grid; grid-template-columns:minmax(0, 1fr); gap:var(--magi-sys-space-300); }
  /* The facts fold away. On this page they answer "what am I looking at" once, and after that they
     are 380px of masthead between the reader and the conversation they came for — measured at
     430px wide, the transcript began 1073px down a 900px screen. Folded, the summary line still
     carries the state and the workspace, which is what somebody glancing back actually wants. */
  #detail .foldbar {
    display:flex; align-items:baseline; gap:var(--magi-sys-space-150); width:100%; cursor:pointer;
    background:none; border:0; padding:0; color:inherit; text-align:left; font:inherit;
  }
  #detail .foldbar .caret { color:var(--magi-ref-muted); transition:transform 200ms var(--magi-sys-ease-standard); }
  #detail[folded] .foldbar .caret { transform:rotate(-90deg); }
  #detail .foldbar .sum {
    font:var(--magi-sys-body-s) var(--magi-ref-mono); color:var(--magi-ref-muted); margin-left:auto;
    overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
  }
  #detail[folded] .grid { display:none; }
  /* Two panels on one screen instead of six cards in a column.
     Below the two-column width the page stacked: the facts, then the conversation, then four cards
     of which three are history — so the transcript was off the first screen and the composer, fixed
     at the foot, was nowhere near the words it answers. The tabs put the conversation on its own
     screen and everything else on the other. Above it, both columns are visible and there is
     nothing to switch between. */
  /* Secondary tabs, drawn with primary tabs' tokens. These switch content INSIDE the companion
     destination, which is what the guide calls a secondary tab — and the second level of tabs on a
     page is exactly the case it says needs them. The bundle has no md-secondary-tab (checked), so
     the difference is made here: a 2dp indicator instead of 3dp, and on-surface for the active
     label and icon instead of primary. The one part no token reaches is the indicator spanning the
     whole tab rather than hugging the label; that is set as a property in paint(). */
  #ptabs {
    margin:0 0 var(--magi-sys-space-200); border-bottom:1px solid var(--magi-ref-outlineVariant);
    --md-primary-tab-active-indicator-height:2px;
    --md-primary-tab-active-label-text-color:var(--magi-ref-on-surface);
    --md-primary-tab-active-icon-color:var(--magi-ref-on-surface);
    --md-primary-tab-active-hover-label-text-color:var(--magi-ref-on-surface);
    --md-primary-tab-active-hover-state-layer-color:var(--magi-ref-on-surface);
    --md-primary-tab-active-pressed-label-text-color:var(--magi-ref-on-surface);
    --md-primary-tab-active-pressed-state-layer-color:var(--magi-ref-on-surface);
  }
  /* 840px, the start of the expanded breakpoint, where the guide recommends two panes. It was
     1100 — a number nobody's scale has. The five are compact <600, medium 600-839, expanded
     840-1199, large 1200-1599, extra-large 1600+, and two panes are recommended from expanded. */
  @media (min-width:52.5em) {
    #ptabs { display:none !important; }
  }
  @media (min-width:52.5em) {
    #agentview { grid-template-columns:minmax(0, 1fr) 22rem; align-items:start; }
    /* The facts stay put while the conversation scrolls: on this page they are the thing you keep
       glancing back at, and a plan that scrolls away is one you re-find rather than read. */
    #side { position:sticky; top:5.5rem; }
    /* A way out. Whichever it is — a fixed pane or a side sheet — the guide asks for one: a fixed
       pane gets a handle that collapses it, a side sheet "requires that a close affordance is
       always present". Without it nobody can tell whether the pane is transient or permanent, and
       the conversation cannot have the width back. */
    body[side="shut"] #agentview { grid-template-columns:minmax(0, 1fr); }
    body[side="shut"] #side { display:none; }
    #sideToggle { display:inline-flex; }
  }
  #sideToggle { display:none; align-self:flex-end; margin-bottom:calc(-1 * var(--magi-sys-space-150)); }
  #stream, #side { min-width:0; display:flex; flex-direction:column; gap:var(--magi-sys-space-300); }
  #side #plan, #side #handoffs, #side #history { max-width:none; }

  /* ── the agent's own plan ───────────────────────────────────────────────── */
  #plan { max-width:var(--magi-sys-measure); }
  /* The plan's own progress. Linear, at the top edge of the card it belongs to, spanning its
     width — where the guide puts a bar for the container that is progressing. The track is
     outline-variant, which is under 3:1 against the surface, so the spec makes the end stop
     mandatory rather than decorative. */
  #plan .planbar {
    display:block; margin:var(--magi-sys-space-50) 0 var(--magi-sys-space-50);
    /* The spec's numbers, which are also the component's defaults — set here so a change to either
       is visible as a change to this line rather than a silent drift. */
    --md-linear-progress-track-height:4px;
    --md-linear-progress-active-indicator-height:4px;
    --md-linear-progress-active-indicator-color:var(--magi-ref-primary);
    --md-linear-progress-track-color:var(--magi-ref-outlineVariant);
  }
  #plan .plancount { font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); margin-bottom:var(--magi-sys-space-100); }
  #plan .k {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em;
    color:var(--magi-ref-muted); margin-bottom:var(--magi-sys-space-100);
  }
  .td { display:grid; grid-template-columns:1.2rem 1fr; gap:var(--magi-sys-space-100); padding:var(--magi-sys-space-50) 0; }
  .td .mark { font:var(--md-sys-typescale-label-medium-size)/1.6 var(--magi-ref-mono); color:var(--magi-ref-muted); text-align:center; }
  .td .what { font-size:var(--md-sys-typescale-body-medium-size); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  .td.completed .what { color:var(--magi-ref-muted); text-decoration:line-through; }
  .td.in_progress .mark { color:var(--magi-ref-primary); }
  .td.in_progress .what { color:var(--magi-ref-primary); }

  /* ── work handed to other companions ────────────────────────────────────── */
  #handoffs { max-width:var(--magi-sys-measure); }

  /* ── the board: a column per companion, a card per piece of work ────────── */
  #board { display:block; max-width:var(--magi-sys-page); }
  .boardhead { display:flex; gap:var(--magi-sys-space-200); align-items:center; margin:0 0 var(--magi-sys-space-200); flex-wrap:wrap; }
  .lanehead .lrole { font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); overflow-wrap:anywhere; }
  .lanehead .lteam { font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-accent); }
  .wcard .wwhat { color:inherit; text-decoration:none; cursor:pointer; }
  .wcard .wwhat:hover { text-decoration:underline; }
  .wcard .wlong { font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); }
  .wcard .wmodel {
    font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-accent); overflow-wrap:anywhere;
  }
  /* A label is pressable, so it is drawn as something that can be pressed — a chip's shape, at the
     size of the line it sits on rather than the size of a control, because a card carrying three
     of them is still a card. */
  .wcard .wlabel { font:inherit; border:0; cursor:pointer; }
  /* Drawn to the chip's own spec rather than to the line it sits on: height 32dp, Label Large,
     8dp between one and the next. It was 23dp tall with 4dp gaps, which is under the 24dp floor
     for any target at all, and the press target is a further 48dp on top — the guide states that
     one separately from the container height, so the chip stays 32 and the reach is 48. */
  .wcard .wlabel {
    display:inline-flex; align-items:center; min-height:2rem; cursor:pointer;
    /* 16dp between rows, not 8: the press target is 48dp on a 32dp chip, and at the 8dp
       the guide gives as a minimum the targets of two wrapped rows overlap by 8dp — the
       lower one takes presses aimed at the upper. Measured, not assumed. */
    margin:var(--magi-sys-space-200) var(--magi-sys-space-100) 0 0;
    font:600 var(--md-sys-typescale-label-large-size)/1.25rem var(--magi-ref-mono); letter-spacing:.06em;
    color:var(--magi-ref-primary); background:color-mix(in srgb, var(--magi-ref-primary) 12%, transparent);
    border-radius:var(--magi-sys-shape-full); padding:0 var(--magi-sys-space-150);
  }
  .wcard .wlabel:hover { background:color-mix(in srgb, var(--magi-ref-primary) 22%, transparent); }
  /* The arrows sit level with the field's box, not with the row's centre — the field is 56dp tall
     and carries a floating label above its text, so centring on the row puts them over the label. */
  .boardhead md-icon-button { align-self:end; }
  /* Scrolls sideways, and ONLY here. The page must never do it, but a board of lanes is the one
     shape where sideways is the reading direction, and clipping a lane would hide a companion. */
  .lanes { display:flex; gap:var(--magi-sys-space-300); align-items:flex-start; overflow-x:auto; padding-bottom:var(--magi-sys-space-100); }
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
  /* Set apart from the chips it sits beside: they filter this list and it leaves it. */
  .lanes::after { content:""; flex:0 0 1.4rem; }   /* the last lane gets a right edge too */
  .lanehead {
    display:flex; gap:var(--magi-sys-space-100); align-items:baseline;
    border-bottom:1px solid var(--magi-ref-fg); padding-bottom:var(--magi-sys-space-50); margin-bottom:var(--magi-sys-space-100);
  }
  .lanehead .lname {
    font:600 var(--md-sys-typescale-label-medium-size)/1.4 var(--magi-ref-mono); letter-spacing:0.0467em; color:var(--magi-ref-fg);
  }
  .lanehead .lcount { margin-left:auto; font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); }
  .wcard {
    border:1px solid var(--magi-ref-outlineVariant); border-radius:var(--magi-sys-shape-s);
    padding:var(--magi-sys-space-100) var(--magi-sys-space-150); margin-bottom:var(--magi-sys-space-100); background:var(--magi-ref-surface-container-low);
  }
  .wcard .wwhen { font:var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-muted); }
  .wcard .wwhat { font-size:var(--md-sys-typescale-body-medium-size); line-height:1.5; color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  /* The one running now, in the colour the rest of the page uses for that. */
  .wcard.now { border-color:var(--magi-ref-success); }
  .wcard.now .wwhen { color:var(--magi-ref-success); font-weight:600; }

  /* ── what this companion did before now ─────────────────────────────────── */
  #history { max-width:var(--magi-sys-measure); }
  #history .k {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em;
    color:var(--magi-ref-muted); margin-bottom:var(--magi-sys-space-100);
  }
  .hs { display:grid; grid-template-columns:5.5rem 1fr; gap:var(--magi-sys-space-50) var(--magi-sys-space-200); padding:var(--magi-sys-space-50) 0; }
  .hs + .hs { border-top:1px solid var(--magi-ref-outlineVariant); }
  .hs .when { font:var(--md-sys-typescale-label-small-size)/1.6 var(--magi-ref-mono); color:var(--magi-ref-muted); text-align:right; }
  .hs .what { font-size:var(--md-sys-typescale-body-medium-size); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  /* The one it is in now is work too, and it is the newest row. Marked rather than left off. */
  .hs.now .when { color:var(--magi-ref-success); font-weight:600; }
  #handoffs .k {
    font:600 var(--md-sys-typescale-label-small-size)/1.4 var(--magi-ref-mono); letter-spacing:0.06em;
    color:var(--magi-ref-muted); margin-bottom:var(--magi-sys-space-100);
  }
    .ho { display:grid; grid-template-columns:8rem 1fr; gap:var(--magi-sys-space-50) var(--magi-sys-space-200); padding:var(--magi-sys-space-100) 0; }
  .ho .to { font:600 var(--md-sys-typescale-label-small-size)/1.6 var(--magi-ref-mono); letter-spacing:.08em; color:var(--magi-ref-accent); text-align:right; }
  .ho .req { font:var(--magi-sys-body-l) var(--magi-ref-display); color:var(--magi-ref-fg); overflow-wrap:anywhere; }
  .ho .ans { grid-column:2; font-size:var(--md-sys-typescale-label-medium-size); color:var(--magi-ref-muted); overflow-wrap:anywhere; }
  .ho.working .to { color:var(--magi-ref-primary); }

  /* ── transcript ─────────────────────────────────────────────────────────── */
  /* Monospace throughout: every line here is something the machine said or did, and a serif would
     be dressing up evidence. The editorial part is the rhythm — a wide gutter of small-caps labels
     against a single column of text. */
  #log { max-width:var(--magi-sys-wide); }
  .row { display:grid; grid-template-columns:6.5rem 1fr; gap:var(--magi-sys-space-200); align-items:start; padding:var(--magi-sys-space-50) 0; }
  .who {
    font:600 var(--md-sys-typescale-label-small-size)/1.9 var(--magi-ref-mono); letter-spacing:0.0533em;
    color:var(--magi-ref-muted); text-align:right; user-select:none; opacity:.8;
  }
  .txt { white-space:pre-wrap; overflow-wrap:anywhere; }

  /* A user turn is the anchor you scan for: set as a lead, with the rule an editorial layout uses
     for a pull quote. */
  .row.user { margin:var(--magi-sys-space-300) 0 var(--magi-sys-space-150); }
  .row.user .txt {
    font:var(--md-sys-typescale-body-large-size)/1.55 var(--magi-ref-display); color:var(--magi-ref-primary);
    border-left:2px solid var(--magi-ref-primary); padding-left:var(--magi-sys-space-200); margin-left:calc(-1 * var(--magi-sys-space-200));
  }
  .row.user .who { color:var(--magi-ref-primary); }
  .row.assistant .txt { color:var(--magi-ref-fg); }
  .row.thinking .txt { color:var(--magi-ref-muted); font-style:italic; opacity:.8; }
  .row.tool .txt { color:var(--magi-ref-accent); }
  .row.tool .who { color:var(--magi-ref-accent); }
  .row.result .txt, .row.failed .txt {
    color:var(--magi-ref-muted); border-left:1px solid var(--magi-ref-outlineVariant);
    padding:var(--magi-sys-space-50) 0 var(--magi-sys-space-50) var(--magi-sys-space-150); max-height:11rem; overflow:auto;
  }
  .row.failed .who, .row.failed .txt { color:var(--magi-ref-error); border-left-color:var(--magi-ref-error); }

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
    background:linear-gradient(to top, var(--magi-ref-bg) 88%, transparent);
    padding-bottom:env(safe-area-inset-bottom);
  }
  #prompt {
    background:var(--magi-ref-bg);
    padding:var(--magi-sys-space-200) 0 var(--magi-sys-space-150);
  }
  /* The rule belongs to the column, not to the window. Drawn on #prompt it ran the full width of
     the viewport while the words under it started 514px in, which reads as a divider for the whole
     page rather than the top of one bar. */
  #prompt .inner { border-top:2px solid var(--magi-ref-warn); padding-top:var(--magi-sys-space-200); }
  /* The dock stands in the same column as the page. It used to centre its own narrower measure in
     whatever space was left, which put the composer 235px to the right of the text it answers — the
     footer read as belonging to a different page. Same max-width and same centring as main, and the
     reading measure applies INSIDE that, pinned left. */
  /* The dock stands in the same column as the page: same max-width, same centring, same inner
     padding as main. It used to centre a narrower measure of its own in whatever space was left,
     which put the composer 235px right of the text it answers, and matching only the OUTER box
     left it 25px off — the padding is part of where a column starts. */
  #prompt .inner, form {
    max-width:var(--magi-sys-page); margin-inline:auto; width:100%; box-sizing:border-box;
    padding-left:var(--magi-sys-space-300); padding-right:var(--magi-sys-space-300);
  }
  #prompt .asking { font:600 var(--md-sys-typescale-body-medium-size)/1.5 var(--magi-ref-mono); color:var(--magi-ref-warn); overflow-wrap:anywhere; }

  /* ── composer ───────────────────────────────────────────────────────────── */
  form {
    padding-top:var(--magi-sys-space-200); padding-bottom:var(--magi-sys-space-200); display:block;
  }
  /* Under the row, not inside the field. As the field's own supporting text it added 20px to the
     field's height, and the buttons — bottom-aligned so they stay put as the box grows — sat 20px
     below the box they belong to. It also reads better here: the note is about what pressing send
     will do, which is the row's business and not the box's. */
  #cnote {
    font:var(--magi-sys-body-s) var(--magi-ref-mono); color:var(--magi-ref-muted); margin-top:var(--magi-sys-space-100);
    padding-left:var(--magi-sys-space-50); overflow-wrap:anywhere;
  }
  .composer {
    display:flex; gap:var(--magi-sys-space-200); align-items:flex-end;
    border-top:1px solid var(--magi-ref-fg); padding-top:var(--magi-sys-space-150);
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
    --md-text-button-label-text-color:var(--magi-ref-error);
    --md-text-button-hover-label-text-color:var(--magi-ref-error);
    --md-text-button-hover-state-layer-color:var(--magi-ref-error);
  }
  md-text-button {
    --md-text-button-label-text-font: var(--magi-ref-mono);
    /* label-large, the role M3 assigns to a button. It was 11px — label-SMALL, a scale value in the
       wrong role — on eight of the page's twelve buttons. The editorial identity is the face and
       the letterspacing, both of which stay; M3 asks for a different typeface to keep the scale,
       not for the scale to be shrunk to fit a look. */
    --md-text-button-label-text-size: var(--md-sys-typescale-label-large-size);
    --md-text-button-label-text-line-height: var(--md-sys-typescale-label-large-line-height);
    --md-text-button-label-text-weight: 500;
    --md-text-button-label-text-color: var(--magi-ref-muted);
    --md-text-button-hover-label-text-color: var(--magi-ref-primary);
    --md-text-button-focus-label-text-color: var(--magi-ref-primary);
    --md-text-button-pressed-label-text-color: var(--magi-ref-primary);
    --md-text-button-hover-state-layer-color: var(--magi-ref-primary);
    --md-text-button-pressed-state-layer-color: var(--magi-ref-primary);
    letter-spacing:0.0467em;
  }
  /* Removing something reads in the error colour on the way to being pressed, and only there: a
     control that is red at rest is a warning, and these are ordinary. */
  md-text-button.drop {
    --md-text-button-hover-label-text-color: var(--magi-ref-error);
    --md-text-button-focus-label-text-color: var(--magi-ref-error);
    --md-text-button-pressed-label-text-color: var(--magi-ref-error);
    --md-text-button-hover-state-layer-color: var(--magi-ref-error);
    --md-text-button-pressed-state-layer-color: var(--magi-ref-error);
  }
  /* Interrupting reads the same way, and it is an ICON button — named md-text-button.stop it was
     styling nothing, so the one control on the fleet that halts a running turn was the only
     destructive-feeling thing on the page that stayed grey under the cursor. */
  md-icon-button.stop {
    --md-icon-button-hover-icon-color: var(--magi-ref-error);
    --md-icon-button-focus-icon-color: var(--magi-ref-error);
    --md-icon-button-pressed-icon-color: var(--magi-ref-error);
    --md-icon-button-hover-state-layer-color: var(--magi-ref-error);
    --md-icon-button-pressed-state-layer-color: var(--magi-ref-error);
  }
  md-outlined-text-field#t { flex:1; }
  md-outlined-text-field {
    --md-sys-color-primary: var(--magi-ref-primary);
    --md-sys-color-on-surface: var(--magi-ref-on-surface);
    --md-sys-color-on-surface-variant: var(--magi-ref-on-surface-variant);
    --md-sys-color-outline: var(--magi-ref-outline);
    --md-sys-color-surface: transparent;
    --md-outlined-text-field-input-text-font: var(--magi-ref-mono);
    /* 16px, and not because the scale says so: under 16 iOS Safari zooms the page when a field
       takes focus and does not zoom back. The component's own default is smaller. */
    --md-outlined-text-field-input-text-size: 16px;
    --md-outlined-text-field-label-text-font: var(--magi-ref-mono);
  }
  /* The select is a text field wearing a menu, and it reads its own copy of these. */
  md-outlined-select {
    --md-sys-color-primary: var(--magi-ref-primary);
    --md-sys-color-on-surface: var(--magi-ref-on-surface);
    --md-sys-color-on-surface-variant: var(--magi-ref-on-surface-variant);
    --md-sys-color-outline: var(--magi-ref-outline);
    --md-sys-color-surface-container: var(--magi-ref-surface-container);
    --md-outlined-select-text-field-input-text-font: var(--magi-ref-mono);
    --md-outlined-select-text-field-input-text-size: 16px;
    --md-outlined-select-text-field-label-text-font: var(--magi-ref-mono);
  }
  /* The composer's two are Material Web buttons. Their shape, state layers, ripple and touch
     target come from the component — this page only tells them which colours magi uses, through
     the --md-sys-* properties the library reads. Writing any of the rest here again is how two
     descriptions of one button start to disagree. */

  /* State layers, not colour swaps: M3 puts the on- colour over the surface at a fixed opacity.
     Doing it with a pseudo-element keeps the label's own contrast untouched, which dimming or
     recolouring the text does not. */

  /* ── the table, when the table does not fit ──────────────────────────────
     A separate breakpoint from the navigation's, because it answers a different question. 768px is
     where a rail stops being worth its width; this is where seven columns stop fitting, which is a
     fact about the columns. Tying the two together would mean moving one every time the other's
     reason changed.

     The row's own comment used to say it "collapses to two lines on a phone". Nothing collapsed it
     — the comment described a mechanism that was never written, and the page scrolled sideways at
     every width instead. */
  @media (max-width:62.5em) {
    .thead { display:none; }   /* no columns left to label */
    .card {
      grid-template-columns:auto auto 1fr;
      gap:var(--magi-sys-space-50) var(--magi-sys-space-200); padding:var(--magi-sys-space-200) var(--magi-sys-space-150) var(--magi-sys-space-200);
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

  @media (max-width:40em) {
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
      .composer md-filled-button, .composer md-filled-tonal-button { order:3; flex:0 0 auto; }
    /* The component, not the element it replaced. These two rules named "textarea" and "button",
       which the composer has not held since it became Material Web — dead selectors, so on a phone
       the field never took its own row and was squeezed to a third of one. The same slip as the
       host rules that could not reach a label: a migration that leaves the old CSS behind leaves
       it pointing at nothing. */
    .composer md-outlined-text-field#t { flex:1 0 100%; }
    header { padding-left:var(--magi-sys-space-200); padding-right:var(--magi-sys-space-200); }
    main { padding:var(--magi-sys-space-200) var(--magi-sys-space-200) calc(var(--dock, var(--magi-sys-space-1600)) + var(--magi-sys-space-300)); }
    .card .name { font:600 var(--magi-sys-title-l) var(--magi-ref-display); }
    .row { grid-template-columns:1fr; gap:var(--magi-sys-space-50); }
    .who { text-align:left; }
    .row.user .txt { font-size:var(--md-sys-typescale-body-large-size); }
    /* The prompt's inner column narrows with the rest of them. Left out, it kept the 1.4rem the
       wide layout gives it and the question sat 6px right of the transcript it is about — which is
       the same misalignment the dock had, one breakpoint down. */
    form, #prompt .inner { padding-left:var(--magi-sys-space-200); padding-right:var(--magi-sys-space-200); }
    /* A phone's rail is a section at the foot of the page rather than a drawer, so there is nothing
       to open and nothing to dim behind. */
    #scrim { display:none; }

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
  @media (min-width:37.5em) {
    /* The rail's room is taken from the BODY, not from the page's own padding. Taken from the
       padding it came out of the CONTENT box, which is why the masthead's rule ran 102px further
       left than the words above it — a border is drawn on the box and padding sits inside it. */
    body { padding-left:var(--magi-comp-rail-w); }
    header, main, #prompt .inner, form { padding-left:var(--magi-sys-space-300); padding-right:var(--magi-sys-space-300); }
    #dock { padding-left:var(--magi-comp-rail-w); }
    /* Open, it is a floating panel. Closed, it is part of the furniture. */

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
  @media (max-width:37.4375em) {
    /* The masthead on one line. It wrapped, so the brand and the count each took a row. */
    header { padding-top:calc(var(--magi-sys-space-100) + env(safe-area-inset-top)); padding-bottom:var(--magi-sys-space-100); gap:var(--magi-sys-space-100); }
    header .mark { font-size:var(--md-sys-typescale-title-large-size); }
    /* The count on the SAME line as the brand. Given its own row it cost 40px of the first screen
       to say something that fits beside a five-letter word. It is allowed to shrink and to clip:
       "5 agents · 2 waiting" is legible at any truncation that keeps the number. */
    /* One line, clipped at the end rather than wrapped. Squeezed between the brand and two icons it
       broke "5 AGENTS ·" across three rows, which is taller than the two-row masthead it replaced. */
    #state {
      font-size:var(--md-sys-typescale-label-small-size); letter-spacing:.08em; margin-left:auto; min-width:0;
      white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
    }
    /* The two icon buttons sit at the end of the same line, not on one of their own. Adding the
       gear pushed the masthead back onto two rows and the first agent from 337px to 393px —
       everything gained on this width, given straight back. */
    header { flex-wrap:nowrap; }
    #prefs, #themeToggle {
      flex:none;
      --md-icon-button-state-layer-width:40px; --md-icon-button-state-layer-height:40px;
    }
    /* The crumb is hidden only where the tab strip below already says the same thing. On a
       companion's page there are no tabs, and hiding it left a masthead reading "magi" with no
       word anywhere for WHICH companion — the one question that page exists to answer. */
    body:not([at="agent"]) #crumbs { display:none; }
    /* And it must be able to give room back. At 320px the masthead's own children came to 13px
       more than the 16dp margins leave, which the row absorbed by pushing the last button past
       the padding — far enough that its 48dp touch expander crossed the viewport edge and the
       page could be scrolled 1px sideways. The crumb is the one item here made of text that can
       be cut, so it is the one that yields. */
    #crumbs { font-size:var(--md-sys-typescale-label-small-size); min-width:0; overflow:hidden; }
    #crumbs #back { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; display:block; }
    /* The filters as one scrolling row rather than three stacked ones. Four chips do not fit across
       390px and never will; a row that scrolls keeps them one line high and keeps the fourth
       reachable, which stacking also did but at three times the cost. */
    #summary {
      flex-wrap:nowrap; overflow-x:auto; scrollbar-width:none;
      margin:var(--magi-sys-space-200) 0 var(--magi-sys-space-50); padding-bottom:var(--magi-sys-space-150);
    }
    #summary::-webkit-scrollbar { display:none; }
    .tile { flex:0 0 auto; }
    /* The tab strip is a navigation, not a heading: it does not need the room a heading takes. */
  }
  @media (max-width:37.4375em) {
    #rail {
      position:static; transform:none; width:auto; overflow:visible;
      border-right:0; border-top:1px solid var(--magi-ref-outlineVariant);
      background:none; padding:var(--magi-sys-space-300) var(--magi-sys-space-300) var(--magi-sys-space-400); margin-top:var(--magi-sys-space-300);
    }
    /* Nothing but navigation, and on this width the tabs do that — so the rail is not drawn at
       all. The preferences it used to carry are in the dialog now. */
    #rail { display:none; }
    /* The aside is four cards of context under a conversation, which is a long way to scroll for
       something you glance at. Tighter, so the transcript keeps the screen. */
    #side { gap:var(--magi-sys-space-150); }
    #side md-outlined-card { padding:var(--magi-sys-space-200) var(--magi-sys-space-200); }
    #history .hs, .ho { grid-template-columns:4.5rem 1fr; }
    }

</style>

<header>
  <!-- The hamburger is on both sizes and means two different things, which is the point: on a wide
       screen it widens the rail into a drawer, on a narrow one it slides that drawer in over the
       page. Either way it is the way to the settings, which is why a phone has it too even though
       a phone navigates with the tabs. -->
  <!-- The page's one h1. Assistive tech navigates by heading and this page had none — every
       section head was a styled div, so there was nothing to jump between. Levels follow the
       CONTENT hierarchy, not the type size: the product, then the groups inside each destination.
       h2 for those groups and not h3 — measured in a browser, the fleet and the board have no
       section head above them, so h3 there skipped a level from this h1, which the guide forbids
       outright. Two levels is all this page has. -->
  <h1 class="mark">magi</h1>
  <!-- Where you are, always, in both views: magi / fleet, or magi / fleet / <agent>. The middle
       crumb is the way back, which is the same element that says where back goes. -->
  <nav id="crumbs"><a href="/" id="back">companions</a><span id="crumbSep" hidden>/</span><span id="crumbHere"></span></nav>
  <span class="sid" id="sid"></span>
  <!-- The one place that speaks. Errors, the connection dropping, and a search narrowing all
       changed silently before this: a screen reader was never told. role=status with a polite
       live region announces without stealing focus, which is what the guide asks for a status
       message and what it names for search results appearing. -->
  <span id="state" role="status" aria-live="polite"></span>
  <!-- The page's one announcer. A live region has to be in the document BEFORE its text changes —
       one created per render is inserted already-full and says nothing. Everything that changes
       without moving focus speaks through here. -->
  <span id="say" class="sr-only" role="status" aria-live="polite"></span>
  <!-- One tooltip for the page. Native title= only appears on hover, never on keyboard focus, and
       carries no role — so every icon-only control on this page was unlabelled for anyone tabbing
       through it. This surface is aria-hidden: the button beside it already carries the same words
       as its aria-label, and announcing both would say everything twice. -->
  <span id="tip" role="tooltip" aria-hidden="true" hidden></span>
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

<div id="scrim"></div>
<nav id="rail">
  <!-- The button that widens the rail lives IN the rail, beside what it moves. In the masthead's
       far corner it did not look like it belonged to the column across the page. -->
  <!-- Two icons, one shown at a time. The guide asks that an expanded rail's menu icon say it can
       be collapsed, and aria-expanded alone says that only to a screen reader — the sighted user
       pressing it twice got the same three lines both times. -->
  <md-icon-button id="railMenu" aria-expanded="false">
    <svg class="ic-open" viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <path d="M3 6h18M3 12h18M3 18h18" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
    </svg>
    <svg class="ic-close" viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
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
        <svg class="ic" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true"><path
          d="M4 19v-1.6a3.4 3.4 0 0 1 3.4-3.4h2.2a3.4 3.4 0 0 1 3.4 3.4V19M8.5 6.2a2.6 2.6 0 1 1 0 5.2 2.6 2.6 0 0 1 0-5.2M15.5 19v-1.6a3.4 3.4 0 0 0-1.2-2.6M15 6.4a2.6 2.6 0 0 1 0 5"
          fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
        <md-badge id="railBadge" hidden></md-badge>
      </span>
      <span class="lbl"></span>
    </md-list-item>
    <md-list-item id="railSkills" type="link">
      <svg slot="start" class="ic" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true"><path
        d="M5 4.5h9.5A2.5 2.5 0 0 1 17 7v12.5H7.5A2.5 2.5 0 0 1 5 17zM19 6.5v13M8.5 8.5h5M8.5 11.5h5"
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
  <md-tabs id="ptabs" hidden>
    <md-primary-tab id="ptabTalk"></md-primary-tab>
    <md-primary-tab id="ptabState"></md-primary-tab>
  </md-tabs>
  <div id="agentview">
    <div id="stream">
      <md-outlined-card id="detail" hidden></md-outlined-card>
      <div id="log"></div>
    </div>
    <md-icon-button id="sideToggle" aria-expanded="true">
      <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
        <path d="M4 5h16v14H4zM14 5v14" stroke="currentColor" stroke-width="1.6" fill="none"
          stroke-linecap="round" stroke-linejoin="round"/></svg>
    </md-icon-button>
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
<md-dialog id="prefsDialog" type="alert">
  <div slot="headline" id="prefsK"></div>
  <!-- No theme here. It has a toggle in the masthead — one tap for the setting that gets changed
       most — and a select saying the same thing three feet away was the same preference twice, with
       two ways to be wrong about it. What is left is what a toggle cannot carry: a choice of three
       languages, and which machine this is. -->
  <form slot="content" method="dialog" id="prefsForm">
    <md-outlined-select id="lang"></md-outlined-select>
    <div class="k" id="notifyK"></div>
    <div id="notify"><md-text-button id="notifyBtn"></md-text-button><div id="notifyWhy"></div></div>
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
    <md-outlined-text-field id="t" type="textarea" rows="1"
      ></md-outlined-text-field>
    <md-filled-button type="submit" id="send">send</md-filled-button>
    <md-filled-tonal-button type="button" id="stop">interrupt</md-filled-tonal-button>
  </div><div id="cnote" hidden></div></form>
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
// A companion's state in the reader's language. Not tr() directly: tr falls back to the KEY, which
// would put "state.gone" on a row, and the raw word is the better fallback because it is at least
// the thing itself. Five states reached the screen in English on a Korean page while the pack
// carried all five — written, and never called.
const stateWord = s => L['state.' + s] || s || '';
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
    embedModel = c.embedModel || '';
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
// ── notifications ────────────────────────────────────────────────────────────
// A phone that is asleep, told that a companion is blocked.
//
// # Why this is the only channel that reaches anybody
//
// The tab title carries the count, and that is a channel to a person who has the tab in front of
// them. The fleet page exists for the person who does not. Everything else this console can do
// requires somebody to be looking at it.
//
// # Three ways this can be unavailable, and they are different
//
// A switch that is simply missing teaches nobody anything, so each reason says itself:
//
//   - no service worker or no PushManager — an old browser, and nothing to be done here;
//   - not a secure context — the page is being read over plain http from another machine. Note that
//     a tunnel to localhost IS secure, and magi-web only ever binds loopback, so the ordinary way
//     of reaching this from a phone already qualifies;
//   - permission denied — the browser was told no once and will not ask again from a click. Only
//     the reader can undo that, in the browser's own settings.
const notifyBtn = document.getElementById('notifyBtn');
const notifyWhy = document.getElementById('notifyWhy');
let vapidKey = null;

// A base64url key becomes the Uint8Array PushManager wants. It takes no other form.
const keyBytes = k => {
  const b = atob(k.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((k.length + 3) % 4));
  return Uint8Array.from(b, c => c.charCodeAt(0));
};

async function currentSub() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return undefined;
  const reg = await navigator.serviceWorker.getRegistration();
  return reg ? reg.pushManager.getSubscription() : null;
}

async function paintNotify() {
  const why = (key, on) => {
    notifyWhy.textContent = tr(key);
    notifyBtn.disabled = !on;
  };
  document.getElementById('notifyK').textContent = tr('notify.k');
  // The static demo has no console behind it and does not export the worker. Checked first, because
  // every reason below it would be the browser's and this one is the page's.
  if (globalThis.MAGI_DEMO) {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.demo', false);
  }
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.unsupported', false);
  }
  if (!window.isSecureContext) {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.insecure', false);
  }
  if (Notification.permission === 'denied') {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.denied', false);
  }
  const sub = await currentSub();
  notifyBtn.textContent = tr(sub ? 'notify.off' : 'notify.on');
  why(sub ? 'notify.is_on' : 'notify.how', true);
}

notifyBtn.onclick = async () => {
  // The prompt is asked for FIRST, before anything is awaited. requestPermission needs transient
  // user activation, and an await hands the turn back to the event loop — the activation is spent
  // by the time the call is reached, and it resolves 'default' without ever showing a prompt. That
  // is exactly what "it does not ask for permission" looks like: a button that does nothing.
  //
  // Harmless when a subscription already exists, which is the other thing this button does: a
  // permission already granted resolves immediately and shows nobody anything.
  const asked = 'Notification' in window && Notification.permission !== 'granted'
    ? Notification.requestPermission() : Promise.resolve('granted');
  notifyBtn.disabled = true;
  try {
    const existing = await currentSub();
    if (existing) {
      // Told BOTH sides. Unsubscribing only in the browser leaves this console posting to an
      // endpoint that answers 410 for a while and then stops existing; telling only the console
      // leaves the browser holding a subscription nothing will ever use.
      await post('/push', new URLSearchParams({
        endpoint: existing.endpoint, p256dh: '-', auth: '-', delete: '1'}));
      await existing.unsubscribe();
      return paintNotify();
    }
    if (await asked !== 'granted') return paintNotify();
    if (!vapidKey) {
      const info = await fetchList('/push');
      vapidKey = info && info.key;
    }
    if (!vapidKey) { notifyWhy.textContent = tr('notify.nokey'); return; }
    const reg = await navigator.serviceWorker.register('/sw.js');
    // The worker has to be running before it can be subscribed against; a registration that is
    // still installing has no active worker and subscribe throws.
    await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.subscribe({
      // Every browser requires this. A subscription that could deliver silently is a subscription
      // a page could use to track somebody without showing them anything.
      userVisibleOnly: true,
      applicationServerKey: keyBytes(vapidKey),
    });
    const j = sub.toJSON();
    await post('/push', new URLSearchParams({
      endpoint: j.endpoint, p256dh: j.keys.p256dh, auth: j.keys.auth}));
  } catch (e) {
    notifyWhy.textContent = String(e && e.message || e);
  } finally {
    paintNotify();
  }
};

labels$.pipe(distinctUntilChanged()).subscribe(() => { if (painted) paint(); });

const fleetEl = document.getElementById('fleet'), log = document.getElementById('log');
const state = document.getElementById('state'), sidEl = document.getElementById('sid');
const back = document.getElementById('back'), f = document.getElementById('f');
const summaryEl = document.getElementById('summary');
const tabsEl = document.getElementById('tabs');
// ── the companion page's two panels, below the two-column width ──────────────
// Which of a companion's two halves is on screen. Only meaningful under 840px, where the columns
// have collapsed into a stack; above it both are visible and the strip is display:none.
//
// Not in the URL. The destination is the companion; which half of it you were reading is a scroll
// position, and putting it in the address bar would make a link somebody sends land on a screen
// they did not mean to share. It does not survive a reload either, on purpose: arriving at a
// companion, the conversation is what you came for.
const ptabs = document.getElementById('ptabs');
const streamEl = document.getElementById('stream'), sideEl = document.getElementById('side');
const detailEl = document.getElementById('detail');
let panel = 'talk';
// A media query object rather than a width read: it fires on the change, so a window dragged past
// the breakpoint re-lays out without waiting for anything else to happen.
const wide = matchMedia('(min-width:52.5em)');
function drawPanels() {
  const s = sock();
  ptabs.hidden = !s || wide.matches;
  if (!s || wide.matches) {
    // Both halves, as they were. Nothing may stay hidden from a previous narrow visit.
    if (sideEl) sideEl.hidden = false;
    if (detailEl) detailEl.hidden = !s;
    log.hidden = !s;
    return;
  }
  const talk = panel === 'talk';
  log.hidden = !talk;
  detailEl.hidden = talk;
  sideEl.hidden = talk;
}
// Only when the reader switched, not on the poll that redraws the facts four times a minute.
// Sideways, in the direction the reader moved. Talk sits left of state, so arriving at state comes
// in from the right and going back to talk comes in from the left — which is what tells somebody
// these two are peers rather than one being under the other.
function revealPanel(fromIndex) {
  const how = fromIndex === undefined ? 'enter'
            : (panel === 'state' ? 'slideL' : 'slideR');
  reveal(panel === 'talk' ? log : detailEl, how);
  if (panel !== 'talk') reveal(sideEl, how);
}
ptabs.addEventListener('change', () => {
  const was = panel;
  panel = ptabs.activeTabIndex === 1 ? 'state' : 'talk';
  drawPanels();
  revealPanel(was === panel ? undefined : 0);
  measureDock();
});
wide.addEventListener('change', drawPanels);
const intervenedEl = document.getElementById('intervened');
const skillsEl = document.getElementById('skills'), tabSkills = document.getElementById('tabSkills');
const boardEl = document.getElementById('board');
const mcpEl = document.getElementById('mcp');
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
const railSkills = document.getElementById('railSkills');
// Which resource this console is showing. A companion's own page is neither — it is one level in.
// Corrections used to be a destination of its own and is now the first half of the experience
// page. An address somebody kept still lands on the thing it was pointing at.
// board went the same way in the other direction: it was a destination and is now reached from the
// fleet, because it is a question ABOUT the fleet — what these companions have been doing — rather
// than a fifth place to be. Both addresses still land where they pointed.
// mcp joined them. What a companion has LEARNED and what it can REACH are the two halves of what
// an organisation shares — one is knowledge and one is capability, and both are managed by the same
// person on the same afternoon. Two destinations for them made the reader carry the connection.
const RENAMED = {interventions: 'skills', mcp: 'skills'};
const view = () => {
  const v = new URLSearchParams(location.search).get('v') || 'fleet';
  return RENAMED[v] || v;
};
const crumbSep = document.getElementById('crumbSep'), crumbHere = document.getElementById('crumbHere');
// The four sections, named as nouns: a tab is a place you are, and "what I had to say" is a
// sentence about it. The same words do three jobs — the tab, the crumb, and the browser title —
// so they are written once.
const SECTION_KEY = {fleet: 'nav.companions', skills: 'nav.shared', board: 'nav.board'};
const SECTION = new Proxy({}, {get: (_, v) => tr(SECTION_KEY[v] || 'nav.companions')});

const HREF = {fleet: '', skills: '?v=skills', board: '?v=board'};
// In the order they are written in the markup, because md-tabs addresses its tabs by index.
// The board is not among them. It keeps its address and its crumb; what it lost is a permanent
// seat in a navigation that has to fit on a phone, for a screen somebody opens when they have a
// question about the past rather than one they live on.
const TABS = ['fleet', 'skills'];

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
// The unit stays compact — s/m/h/d reads the same in every language this ships in, and a table
// column sized for "4s" cannot hold "4 seconds". Only the word is translated, which is the part
// that was English on an otherwise Korean row.
const ago = s => s < 0 ? '' : tr('time.ago', {d: dur(s)});

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

  const badge = cell('badge', stateWord(a.state));
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
  tip(stop, tr('action.interrupt'));
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
  // Written out here in English until now, on a page where every other word came from the pack —
  // and invisible to the audit that finds unasked-for phrases, because a column nobody translated
  // has no key to go unused. The last column is the actions, which has no heading to give.
  for (const [c, key] of [['', 'col.status'], ['', 'col.agent'], ['', 'col.doing'],
                          ['r', 'col.steps'], ['r', 'col.age'], ['', 'col.host'], ['r', '']]) {
    h.append(cell(c, key ? tr(key) : ''));
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
    b.append(cell('n', n + ''), cell('k', stateWord(k)));
    b.onclick = () => {
      filter = filter === k ? null : k;
      render();
      if (filter) jumpToFirstRow();
    };
    return b;
  }));
  // The way to the board, from the list it is about. Text rather than a chip: the chips are a
  // filter on this table and this is not — a control that looked like them and did something else
  // would be the worst of both.
  // …and only when there is a past to look at. On a machine with no companions the board can
  // never have held anything, and a control that can be pressed to reach a blank screen is worse
  // than one that is not there — the same rule the zero tiles above already follow.
  if (list.length) {
    // An icon, not a word. The row it sits in is four counting chips, and a fifth thing shaped like
    // a word reads as a fifth count — this is a way OUT of the list rather than a filter on it, and
    // the shape is what says so. It keeps its name in the tooltip and its aria-label, because an
    // icon alone is a guess for anybody who has not pressed it once.
    const past = document.createElement('md-icon-button');
    past.className = 'toboard';
    tip(past, tr('nav.board'));
    past.setAttribute('aria-label', tr('nav.board'));
    past.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
      '<path d="M4 5.5h5v13H4zM9.5 5.5h5v8h-5zM15 5.5h5v10.5h-5z" fill="none" ' +
      'stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    past.onclick = () => { history.pushState({}, '', at(HREF.board)); render(); };
    box.append(past);
  }
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
    // Four characters including the "+", which is what the badge container is drawn to hold.
    b.value = n ? (n > 999 ? '999+' : String(n)) : '';
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
// jumpToNextWaiting brings the next blocked companion into view, skipping the one just answered.
//
// Skipped by socket rather than by "the first waiting row", because the fleet is polled and the row
// just answered can still be drawn as waiting for one more tick — landing back on it would look
// like the answer did not take.
function jumpToNextWaiting(justAnswered) {
  requestAnimationFrame(() => {
    for (const row of fleetEl.querySelectorAll('.card.waiting')) {
      if (row.getAttribute('href') && justAnswered &&
          row.getAttribute('href').includes(encodeURIComponent(justAnswered))) {
        continue;
      }
      if (row.scrollIntoView) row.scrollIntoView({block: 'center', behavior: 'smooth'});
      return;
    }
  });
}

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
  // Answering moves on to the next one still waiting.
  //
  // Two blocked companions among twenty is a list you hunt through twice: the count at the top
  // takes you to the first, and after that you are on your own — scrolling a table looking for the
  // other pause marker. Answering is a QUEUE, not a browse, and the difference between the two is
  // whether the page advances or leaves you where you were.
  //
  // It does not switch the filter or navigate anywhere. Somebody who was reading the whole fleet
  // stays reading the whole fleet; the view just moves to the row that now needs them. When
  // nothing else is waiting it does nothing at all, which is the right end of a queue.
  const send = (text) => post('/answer', new URLSearchParams({call: a.askId, kind: a.askKind, text}),
                              a.socket, a.peer)
    .then(loadFleet)
    .then(() => jumpToNextWaiting(a.socket));
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
    for (const [label, decision] of [['action.allow', 'allow'], ['action.always', 'always'],
                                     ['action.deny', 'deny']]) {
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
      const b = document.createElement('md-filled-tonal-button'); b.textContent = tr(label);
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
// Whether the prompt bar was already up. It is redrawn on every poll while an agent waits, so
// animating on "it is visible" would restart the entrance three times a minute under somebody who
// is trying to read the question.
let promptWasUp = false;
function drawPrompt(a) {
  const box = document.getElementById('prompt');
  if (!a || a.state !== 'waiting') {
    box.hidden = true; box.replaceChildren(); promptWasUp = false; measureDock(); return;
  }
  const inner = document.createElement('div'); inner.className = 'inner';
  const k = document.createElement('div'); k.className = 'asking'; k.textContent = '⏸ ' + a.asking;
  inner.append(k);
  const why = grounds(a);
  if (why) inner.append(why);
  // A question is answered in the composer, not in a second box above it. Both drawn, the page had
  // two text fields one over the other — the upper one answering the question and the lower one
  // sending a fresh request to an agent that is not listening — and no mark saying which was which.
  // A permission prompt keeps its own controls: they are buttons, so nothing collides, and leaving
  // the composer live there is deliberate — "do something else instead" is a legitimate reply to
  // being asked whether to drop a table.
  if (a.askKind !== 'question') inner.append(answerBox(a));
  box.replaceChildren(inner);
  box.hidden = false;
  if (!promptWasUp) reveal(box, 'rise');
  promptWasUp = true;
  answerMode(a.askKind === 'question' ? a : null);
  measureDock();
}

// What the composer is for right now. Null when it sends work; an agent when it answers that
// agent's question. Held here rather than read off the DOM because the submit handler needs the
// call id, and a call id parked in a data attribute is a string that can go stale without anything
// noticing.
let answering = null;
// One field in two roles rather than two fields that have to agree about which is live. The
// component is the same md-outlined-text-field; what changes is the label, the supporting line
// under it, and where pressing the button sends what was typed.
function answerMode(a) {
  answering = a;
  t.setAttribute('label', tr(a ? 'label.answer' : 'label.ask'));
  const note = document.getElementById('cnote');
  note.textContent = a ? tr('answer.instead') : '';
  note.hidden = !a;
  document.getElementById('send').textContent = tr(a ? 'action.answer' : 'action.send');
  // The old text was addressed at magi and the new question is not the same subject. Carrying it
  // over would put a half-written request in front of somebody as though it were their answer.
  if (!!a !== wasAnswering) { t.value = ''; }
  wasAnswering = !!a;
}
let wasAnswering = false;

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
// Which day a timestamp fell on, in the READER's timezone.
//
// It used to be the first ten characters of the RFC3339 string, which is the UTC day — and it was
// compared against todayISO(), which is the local one. East of UTC in the evening the two disagree,
// so the board showed the wrong day's work and could filter out a session running at that moment:
// measured at 00:30 KST, four cards became one, and the one that survived was the only one whose
// UTC and local days happened to straddle the boundary.
//
// A person asking what happened on the 9th means their own 9th. Formatted through the same offset
// arithmetic todayISO uses, so the two are the same function applied to two different instants.
const dayOf = ts => {
  const t = Date.parse(ts || '');
  if (Number.isNaN(t)) return '';
  return new Date(t - new Date(t).getTimezoneOffset() * 60000).toISOString().slice(0, 10);
};
const todayISO = () => new Date(Date.now() - new Date().getTimezoneOffset() * 60000)
  .toISOString().slice(0, 10);
// The time of day, local, as HH:MM. Empty for anything unparseable rather than "Invalid".
const hhmm = ts => {
  const t = Date.parse(ts || '');
  if (Number.isNaN(t)) return '';
  return new Date(t - new Date(t).getTimezoneOffset() * 60000).toISOString().slice(11, 16);
};
let boardDay = '';
let boardQuery = '';

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
  // Reading a week backwards is the common way to use this, and a date field makes that four
  // interactions per day: open the picker, find the cell, click, wait. A step is one click. The
  // field stays because jumping to a date a month back is the other way to use it, and stepping
  // there would be thirty clicks.
  const step = (delta, key) => {
    const b = document.createElement('md-icon-button');
    b.setAttribute('aria-label', tr(key));
    tip(b, tr(key));
    b.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
      '<path d="' + (delta < 0 ? 'M14.5 5.5 8 12l6.5 6.5' : 'M9.5 5.5 16 12l-6.5 6.5') +
      '" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" ' +
      'stroke-linejoin="round"/></svg>';
    b.onclick = () => {
      // Parsed as UTC and stepped in whole days, so the answer does not depend on which side of a
      // daylight-saving change the reader is standing on: local-midnight arithmetic lands on 23:00
      // the previous day twice a year, and a board that skips a day is worse than no arrows.
      const d = new Date(boardDay + 'T00:00:00Z');
      d.setUTCDate(d.getUTCDate() + delta);
      boardDay = d.toISOString().slice(0, 10);
      loadBoard();
    };
    return b;
  };
  const today = document.createElement('md-text-button');
  today.textContent = tr('board.today');
  today.onclick = () => { boardDay = todayISO(); loadBoard(); };
  // Narrowing a day's work by what it was about. Ranked the same way the shared-knowledge search
  // is, so "the one about retries" finds it without knowing how the request was worded — and over
  // the cards already fetched, so it narrows as you type rather than after a round trip.
  const find = document.createElement('md-outlined-text-field');
  find.setAttribute('label', tr('label.find'));
  find.value = boardQuery;
  find.addEventListener('input', () => { boardQuery = find.value; loadBoard(); });
  // Tomorrow holds nothing: the store only has what has happened. The forward arrow is disabled on
  // today rather than hidden, because a control that vanishes moves the ones beside it.
  const fwd = step(1, 'board.next');
  if (boardDay >= todayISO()) fwd.setAttribute('disabled', '');
  head.append(step(-1, 'board.prev'), day, fwd, today, find);

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
    let work = runs[i].filter(h => dayOf(h.started) <= boardDay && dayOf(h.ended) >= boardDay);
    if (boardQuery.trim()) {
      const order = rankByIDF(boardQuery,
        work.map(h => [h.title, h.model, ...(h.labels || [])].filter(Boolean).join(' ')));
      work = order.map(k => work[k]);
    }
    if (!work.length) return;
    anything = true;
    const lane = cell('lane');
    const title = document.createElement('h2');
    title.className = 'lanehead';
    title.append(cell('lname', a.name));
    // Who did it is the column, so the label that is missing from a card is what it was FOR. The
    // team and the role are what the fleet already publishes about this companion — nothing new is
    // recorded to put them here.
    if (a.role) title.append(cell('lrole', a.role));
    if (a.team) title.append(cell('lteam', a.team));
    title.append(cell('lcount', work.length + ''));
    lane.append(title);
    for (const h of work) {
      // The card is a CONTAINER, not a button. A card is allowed to be one large target with
      // nothing actionable inside it, or a plain container holding actions — not both. It used to
      // be both: an onclick on the card and another on every label, with stopPropagation between
      // them, which is the shape the guide names when it says an action must not sit on an
      // actionable surface. It also put every one of those actions on a div, so a keyboard could
      // reach none of them — the fleet row beside it has been an <a> the whole time.
      const card = cell('wcard' + (h.current ? ' now' : ''));
      // The clock face is the reader's too. A card that said 22:00 for work somebody started at
      // seven in the morning is not telling them about their own day.
      const when = cell('wwhen', h.current ? tr('board.now') : hhmm(h.started));
      card.append(when);
      // The title is the way in. It carries the address so the companion is reachable with a middle
      // click and a copied url, the same as the fleet row.
      const what = document.createElement('a');
      what.className = 'wwhat';
      what.href = href(a);
      what.textContent = h.title || tr('history.untitled');
      what.onclick = e => { e.preventDefault(); go(a.socket, a.peer); };
      card.append(what);
      // How long it took, when it is over. A card that says only when it started tells you nothing
      // about whether the day went well.
      if (!h.current && h.started && h.ended) {
        const mins = Math.round((Date.parse(h.ended) - Date.parse(h.started)) / 60000);
        if (mins > 0) card.append(cell('wlong', dur(mins * 60)));
      }
      // Which engine did it. The lane says WHO, and a companion's model can be changed mid-life —
      // so the one it is on now says nothing about the work on this card, and this is the only
      // place that fact survives.
      if (h.model) card.append(cell('wmodel', h.model));
      // What the agent said it was about. First on the card after the title, because it is the one
      // line somebody scanning a week is actually reading for.
      for (const l of h.labels || []) {
        // A button, because pressing it does something. As a div with an onclick it was invisible
        // to the keyboard and announced as nothing.
        const chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'wlabel hit48';
        chip.textContent = l;
        // Pressing one searches for it, which is the whole point of a label: the second piece of
        // work that carries it is what you are looking for.
        chip.onclick = () => { boardQuery = l; loadBoard(); };
        card.append(chip);
      }
      lane.append(card);
    }
    lanes.append(lane);
  });

  boardEl.replaceChildren(head,
    anything ? lanes : emptyState('board.nothing', 'board.nothing_how'));
}

// What the board would draw, as one string. The poll below rebuilds only when this changes: the
// cards come from every companion's history, and redrawing an identical board would blink the lane
// somebody is reading and throw away the sideways scroll of the strip.
async function boardSig() {
  const list = await fetchList('/fleet');
  if (!list) return null;
  const runs = await Promise.all(list.map(a =>
    fetchList('/history?d=' + encodeURIComponent(a.socket) + (a.peer ? '&p=' + encodeURIComponent(a.peer) : ''))
      .then(h => h || [])));
  return JSON.stringify(runs);
}

// A list from this console, or null when the console itself cannot be reached.
//
// The three loaders had this same try/catch, and the distinction it draws is the one thing they
// must not get differently: "magi-web is not answering" is a fact about the page you are looking
// at, and it is not the same as a companion being quiet. Null, so a caller cannot mistake the
// failure for an empty list and draw "nothing here" over a screen that simply lost its server.
async function fetchList(path) {
  try { return await (await fetch(path)).json(); }
  catch { state.className = 'lost'; says(tr('error.unreachable')); return null; }
}

async function loadFleet() {
  const list = await fetchList('/fleet');
  if (!list) return;
  state.className = '';
  const waiting = list.filter(a => a.state === 'waiting').length;
  retitle(waiting);

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
    tr(list.length === 1 ? 'count.agent' : 'count.agents', {n: list.length}) +
    (waiting ? ' · ' : '')));
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
  const h = document.createElement('h2');
  h.className = 'teamhead';
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

// Folded or not, remembered. A preference somebody sets on one companion means the same thing on
// the next one — it is a statement about how much of the screen they want the conversation to have,
// not about this agent.
function setFolded(want) {
  const box = document.getElementById('detail');
  box.toggleAttribute('folded', want);
  const bar = box.querySelector('.foldbar');
  if (bar) bar.setAttribute('aria-expanded', want ? 'false' : 'true');
  localStorage.setItem('facts', want ? 'folded' : 'open');
  measureDock();
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
  const grid = cell('grid');
  grid.append(
    field('field.status', stateWord(a.state), 'state ' + a.state),
    field('field.workspace', a.workdir),
    ...(a.role ? [field('field.role', a.role)] : []),
    ...(a.team ? [field('field.team', a.team + (a.hub ? ' · ' + tr('team.speaks') : ''))] : []),
    field('field.host', (a.host || 'this machine') + (a.addr ? ' · ' + a.addr : '') +
                  (a.pid ? ' · pid ' + a.pid : '')),
    field('field.steps', a.steps ? a.steps + '' : '—'),
    field('field.last_activity', ago(a.idle)),
    field('field.session', a.session),
  );
  // A button, not a clickable div: this is the one control on the card and it has to be reachable
  // by keyboard and announce itself as pressed or not.
  const bar = document.createElement('button');
  bar.type = 'button';
  bar.className = 'foldbar hit48';
  bar.append(cell('caret', '▾'), cell('k', tr('field.facts')),
             (() => {
               // The same rule as the status line: it ellipses, so it has to be readable in full
               // somewhere. The workdir is the part that gets cut and the part somebody is looking
               // for.
               const sum = cell('sum', stateWord(a.state) + ' · ' + a.workdir);
               tip(sum, stateWord(a.state) + ' · ' + a.workdir);
               return sum;
             })());
  bar.onclick = () => setFolded(!box.hasAttribute('folded'));
  box.replaceChildren(bar, grid);
  setFolded(localStorage.getItem('facts') === 'folded');
  box.hidden = false;
  // Which of the two panels it belongs to when the columns have stacked. Called here rather than
  // left to render(), because this runs on every fleet poll and render() does not.
  if (sock()) drawPanels();
  drawPlan(a);
  drawHandoffs(a);
  // Returned rather than dropped: the caller does not wait for it, but a caller that WANTS to —
  // a test, or a later screen that needs the whole panel settled — has no other way to know when
  // the slow half landed, and a promise nobody can await is a promise nobody can check.
  return drawContext(a, box, grid, field);
}

// ── the plan it is working through ───────────────────────────────────────────
// The agent's own todo list, as it last recorded it. Shown as it is: an item it dropped is gone,
// because the record is the whole plan each time and merging would resurrect what it decided
// against.
async function drawPlan(a) {
  const box = document.getElementById('plan');
  const todos = await fetchList('/plan' + qFor(a));
  if (!todos || !todos.length) { box.hidden = true; box.replaceChildren(); return; }
  // completed | in_progress | pending, which is the todo tool's whole enum. A branch for 'done'
  // sat here and a .td.done rule sat in the stylesheet, both waiting on a value the schema forbids.
  const mark = t => t.status === 'completed' ? '✓'
                  : t.status === 'in_progress' ? '▸' : '·';
  // How much of the plan is behind it. Determinate, because the counts are known — an
  // indeterminate bar here would say "something is happening" to somebody who can already see
  // exactly what is happening in the list below it.
  const done = todos.filter(t => t.status === 'completed').length;
  const pct = Math.round(done / todos.length * 100);
  // The library's own, not one drawn here. It was a div with a width — which meant reimplementing
  // the stop indicator, the role, and every state by hand. md-linear-progress is in the stable half
  // of Material Web and ships all three.
  const bar = document.createElement('md-linear-progress');
  bar.value = done / todos.length;
  // Named for what it is measuring, not "loading": a progress bar that only says "progress" tells
  // a screen reader nothing the number did not already say.
  bar.setAttribute('aria-label', tr('plan.progress', {done: done, total: todos.length}));
  bar.className = 'planbar';
  box.replaceChildren(cell('k', tr('field.plan')), bar,
    cell('plancount', tr('plan.progress', {done: done, total: todos.length})),
    ...todos.map(t => {
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

async function drawContext(a, box, grid, field) {
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
  // The panel it was drawn into may have been replaced while this was in flight. Checked on the
  // box that was passed in rather than looked up: on the stacked layout the card is legitimately
  // hidden behind the other tab, and looking it up would drop the context every time somebody was
  // reading the conversation.
  //
  // ⚠ Asked of the GRID, not of the box's children list. box.children.indexOf(...) was written
  // against the fake DOM, where children is an array — in a browser it is an HTMLCollection with no
  // indexOf, so this threw, the async function rejected with nobody awaiting it, and the whole
  // context block silently stopped rendering. The fake disagreed with the DOM in the direction that
  // makes a test pass and a page fail, which is the fourth time it has done that.
  if (!c || grid.parentNode !== box) return;

  // Which model, because the window below is that model's and a companion can be on one you did
  // not put it on — /route changes it mid-session and nothing else on this page would say so.
  if (c.model) grid.append(field('field.model', c.model));
  // Said once, where somebody would otherwise wonder why there is no cache figure at all.
  if (!c.cacheReported && !c.estimated) {
    grid.append(field('field.cache', tr('context.no_cache_report')));
  }

  const size = cell('v', '');
  size.append(document.createTextNode(
    (c.estimated ? '~' : '') + (c.used || 0).toLocaleString() +
    (c.window ? ' / ' + c.window.toLocaleString() : '') + ' tokens'));
  const note = document.createElement('small');
  // Said plainly, because the difference decides what the number is worth: one is the provider's
  // own count from the last turn, the other is arithmetic over the transcript.
  note.textContent = ' ' + tr(c.estimated ? 'context.estimated' : 'context.measured') +
                     (c.messages ? ' · ' + tr('context.messages', {n: c.messages}) : '');
  // What the backend served from its own prompt cache — and only when it said. A backend that
  // reports nothing about a cache is not a backend whose cache never hits, and drawing 0% for both
  // would report a working one as broken. Measured on the default local backend: it says nothing.
  if (c.cacheReported) {
    const share = c.used ? Math.round((c.cached || 0) * 100 / c.used) : 0;
    note.textContent += ' · ' + tr('context.cached_share', {pct: share});
  }
  size.append(note);
  const f = cell('f');
  f.append(cell('k', tr('field.context')), size);
  // The lever beside the reading. magi folds by itself when the window fills past its ratio; this
  // is for the case that rule does not cover — somebody who can see the run is about to need room
  // and would rather it happened now, between turns, than in the middle of the next one.
  const fold = document.createElement('md-text-button');
  fold.className = 'fold'; fold.textContent = tr('action.compact_now');
  tip(fold, tr('hint.compact'));
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
  grid.append(f);

  // A compaction is the one moment a companion silently stops knowing something. Four of them in
  // one session is the reason its earlier reasoning cannot be assumed still there.
  if (c.compactions) {
    const v = cell('v', c.compactions === 1 ? tr('context.fold')
                                       : tr('context.folds', {n: c.compactions}));
    const s2 = document.createElement('small');
    s2.textContent = ' · ' + tr('context.shed', {n: (c.shed || 0).toLocaleString()}) +
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
    grid.append(cf);
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
// What the organisation shares, and the two things a person does with it: find something, and
// write something down.
//
// # Why the search is here and not only in an agent's hands
//
// A companion can ask another what it knows (magi --mcp). A person had no such thing: the screen
// listed everything in whatever order the tiers came back in, and finding one rule among two
// hundred meant reading two hundred. The same IDF ranking the agents' search uses runs here, over
// the rows already fetched — no request, no round trip, and it narrows as you type.
//
// ⚠ Lexical, not semantic. The agents' search fuses in embeddings when a model is configured;
// this one cannot, because the vectors live where the store is and this page holds only what
// /skills returned. The heading says which model the machine is set up with, since that is the one
// thing a person managing shared knowledge has to keep the same across a team — vectors from two
// models are not comparable, and a search that quietly stopped matching is the symptom.
// One readout for one destination. Two halves load independently and each used to write the whole
// line, so whichever answered second erased the other's count — the shape of every readout built
// by two writers.
const shared = {rules: 0, facts: 0, crossing: 0, servers: null, reachedFrom: 0};
function sayShared() {
  state.className = '';
  const bits = [tr(shared.rules === 1 ? 'count.rule' : 'count.rules', {n: shared.rules}),
                tr('count.remembered', {n: shared.facts}),
                tr('count.crossing', {n: shared.crossing})];
  // Null until the servers have answered, which is not the same as none — a line that said "0
  // servers" while the request was in flight would be wrong for as long as it took.
  if (shared.servers !== null) {
    bits.push(tr(shared.servers === 1 ? 'count.server' : 'count.servers', {n: shared.servers}));
  }
  says(bits.join(' · '));
}

let skillQuery = '';
let mcpQuery = '';
// Which model this machine embeds with, from /console. Empty is a real answer, not a missing one.
let embedModel = '';

// The same ranking the agents' search uses, in the page's own words: rare shared words first.
//
// Ported rather than fetched. It is nine lines, it runs over rows already in hand, and asking the
// server would put a round trip between a keystroke and the list narrowing. ⚠ Two implementations
// of one formula is the shape this tree keeps finding defects in — pinned here by a test that
// checks this and internal/core/rank agree on the same corpus.
function rankByIDF(query, docs) {
  const toks = [...new Set(String(query).toLowerCase().split(/[^a-z0-9]+/).filter(w => w.length >= 3))];
  if (!toks.length || !docs.length) return docs.map((_, i) => i);
  const lower = docs.map(d => String(d).toLowerCase());
  const df = Object.fromEntries(toks.map(t => [t, lower.filter(d => d.includes(t)).length]));
  const n = docs.length;
  const hits = [];
  for (let i = 0; i < n; i++) {
    let score = 0, matched = 0;
    for (const t of toks) {
      if (lower[i].includes(t)) {
        score += Math.log(1 + (n - df[t] + 0.5) / (df[t] + 0.5));
        matched++;
      }
    }
    if (matched) hits.push({i: i, score: score, matched: matched});
  }
  hits.sort((a, b) => b.score - a.score || b.matched - a.matched || a.i - b.i);
  return hits.map(h => h.i);
}

// One find box, told which half it belongs to. Written once because two of them written twice is
// two that drift, and the only difference between the halves is where the typed text is kept.
// ── the page's one tooltip ───────────────────────────────────────────────────
// Hover OR focus, which is the half native title= never did: a keyboard user tabbing onto an
// icon-only button saw nothing at all. Placed above by default and flipped below when there is no
// room, 4dp off a control's edge. It leaves 1.5s after the pointer or focus goes, and there is only
// ever one — showing a second closes the first, because two tooltips is two answers to one
// question.
const tipEl = document.getElementById('tip');
let tipTimer = 0, tipHost = null;
function showTip(host) {
  const text = host.getAttribute('data-tip');
  if (!text) return;
  clearTimeout(tipTimer);
  tipTimer = 0;
  tipHost = host;
  tipEl.textContent = text;
  tipEl.hidden = false;
  const r = host.getBoundingClientRect(), t = tipEl.getBoundingClientRect();
  const above = r.top - t.height - 4;
  tipEl.style.top = (above >= 0 ? above : r.bottom + 4) + 'px';
  tipEl.style.left = Math.max(4, Math.min(r.left, innerWidth - t.width - 4)) + 'px';
}
function hideTip() {
  // Idempotent. This is called from pointerout AND from every pointermove that lands off the host,
  // and the first version cleared the pending timer before setting a new one — so moving the mouse
  // rewound the countdown on every frame and the tooltip never left until something else hid it.
  if (tipTimer) return;
  tipTimer = setTimeout(() => { tipEl.hidden = true; tipHost = null; tipTimer = 0; }, 1500);
}
// The tooltip outlived its button. This panel is redrawn on every fleet poll, so a control hovered
// at the wrong moment is REPLACED rather than left — and a node that is gone never fires pointerout,
// so nothing ever asked the tooltip to leave. Two guards: drop it the moment its host is off the
// document, and drop it when the pointer is somewhere else regardless of which events fired.
addEventListener('pointermove', e => {
  if (!tipHost) return;
  if (!tipHost.isConnected || !tipHost.contains(e.target)) hideTip();
}, true);
addEventListener('pointerdown', () => { if (tipHost) { clearTimeout(tipTimer); tipTimer = 0; tipEl.hidden = true; tipHost = null; } }, true);
for (const [on, fn] of [['pointerover', showTip], ['focusin', showTip], ['pointerout', hideTip], ['focusout', hideTip]]) {
  addEventListener(on, e => {
    const host = e.target.closest && e.target.closest('[data-tip]');
    if (host) fn(host); else if (fn === hideTip) hideTip();
  }, true);
}
// Set where title= used to be set. Both, for now: the native one still serves a pointer user on a
// browser that has not run the script yet.
// No title= alongside it. Setting both drew TWO tooltips on the same control — the browser's own,
// which no script can time or place, and this one. setAttribute rather than el.dataset because the
// render test's DOM has no dataset, and a tooltip is not worth teaching it one.
function tip(el, text) { el.setAttribute('data-tip', text); }

// Said to assistive tech, shown to nobody. The list visibly shrinks as you type, which is all the
// feedback a sighted user needs and none of it for anyone else.
const sayEl = document.getElementById('say');
let sayTimer = 0;
// The status line, with a way to read what got cut. It is nowrap-and-ellipsis at narrow widths,
// and the guide is explicit: do not cut text off without giving people a way to see it — a tooltip
// or a link is what it names. Now that this page draws its own tooltips, it can be the tooltip.
function says(text) {
  state.textContent = text;
  if (text) tip(state, text); else state.removeAttribute('data-tip');
}

function say(text) {
  clearTimeout(sayTimer);
  // Cleared first: repeating the same string into a live region is not a change, so the second
  // search that lands on the same count would be silent.
  sayEl.textContent = '';
  sayTimer = setTimeout(() => { sayEl.textContent = text; }, 60);
}

function findBox(get, set) {
  const box = cell('skfind');
  const f = document.createElement('md-outlined-text-field');
  f.setAttribute('label', tr('label.find'));
  f.value = get();
  f.addEventListener('input', () => set(f.value));
  box.append(f);
  return box;
}

// A heading over each half of the shared destination. Two lists under one tab need to say which is
// which, and the destination's own name is now the pair rather than either.
function sectionHead(key) {
  const h = document.createElement('h2');
  h.className = 'sectionhead';
  h.append(cell('', tr(key)));
  return h;
}

// The screen's own controls: find something, and write something down.
//
// Rebuilt on every load rather than kept, because the list behind it is — and a box whose value
// survived while the rows under it were replaced is a box that lies about what it is filtering.
// The typed text is held outside, in skillQuery, which is the part that must survive.
const skillFind = () => findBox(() => skillQuery, v => { skillQuery = v; loadSkills(); });
const mcpFind = () => findBox(() => mcpQuery, v => { mcpQuery = v; loadMCP(); });

// Writing goes UNDER what you have read, not over it.
//
// Above the list it was the first thing on a screen whose job is reading what the organisation
// shares — and the first four stops of a keyboard, which had to walk a form to reach the content.
// It also reads better in that order: somebody writes a rule down BECAUSE they just looked for one
// and it was not there.
function skillWrite(all) {
  const box = cell('skwrite');

  // Where it goes. Named tiers rather than "share this": the whole decision on this screen is how
  // far something reaches, and a control that hid it would be deciding on somebody's behalf.
  const where = document.createElement('md-outlined-select');
  where.setAttribute('label', tr('label.reaches'));
  const teams = [...new Set(all.filter(s => s.tier === 'team' && s.team).map(s => s.team))].sort();
  const opts = [['global', tr('reach.every_companion')],
                ...teams.map(t => ['team:' + t, tr('reach.team', {team: t})])];
  for (const [value, label] of opts) {
    const o = document.createElement('md-select-option');
    o.value = value;
    const h = document.createElement('div');
    h.setAttribute('slot', 'headline');
    h.textContent = label;
    o.append(h);
    where.append(o);
  }
  box.append(where);

  const note = document.createElement('md-outlined-text-field');
  note.setAttribute('label', tr('label.write_down'));
  note.setAttribute('type', 'textarea');
  note.setAttribute('rows', '1');
  box.append(note);

  const save = document.createElement('md-filled-button');
  save.textContent = tr('action.write_down');
  save.onclick = () => {
    const v = note.value.trim();
    if (!v) return;
    // The select resolves its value against the option it chose, and the options are custom
    // elements — so it is read here rather than held, and defaulted rather than guessed.
    const pick = where.value || 'global';
    const body = new URLSearchParams({text: v, tier: pick.startsWith('team:') ? 'team' : 'global'});
    if (pick.startsWith('team:')) body.set('team', pick.slice(5));
    post('/remember', body).then(() => { note.value = ''; loadSkills(); });
  };
  box.append(save);

  // Which model the searches on this machine are built on. It belongs on this screen because it is
  // the one setting a person managing shared knowledge has to keep the same across a team: vectors
  // from two models are not comparable, and the symptom is a search that quietly stops matching.
  const model = cell('skmodel');
  model.textContent = tr(embedModel ? 'embed.model' : 'embed.none', {model: embedModel});
  box.append(model);
  return box;
}

async function loadSkills() {
  const list = await fetchList('/skills');
  if (!list) return;
  const crossing = list.filter(s => s.tier === 'global').length;
  const rules = list.filter(s => s.kind !== 'memory').length;
  state.className = '';
  shared.rules = rules;
  shared.facts = list.length - rules;
  shared.crossing = crossing;
  sayShared();
  if (!list.length) {
    skillsEl.replaceChildren(sectionHead('nav.lessons'),
      emptyState('empty.nothing_learned', 'empty.nothing_learned_how'), skillWrite(list));
    return;
  }
  // Ranked, not filtered on a substring: "cache" should find the rule about prompt caching before
  // the one that merely mentions caches in passing, and a substring match cannot order anything.
  let shown = list;
  if (skillQuery.trim()) {
    const docs = list.map(sk => [sk.description, sk.name, sk.body, sk.source].filter(Boolean).join(' '));
    const order = rankByIDF(skillQuery, docs);
    shown = order.map(i => list[i]);
  }
  if (!shown.length) {
    skillsEl.replaceChildren(sectionHead('nav.lessons'), skillFind(),
      emptyState('empty.no_match', 'empty.no_match_how'), skillWrite(list));
    return;
  }
  if (skillQuery) say(tr('find.results', {n: shown.length}));
  skillsEl.replaceChildren(sectionHead('nav.lessons'), skillFind(), ...shown.map(sk => {
    const el = cell('sk ' + sk.tier + (sk.kind === 'memory' ? ' fact' : ''));
    const top = cell('top');
    top.append(cell('tier',
      (sk.tier === 'global' ? tr('reach.every_companion')
       : sk.tier === 'team' ? tr('reach.team', {team: sk.team})
       : tr('reach.only', {name: sk.companion})) +
      (sk.peer ? tr('reach.on_peer', {peer: sk.peer}) : '')));
    top.append(cell('what', sk.description || sk.name));
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    tip(drop, tr('hint.forget'));
    arm(drop, tr('action.forget'), () => {
      // A rule on another console is forgotten THERE. The socket is that machine's path and the
      // peer name is how this one knows which machine to ask; a global rule has no socket and the
      // peer name alone routes it.
      const body = new URLSearchParams({name: sk.name, tier: sk.tier});
      if (sk.tier === 'team') body.set('team', sk.team);
      post('/forget', body,
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
  }), skillWrite(list));
}

// ── what they can reach ──────────────────────────────────────────────────────
// The MCP servers each companion has, and the form to add one. Not polled: a config file does not
// change while you are looking at it, and this page is read to decide something.
async function loadMCP() {
  const list = await fetchList('/mcp');
  if (!list) return;
  const reach = new Set(list.map(s => s.companion || 'every companion here'));
  state.className = '';
  shared.servers = list.length;
  sayShared();
  shared.reachedFrom = reach.size;

  // Same search, the other half. Ten servers is a screen and a half, and the half below the fold is
  // the half nobody scrolls to — the experience list got a find the moment it had four rows and
  // this one was left to grow without.
  let show = list;
  if (mcpQuery.trim()) {
    const docs = list.map(sv => [sv.name, sv.command, (sv.args || []).join(' '), sv.url,
                                 sv.companion, (sv.env || []).join(' ')].filter(Boolean).join(' '));
    show = rankByIDF(mcpQuery, docs).map(i => list[i]);
  }

  const rows = show.map(sv => {
    const el = cell('srv ' + sv.tier);
    const top = cell('top');
    // ⚠ Hardcoded English, not from the pack — so it missed the sentence-case pass and it does not
    // translate. Cased here to match the rest; the missing translation is recorded separately.
    top.append(cell('tier', sv.tier === 'global' ? 'Every companion here' : 'Only ' + sv.companion));
    top.append(cell('what', sv.name));
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    tip(drop, tr('hint.remove_server', {file: sv.file}));
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
    mcpEl.replaceChildren(sectionHead('nav.connections'), emptyState('empty.no_servers', 'empty.no_servers_how'), form);
    return;
  }
  if (!rows.length) {
    mcpEl.replaceChildren(sectionHead('nav.connections'), mcpFind(),
      emptyState('empty.no_match', 'empty.no_match_how'), form);
    return;
  }
  mcpEl.replaceChildren(sectionHead('nav.connections'), mcpFind(), ...rows, form);
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

let es, fleetTimer, boardSub;
function connect() {
  es = new EventSource('/events' + q());
  es.onopen = () => { state.className = 'live'; says(tr('state.live')); };
  es.onmessage = e => draw(JSON.parse(e.data));
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { state.className = 'lost'; says(tr('state.reconnecting'));
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
  tabSkills.textContent = tr('nav.shared');
  document.getElementById('ptabTalk').textContent = tr('panel.talk');
  document.getElementById('ptabState').textContent = tr('panel.state');
  // label, not placeholder. Material Web floats the LABEL into the outline's notch when the field
  // takes focus or holds a value; a placeholder is the grey hint inside an empty one and never
  // moves. Written as placeholders here, the fields had no notch and nothing to float — which is
  // what "the placeholder looks wrong" was. The longer sentence becomes supporting text, which is
  // where an explanation belongs and where it does not have to fit in a notch.
  // Through answerMode, so a language change does not quietly turn the answer field back into the
  // request field while the agent is still waiting on the question above it.
  answerMode(answering);
  document.getElementById('stop').textContent = tr('action.interrupt');
  railMenu.setAttribute('aria-label', tr('nav.menu'));
  // A secondary tab's indicator spans the tab; a primary tab's hugs its label. The bundle keeps
  // that as a reactive @state with no attribute behind it, so it is set as a property — assigning
  // it re-renders the tab with the indicator on the button instead of on the content.
  for (const id of ['ptabTalk', 'ptabState']) document.getElementById(id).fullWidthIndicator = true;
  // The waiting badge changes parent with the rail, per the spec: on the icon while collapsed,
  // beside the label once there is one.
  sideToggle.setAttribute('aria-label', tr(document.body.getAttribute('side') === 'shut' ? 'side.show' : 'side.hide'));
  tip(sideToggle, tr(document.body.getAttribute('side') === 'shut' ? 'side.show' : 'side.hide'));
  placeRailBadge();
  // Two navigation landmarks on one page have to be told apart, and the label must not repeat the
  // role — a screen reader already says "navigation". Named one at a time rather than swept with a
  // selector: the phrase pack's own test reads literal tr('…') calls to find phrases nobody asks
  // for, and a lookup through a data attribute is invisible to it.
  railEl.setAttribute('aria-label', tr('nav.destinations'));
  document.getElementById('crumbs').setAttribute('aria-label', tr('nav.where'));
  themeToggle.setAttribute('aria-label', tr('pref.theme'));
  prefsEl.setAttribute('aria-label', tr('nav.preferences'));
  prefsClose.textContent = tr('action.close');
  prefsK.textContent = tr('nav.preferences');
  consoleK.textContent = tr('nav.this_console');
  for (const [el, key] of [[railFleet, 'nav.companions'], [railSkills, 'nav.shared']]) {
    // The word is on the item whether or not it is drawn: collapsed, the icon is all there is to
    // see, and a rail nobody can read aloud is not a navigation. The icon itself is markup and is
    // not touched here — a shape does not need translating, and rebuilding it on every language
    // change would throw away four elements to replace them with the same four.
    el.setAttribute('aria-label', tr(key));
    el.querySelector('.lbl').textContent = tr(key);
  }
  paintChoice(langEl, 'lang');
  if (consoleEl.children.length) loadConsole();   // its two labels are words too
  paintNotify();

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
  if (view() === 'skills') { loadSkills(); loadMCP(); }

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

// reveal restarts an entrance animation on an element that has just become visible.
//
// The class has to come OFF and the layout be read before it goes back on: an animation already
// running does not restart because the class is set again, so without the reflow the second visit
// to a destination arrives with no motion at all. Reading offsetWidth is what forces it.
function reveal(el, how) {
  if (!el || el.hidden) return;
  el.classList.remove('enter', 'rise', 'slideL', 'slideR');
  void el.offsetWidth;
  el.classList.add(how || 'enter');
}

function render() {
  if (es) { es.close(); es = null; }
  if (fleetTimer) { clearInterval(fleetTimer); fleetTimer = null; }
  if (boardSub) { boardSub.unsubscribe(); boardSub = null; }
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
  // A companion's page is INSIDE the companions destination, so that is the one that stays lit.
  // Marked by view alone it went dark the moment you opened a row, and the rail then said you were
  // nowhere — on the screen you reach it from most often.
  for (const [el, key] of RAILS) el.toggleAttribute('selected', s ? key === 'fleet' : v === key);
  fleetEl.hidden = !!s || v !== 'fleet';
  summaryEl.hidden = !!s || v !== 'fleet';
  skillsEl.hidden = !!s || v !== 'skills';
  boardEl.hidden = !!s || v !== 'board';
  mcpEl.hidden = !!s || v !== 'skills';
  // Only on a companion's own page. Addressing one by typing its name into a box, from a list where
  // it is already on screen and one click away, is a second way to do the thing the list does — and
  // the harder one: it asks somebody to spell a name they can see.
  f.hidden = !s;
  document.getElementById('stop').hidden = !s; // nothing to interrupt from the fleet view
  // Leaving a companion resets the panel: the next one is arrived at for its conversation, and
  // landing on the facts of an agent you just opened is a screen nobody asked for.
  if (!s) panel = 'talk';
  drawPanels();
  document.getElementById('handoffs').hidden = true;
  historyEl.hidden = true;
  intervenedEl.hidden = true;
  document.getElementById('plan').hidden = true;
  document.getElementById('prompt').hidden = true;
  sidEl.textContent = '';
  // Whichever body of content this navigation arrived at. One of them, not all of them: reveal on a
  // hidden element does nothing, so the list is the page's destinations and the right one answers.
  for (const el of [fleetEl, skillsEl, boardEl, mcpEl, streamEl]) reveal(el);
  measureDock();
  if (s) { draw([]); connect(); }
  else { state.className = ''; says(''); }
  if (v === 'board') {
    // Live, like the fleet beside it. A board that showed the day as it stood when you opened it
    // went stale the moment an agent finished something — and the day you watch it is the day work
    // is happening. rxjs because the page already speaks it, and because the guard belongs in the
    // pipe rather than in a flag somebody has to remember to clear.
    loadBoard();
    boardSub = timer(3000, 3000).pipe(
      switchMap(() => from(boardSig())),
      onlyWhen(Boolean),
      distinctUntilChanged(),
      // A field with the caret in it is a field somebody is using. Rebuilding the header under
      // them would take the focus and the half-typed date with it.
      onlyWhen(() => !boardEl.contains(document.activeElement)),
    ).subscribe(() => loadBoard());
    return;
  }
  if (v === 'skills') {
    // Both halves of the same story, in the order it happens: what has been said often enough to
    // become a rule, then the rules. Not polled — this is read and thought about, and a list that
    // reorders itself under the cursor while somebody decides what to promote is worse than one a
    // minute old.
    //
    // BOTH halves, from here. There used to be a separate v === 'mcp' branch above this one, and
    // it could not run: view() folds mcp into skills (RENAMED), so the test never matched while the
    // element beside it was shown by the same fold. The servers arrived only when something else
    // happened to call loadMCP — a language change, or adding one — which is why the list was there
    // on one visit and empty on the next.
    loadSkills();
    // The server picker names companions, so the fleet is read before the list is drawn.
    fetchList('/fleet').then(list => { if (list) fleetSeen = list; loadMCP(); });
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
const RAILS = [[railFleet, 'fleet'], [railSkills, 'skills']];
for (const [el, key] of RAILS) {
  el.href = at(HREF[key]);
  el.onclick = e => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button) return;  // let the browser have it
    e.preventDefault();
    // Pressing the destination you are already on scrolls back to the top, which is what the guide
    // asks a re-selected destination to do. Without it the press did nothing at all — the url was
    // already this one — and a control that answers nothing reads as broken.
    if (!sock() && view() === key) { scrollTo({top: 0, behavior: 'smooth'}); return; }
    history.pushState({}, '', at(HREF[key]));
    render();
  };
}

// Widening the rail is a wide-screen idea only. On a phone the rail is not a drawer — it is a
// section at the foot of the page — so there is nothing to open and nothing to close.
// The side pane's own control. Remembered, because a pane you shut should stay shut when you open
// the next companion — reopening it every time would make the button feel like it did nothing.
const sideToggle = document.getElementById('sideToggle');
if (localStorage.getItem('side') === 'shut') document.body.setAttribute('side', 'shut');
sideToggle.onclick = () => {
  const shut = document.body.getAttribute('side') !== 'shut';
  document.body.setAttribute('side', shut ? 'shut' : '');
  localStorage.setItem('side', shut ? 'shut' : '');
  sideToggle.setAttribute('aria-expanded', String(!shut));
  paint();
};

const scrimEl = document.getElementById('scrim');
// Collapsed the badge sits on the icon's upper right; expanded it moves beside the label, which
// is where the spec puts it once there is a label to sit beside. It is a different PARENT, not a
// different offset — and it has to be said from wherever the rail changes width, not only from
// paint(): paint does not run on a nav toggle, so the reparent never happened and a stylesheet
// rule was quietly doing the work with a calc(100% + 9.2rem) nobody could derive. One mechanism.
function placeRailBadge() {
  const open = document.body.getAttribute('nav') === 'open';
  const home = open ? railFleet : railFleet.querySelector('.icwrap');
  // A list item lays its slotted children out itself, so which SLOT is the whole of where the
  // badge lands: appended without one it goes to the default slot and stands wherever the label
  // stops. A margin-left:auto next to it did nothing, because there is no flex line of ours for
  // it to push along.
  if (open) railBadge.slot = 'end'; else railBadge.removeAttribute('slot');
  if (home && railBadge.parentNode !== home) home.append(railBadge);
}
const closeNav = () => {
  document.body.removeAttribute('nav');
  railMenu.setAttribute('aria-expanded', 'false');
  placeRailBadge();
};
scrimEl.onclick = closeNav;
railMenu.onclick = () => {
  if (document.body.getAttribute('nav') === 'open') { closeNav(); return; }
  document.body.setAttribute('nav', 'open');
  railMenu.setAttribute('aria-expanded', 'true');
  placeRailBadge();
};
// One door to the preferences, at every width. The rail's hamburger is a different thing: it
// widens the navigation, and it no longer opens anything.
prefsEl.onclick = () => prefsDialog.show();
// Painted when it OPENS, not before. A dialog does not render what is slotted into it until then,
// so a select told its value while the dialog was closed had no options to resolve it against and
// showed an empty field over a value it was holding.
prefsDialog.addEventListener('opened', () => { if (painted) paint(); paintNotify(); });
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

for (const [el, key] of [[tabFleet, 'fleet'], [tabSkills, 'skills']]) {
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
  if (!r.ok) { state.className = 'lost'; says((await r.text()).trim().slice(0, 80)); }
}

const t = document.getElementById('t');
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
  if (answering) {
    const a = answering;
    t.value = ''; grow();
    post('/answer', new URLSearchParams({call: a.askId, kind: a.askKind, text: v}), a.socket, a.peer)
      .then(loadFleet);
    return;
  }
  // The composer is only on a companion's page, so there is one place the work can go.
  t.value = ''; grow(); post('/submit', new URLSearchParams({text: v}));
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
