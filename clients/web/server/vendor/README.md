# vendor

Third-party JavaScript, **built once and committed**, served from this binary like the fonts.

Not fetched at runtime and not built by CI: a CDN would make this page's behaviour depend on
somebody else's machine and tell that machine when you look at your agents, and an npm step in a Go
repository is a second toolchain for a file that changes twice a year.

## rxjs.js — RxJS 7.8.2, 28.7KB

    npm pack rxjs@7                     # rxjs-7.8.2.tgz
    #   sha256 2312f8ffd9726ffd7bd53ea12c5f13663d09a3dc3326f448c70b88f5ef6fac82
    npm pack tslib@2                    # rxjs imports it
    mkdir -p node_modules && tar xzf rxjs-7.8.2.tgz && mv package node_modules/rxjs
    tar xzf tslib-*.tgz && mv package node_modules/tslib
    cat > entry.mjs <<'ENTRY'
    export { BehaviorSubject, Subject, of, from, timer, fromEvent, EMPTY, firstValueFrom,
             map, switchMap, catchError, distinctUntilChanged, shareReplay, startWith,
             filter, takeUntil, tap, retry, debounceTime } from 'rxjs';
    ENTRY
    npx esbuild@0.25 entry.mjs --bundle --format=esm --minify --outfile=rxjs.js
    #   sha256 0eb0ef7ed1d58650de6576f1401bea13366cd5e0619f1c8111123b953befe306
    # Without debounceTime the same commands rebuild the previous file, 45fd4c08…, byte for byte.
    # The console's typing pauses (composer suggestion, editor completion and look-over) use it
    # through dev.sayaya.rx; a binding whose operator is missing here fails at mount, not at compile.

Only the operators the console uses are exported, so the bundle carries what runs and nothing else.
Re-running those commands on the same versions reproduces the file — which is the point of writing
them down rather than saying "built with esbuild".

Licence: Apache-2.0 (RxJS), same as this repository.

## material.js — Material Web 2.5.0, 330KB

The M3 components themselves, so the design comes from the system rather than from CSS written here
a second time. Only the ones the page uses are imported: `all.js` would register every component
the library ships.

    npm pack @material/web@2            # material-web-2.5.0.tgz
    #   sha256 d2974cfab7e8249774c39d9cab22cd24cae9204a74c62b7cb40106c7bac798ed
    npm pack lit@3 @lit/context@1 @lit/reactive-element@2 lit-html@3 lit-element@4 tslib@2
    # unpack each into node_modules/<name> (scoped ones under node_modules/@lit/…)
    cat > entry.mjs <<'ENTRY'
    import '@material/web/button/filled-button.js';
    import '@material/web/button/filled-tonal-button.js';
    import '@material/web/button/outlined-button.js';
    import '@material/web/button/text-button.js';
    import '@material/web/textfield/outlined-text-field.js';
    import '@material/web/select/outlined-select.js';
    import '@material/web/select/select-option.js';
    import '@material/web/labs/card/outlined-card.js';
    import '@material/web/labs/badge/badge.js';
    import '@material/web/dialog/dialog.js';
    import '@material/web/iconbutton/icon-button.js';
    import '@material/web/list/list.js';
    import '@material/web/list/list-item.js';
    import '@material/web/menu/menu.js';
    import '@material/web/menu/menu-item.js';
    import '@material/web/chips/chip-set.js';
    import '@material/web/chips/filter-chip.js';
    import '@material/web/chips/input-chip.js';
    import '@material/web/switch/switch.js';
    import '@material/web/tabs/tabs.js';
    import '@material/web/tabs/primary-tab.js';
    import '@material/web/tabs/secondary-tab.js';
    import '@material/web/progress/linear-progress.js';
    ENTRY
    npx esbuild@0.25 entry.mjs --bundle --format=esm --minify --outfile=material.js
    #   sha256 d19b5d7dddf57b52da0e68efbe5d5af7da74ddd47b82dab7dfa8c4baf81ad469
    # Verified reproducible at every step: removing the imports added in a change rebuilds the
    # bundle this repository carried before it, byte for byte. secondary-tab and input-chip put it
    # back to e3fa47ae…; the two menu imports put it back to bba391a2…. So the size each time grew
    # by exactly the components named and nothing else.

`labs/card` and `labs/badge` are the two imports from the library's unstable half, taken for the
same reason: a badge is a number in a shape and a card there is a container and nothing more — elevation, a background, a slot and an outline, with no ripple, no
focus ring and no role — so an unstable API here can only change how a box is drawn. The interactive
rows are NOT cards for the same reason: they are links, and this component would take the link away.

Licence: Apache-2.0, same as this repository.

## marked.js — marked 18.1.1, 45.4KB, **lexer only**

    npm pack marked@18.1.1              # marked-18.1.1.tgz
    mkdir -p node_modules && tar xzf marked-18.1.1.tgz && mv package node_modules/marked
    cat > entry.mjs <<'ENTRY'
    export { lexer } from 'marked';
    ENTRY
    npx esbuild@0.25.12 entry.mjs --bundle --format=esm --minify --outfile=marked.js
    #   sha256 6cb3abdef30d09fe576be18224b81ab8ac703fd50cb22c3b3482f0f2396998d4

Only `lexer` is exported, and that is the whole point rather than a size optimisation.

The transcript is arbitrary output from a model and from tools. marked's HTML generation would turn
that into a string this page then has to trust, and the usual answer — a sanitiser — makes safety
depend on the sanitiser being right about every case. Taking tokens instead and building DOM nodes
from them means no HTML is ever produced from that output, so there is nothing to sanitise and no
case for a sanitiser to be wrong about.

One token type carries the hazard across: markdown allows raw HTML, and the lexer reports it as a
token of type "html" with the source in `raw`. The renderer draws that as TEXT. A tool result
containing an img tag with an onerror handler appears in the transcript as those characters, which
is what somebody reading a transcript wants to see anyway.

Licence: MIT.

## sayaya-feedback.js — sayaya-ui 2.5.0.5 feedback bundle, 35.4KB

The shell's tooltip (`md-tooltip`, plus `md-snackbar` and `md-toast`), from the same library the
console's Java side already depends on (`dev.sayaya:ui`). Not built here: it is the release's own
standalone ES module, which defines only those three names and skips any name that is already
registered, so it sits after `material.js` without redefining an `md-*` from it.

    gh release download v2.5.0.5 -R sayaya1090/sayaya-ui -p 'sayaya-ui-feedback-2.5.0.5.js*'
    shasum -a 256 -c sayaya-ui-feedback-2.5.0.5.js.sha256
    #   sha256 c9354aefaa0b6b378a32c4c10fbd24ceb74846457a3c013fb2d6c0d8c119d69d
    mv sayaya-ui-feedback-2.5.0.5.js sayaya-feedback.js

The name carries no version, like the bundles above; this section does. Bump it together with
`sayaya-ui` in `clients/web/ui/gradle/libs.versions.toml`, so the Java wrapper and the element it
wraps come from one release.

Licence: Apache-2.0 (sayaya-ui, as its published POM declares), same as this repository.
