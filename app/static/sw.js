// Athanor's service worker: only the notifications of a paired device (distance_push.go); no cache, no
// request of the pages goes through it.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));
self.addEventListener("push", (e) => {
  let m = {};
  try { m = e.data ? e.data.json() : {}; } catch (_) { m = { texte: e.data ? e.data.text() : "" }; }
  e.waitUntil(self.registration.showNotification(m.titre || "Athanor", {
    body: m.texte || "", icon: "/icone-180.png", badge: "/icone-180.png", tag: m.tag || undefined,
    renotify: !!m.tag, data: { url: typeof m.url === "string" && m.url.startsWith("/") && !m.url.startsWith("//") ? m.url : "/" },
  }));
});
self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const url = (e.notification.data && e.notification.data.url) || "/";
  e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((liste) => {
    for (const c of liste) {
      if (new URL(c.url).origin === self.location.origin && "focus" in c) return c.focus().then((f) => (f && "navigate" in f ? f.navigate(url) : f)).catch(() => {});
    }
    return self.clients.openWindow(url);
  }));
});
