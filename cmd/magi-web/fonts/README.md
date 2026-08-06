# Newsreader, embedded

The display face this page sets its headlines, names and lead lines in. Newsreader was drawn for
reading on screens — an editorial serif with real italics — which is what the layout wanted and what
no system font on every platform can be counted on to be.

Embedded rather than fetched. A viewer that reached out to fonts.gstatic.com would make this page's
appearance depend on a machine that is not yours, tell that machine when you look at your agents,
and render in a fallback on a laptop with no route out — and the whole point of this binary is that
it holds everything it serves. The routes under /font/ are served by the same process; nothing here
leaves the machine.

Three faces, subset to Latin plus the punctuation the UI uses, and instanced off the variable font
at fixed weights: 400 and 600 roman, 400 italic. 60KB in total. Text in other scripts (a Korean
workspace name, a prompt in any language) falls through to the system serif behind it in the stack,
which is the honest outcome — a Hangul-capable serif is megabytes and this is a control panel.

Rebuilt with:

    curl 'https://fonts.googleapis.com/css2?family=Newsreader:ital,wght@0,400;0,600;1,400'   # latin URLs
    python3 -m fontTools.varLib.instancer roman.woff2 wght=400 --output r400.ttf
    python3 -m fontTools.subset r400.ttf --unicodes=U+0020-007E,U+00A0-00FF,U+2010-2015,U+2018-201D,\
U+2022,U+2026,U+00B7,U+2190,U+23F8,U+2713,U+2192 --layout-features=kern,liga \
--flavor=woff2 --output-file=newsreader-400.woff2

Licensed under the SIL Open Font License 1.1 — OFL.txt, which the licence requires to travel with
the fonts and which is why it is in this directory rather than in a footnote.
