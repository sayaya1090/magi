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
