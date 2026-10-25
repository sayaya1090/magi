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

## sayaya-ui.js — sayaya-ui 2.5.0.6 standalone bundle, 671KB

The M3 components the console draws with: Material Web 2.5.0's `all.js` and sayaya-ui's own labs
(`md-tooltip`, `md-navigation-rail`, …) in one IIFE classic script. Not built here — it is the
release's own attachment, from the same library the console's Java side already depends on
(`dev.sayaya:ui` in `clients/web/ui/gradle/libs.versions.toml`), so the Java wrapper and the element
it wraps come from one release.

    gh release download v2.5.0.6 -R sayaya1090/sayaya-ui -p 'sayaya-ui-2.5.0.6.js*'
    shasum -a 256 -c sayaya-ui-2.5.0.6.js.sha256
    #   sha256 b3e31f921037899d7e586044ac22eb1d0ba1c25a0568cb8ff7efae1d59861174
    mv sayaya-ui-2.5.0.6.js sayaya-ui.js

It replaced a `material.js` that was built here from only the components the page imported
(310KB, 60KB gzip — with the tooltip bundle beside it, 71KB gzip in all). This one is 122KB gzip:
`all.js` registers every Material Web component. The price buys one bundle instead of two, and the
sayaya-ui navigation rail, which shipped only in this form. Both cannot be loaded together: this
bundle defines the same `md-*` names, and a second `customElements.define` of a name throws.

It is a classic script, not a module: it writes to the top-level `this`, which a module does not
have. Pages load it with `<script src>` ahead of their module script, which then still runs before
the GWT bootstrap it appends.

Licence: Apache-2.0 (sayaya-ui, Material Web), BSD-3-Clause (Lit), as the bundle's header states.

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
