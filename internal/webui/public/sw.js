// Stone Age Service Worker
// Grug say: I do nothing. I exist to satisfy Chrome Install Criteria.
// No caching = No bugs.
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
