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

## material.js — Material Web 2.5.0, 242KB

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
    import '@material/web/button/text-button.js';
    import '@material/web/textfield/outlined-text-field.js';
    import '@material/web/select/outlined-select.js';
    import '@material/web/select/select-option.js';
    import '@material/web/labs/card/outlined-card.js';
    import '@material/web/iconbutton/icon-button.js';
    import '@material/web/list/list.js';
    import '@material/web/list/list-item.js';
    import '@material/web/chips/chip-set.js';
    import '@material/web/chips/filter-chip.js';
    import '@material/web/tabs/tabs.js';
    import '@material/web/tabs/primary-tab.js';
    ENTRY
    npx esbuild@0.25 entry.mjs --bundle --format=esm --minify --outfile=material.js
    #   sha256 19cf9f14745754689a593f78a306132365527e9182a27c81eb38da631d525e75

`labs/card` is the one import from the library's unstable half, taken deliberately: a card there is
a container and nothing more — elevation, a background, a slot and an outline, with no ripple, no
focus ring and no role — so an unstable API here can only change how a box is drawn. The interactive
rows are NOT cards for the same reason: they are links, and this component would take the link away.

Licence: Apache-2.0, same as this repository.
