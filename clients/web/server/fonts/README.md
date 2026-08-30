# The faces this page is set in, embedded

Two families, both embedded and served from this binary under `/font/`.

**Newsreader** — the display face: headlines, names, lead lines. Drawn for reading on screens, an
editorial serif with real italics, which is what the layout wanted and what no system font on every
platform can be counted on to be. Three faces, subset to Latin plus the punctuation the UI uses and
instanced off the variable font at fixed weights: 400 and 600 roman, 400 italic. 60KB in total.

**Noto Serif KR** — Korean, in the same two weights. 3.7MB, hangul and hanja.

## Why Noto Serif KR and not another 명조

Chosen by looking rather than by reputation, set beside Newsreader at the sizes this page uses:

- **Nanum Myeongjo** — its hangul sits about a size smaller than Newsreader's Latin, so a mixed
  heading reads as two typefaces at two sizes.
- **Gowun Batang** — larger and considerably lighter; the hangul overpowers the Latin in width while
  being paler in colour. Elegant on its own, mismatched here.
- **Noto Serif KR** — matches Newsreader's cap height and its colour closely enough that a mixed
  line reads as one face. It also carries hanja, so a name or a quotation with 漢字 in it does not
  drop out of the face halfway through a word.

## Why unicode-range and not a language setting

The Korean faces are attached by the characters they cover, not by which language the console is
set to. The browser fetches them only when Korean is actually on the page, and a Korean workspace
name on an English console still renders in the right face — which a per-locale switch gets wrong
in exactly the case that matters.

## Why embedded

A viewer that reached out to fonts.gstatic.com would make this page's appearance depend on a machine
that is not yours, tell that machine when you look at your agents, and render in a fallback on a
laptop with no route out. The whole point of this binary is that it holds everything it serves.

Korean sits in the monospace stack as well as the display one. Hangul is not monospaced by anybody,
so it was falling through to whatever the platform happened to install — a different page on every
machine, which is the thing embedding a face exists to stop.

## Rebuilt with

    # Newsreader: latin subset off the variable font
    curl 'https://fonts.googleapis.com/css2?family=Newsreader:ital,wght@0,400;0,600;1,400'
    python3 -m fontTools.varLib.instancer roman.woff2 wght=400 --output r400.ttf
    python3 -m fontTools.subset r400.ttf --unicodes=U+0020-007E,U+00A0-00FF,U+2010-2015,U+2018-201D,\
U+2022,U+2026,U+00B7,U+2190,U+23F8,U+2713,U+2192 --layout-features=kern,liga \
--flavor=woff2 --output-file=newsreader-400.woff2

    # Noto Serif KR: instance the weight, then subset to Korean only — the Latin comes from
    # Newsreader, and shipping a second copy of it would be bytes the unicode-range never reaches.
    python3 -m fontTools.varLib.instancer 'NotoSerifKR[wght].ttf' wght=400 --output NSKR-400.ttf
    python3 -m fontTools.subset NSKR-400.ttf --text-file=korean.txt \
--unicodes=U+1100-11FF,U+3130-318F,U+A960-A97F,U+D7B0-D7FF,U+3000-303F,U+FF01-FF60,U+FFE0-FFE6,\
U+2027,U+00B7,U+2018-201D,U+2026 --layout-features=kern,liga --flavor=woff2 \
--output-file=notoserifkr-400.woff2

`korean.txt` is every hangul syllable (U+AC00–U+D7A3) plus the KS X 1001 hanja — the set a Korean
keyboard can produce, rather than the whole CJK block.

Both families are licensed under the SIL Open Font License 1.1 — `OFL.txt` (Newsreader) and
`OFL-NotoSerifKR.txt`, which the licence requires to travel with the fonts and which is why they
are in this directory rather than in a footnote.
