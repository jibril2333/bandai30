// Minimal service worker — enables "add to home screen" / installability.
// Intentionally network-passthrough (no offline asset cache) so the version-hash
// busting on app.js/styles.css keeps working and nothing goes stale.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));
self.addEventListener('fetch', () => { /* pass through to network */ });
