# vendor

Third-party JavaScript, **built once and committed**, served from this binary like the fonts.

Not fetched at runtime and not built by CI: a CDN would make this page's behaviour depend on
somebody else's machine and tell that machine when you look at your agents, and an npm step in a Go
repository is a second toolchain for a file that changes twice a year.

## rxjs.js — RxJS 7.8.2, 28.3KB

    npm pack rxjs@7                     # rxjs-7.8.2.tgz
    #   sha256 2312f8ffd9726ffd7bd53ea12c5f13663d09a3dc3326f448c70b88f5ef6fac82
    npm pack tslib@2                    # rxjs imports it
    mkdir -p node_modules && tar xzf rxjs-7.8.2.tgz && mv package node_modules/rxjs
    tar xzf tslib-*.tgz && mv package node_modules/tslib
    cat > entry.mjs <<'ENTRY'
    export { BehaviorSubject, Subject, of, from, timer, fromEvent, EMPTY, firstValueFrom,
             map, switchMap, catchError, distinctUntilChanged, shareReplay, startWith,
             filter, takeUntil, tap, retry } from 'rxjs';
    ENTRY
    npx esbuild@0.25 entry.mjs --bundle --format=esm --minify --outfile=rxjs.js
    #   sha256 45fd4c0873ad1ac17ff905ccd6876a62c3c422d2890bee8737f6783fed5ded49

Only the operators the console uses are exported, so the bundle carries what runs and nothing else.
Re-running those commands on the same versions reproduces the file — which is the point of writing
them down rather than saying "built with esbuild".

Licence: Apache-2.0 (RxJS), same as this repository.

## material.js — Material Web 2.5.0, 285KB

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
    import '@material/web/chips/chip-set.js';
    import '@material/web/chips/filter-chip.js';
    import '@material/web/switch/switch.js';
    import '@material/web/tabs/tabs.js';
    import '@material/web/tabs/primary-tab.js';
    import '@material/web/progress/linear-progress.js';
    ENTRY
    npx esbuild@0.25 entry.mjs --bundle --format=esm --minify --outfile=material.js
    #   sha256 e3fa47aec0bbc26979b94be1ef37db46f11fe955b5606a65dc12e95040bd0722

`labs/card` and `labs/badge` are the two imports from the library's unstable half, taken for the
same reason: a badge is a number in a shape and a card there is a container and nothing more — elevation, a background, a slot and an outline, with no ripple, no
focus ring and no role — so an unstable API here can only change how a box is drawn. The interactive
rows are NOT cards for the same reason: they are links, and this component would take the link away.

Licence: Apache-2.0, same as this repository.

## marked.js — marked 18.0.9, 41.3KB, **lexer only**

    npm pack marked@18                  # marked-18.0.9.tgz
    #   sha256 3017275f02c3bb33d668a892566f47c129da751292f29cfcaf45bded787d0dc6
    mkdir -p node_modules && tar xzf marked-18.0.9.tgz && mv package node_modules/marked
    cat > entry.mjs <<'ENTRY'
    export { lexer } from 'marked';
    ENTRY
    npx esbuild@0.25 entry.mjs --bundle --format=esm --minify --outfile=marked.js
    #   sha256 991cb97e6124dd65bec8acc26d85a4f7d23557779e0ec8b12d351565e8325c3a

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
