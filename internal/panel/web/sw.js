// Mainspring's service worker. It caches nothing: every screen shows live
// data. Its one job is a clear page when the phone can't reach Mainspring,
// instead of the browser's error, so the installed app stays an app.
const OFFLINE = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover"><meta name="color-scheme" content="dark"><title>Mainspring</title>
<style>html,body{height:100%;margin:0;background:#1C1C1C;color:#E5E5E5;font:15px/22px -apple-system,BlinkMacSystemFont,Inter,"Segoe UI",sans-serif}
main{min-height:100%;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:8px;padding:24px;text-align:center;box-sizing:border-box}
h1{margin:0;font-size:17px;font-weight:500;color:#F5F5F5}p{margin:0;color:#A1A1A1;max-width:320px}
button{margin-top:12px;height:44px;padding:0 18px;border-radius:10px;border:0;background:#EDEDED;color:#111;font:inherit;font-weight:500}</style></head>
<body><main><h1>Can't reach Mainspring</h1><p>Your phone isn't on the same network or tailnet as Mainspring right now.</p><button onclick="location.reload()">Try again</button></main></body></html>`;

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
self.addEventListener("fetch", (event) => {
  if (event.request.mode !== "navigate") return;
  event.respondWith(fetch(event.request).catch(() => new Response(OFFLINE, { headers: { "Content-Type": "text/html; charset=utf-8" } })));
});
