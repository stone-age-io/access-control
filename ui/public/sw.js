// Stone Access Service Worker
// Grug say: I do nothing. I exist to satisfy Chrome Install Criteria.
// No caching = No bugs.
//
// Deliberately not an offline cache, and here that is a safety property rather than
// only a simplicity one: this app's job is to tell someone what their badge opens
// RIGHT NOW. A cached /api/badge/me would show a revoked pass as live, and a cached
// bundle would keep running against a changed API. Offline resilience in this system
// lives at the edge (the controller decides locally, and internal/controller's policy
// cache is the thing designed to be stale safely) — not in a phone's browser.
self.addEventListener('install', (event) => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(clients.claim());
});

self.addEventListener('fetch', (event) => {
  // Network only. No cache. No complexity.
  return;
});
