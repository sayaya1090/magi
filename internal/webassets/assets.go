// The files that make this console installable — one copy, for whichever console serves it.
//
// They were constants inside the old console's package, so the new one could not serve them
// without a second copy of the same bytes: a manifest that drifts is a phone that opens a
// differently-named app, and a service worker that drifts is a notification that arrives in the
// wrong shape. The old console is being replaced; these outlive it here.
package webassets

// Sprite is the block of <symbol> definitions the console draws its icons from, or empty.
//
// Empty is a supported state, not a failure: the art is Font Awesome Pro, whose licence lets a
// build use it without republishing it as files, so it is baked in at build time (gen_icons.go)
// and absent from any build with no licence to hand. Those builds draw the shapes the screens
// have always drawn — every icon call has a glyph behind it.
var Sprite = ""

// Manifest is what a phone reads when somebody adds this console to their home screen.
const Manifest = `{
  "name": "magi",
  "short_name": "magi",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#14110d",
  "theme_color": "#14110d",
  "icons": [
    {"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any"},
    {"src": "/icon-maskable.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "maskable"}
  ]
}`

// Icon is the console's mark, drawn rather than fetched (this page calls no other server).
const Icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192">
  <circle cx="96" cy="70" r="21" fill="#FFB454"/>
  <circle cx="70" cy="115" r="21" fill="#5CD8E6"/>
  <circle cx="122" cy="115" r="21" fill="#FF8A8A"/>
  <circle cx="96" cy="97" r="43" fill="none" stroke="#FF7A1A" stroke-width="4" opacity=".55"/>
</svg>`

// IconMaskable is the same mark inside the safe area Android crops to.
const IconMaskable = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192">
  <rect width="192" height="192" fill="#211B14"/>
  <circle cx="96" cy="70" r="21" fill="#FFB454"/>
  <circle cx="70" cy="115" r="21" fill="#5CD8E6"/>
  <circle cx="122" cy="115" r="21" fill="#FF8A8A"/>
  <circle cx="96" cy="97" r="43" fill="none" stroke="#FF7A1A" stroke-width="4" opacity=".55"/>
</svg>`

// ServiceWorker receives web push and opens the console where the notice points.
const ServiceWorker = `// magi's service worker. It exists to receive notifications; it caches nothing.
//
// Caching would make the console show a stale fleet, and a stale fleet is worse than no fleet: the
// whole page is an answer to "what is happening right now".
self.addEventListener('push', e => {
  let m = {};
  try { m = e.data ? e.data.json() : {}; } catch (_) { m = {body: e.data ? e.data.text() : ''}; }
  e.waitUntil(self.registration.showNotification(m.title || 'a companion is waiting', {
    body: m.body || '',
    tag: m.tag || 'magi',
    // Replaces the earlier notification for this companion without a second buzz: the tag already
    // makes it silent-replace on some platforms and explicit on the rest.
    renotify: false,
    icon: '/icon.svg',
    badge: '/icon.svg',
    data: {url: m.url || '/'},
  }));
});
self.addEventListener('notificationclick', e => {
  e.notification.close();
  const want = new URL(e.notification.data && e.notification.data.url || '/', self.location.origin).href;
  // A console already open is focused rather than opened again. Somebody with the fleet on a second
  // screen does not want a third copy of it.
  e.waitUntil(clients.matchAll({type: 'window', includeUncontrolled: true}).then(list => {
    for (const c of list) {
      if (c.url.startsWith(self.location.origin) && 'focus' in c) return c.navigate(want).then(x => x.focus());
    }
    return clients.openWindow(want);
  }));
});
`
