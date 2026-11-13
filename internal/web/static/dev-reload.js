// Included only with `serve --dev`. The server sends its boot id on connect and
// the path of each changed file under internal/web. Stylesheets are swapped in
// place, keeping scroll position; anything else, or a server restart, reloads.
let boot = null;

new EventSource("/_dev/events").onmessage = (e) => {
  const msg = JSON.parse(e.data);
  if (msg.boot) {
    if (boot !== null && boot !== msg.boot) location.reload();
    boot = msg.boot;
    return;
  }
  if (!msg.changed.endsWith(".css")) {
    location.reload();
    return;
  }
  for (const link of document.querySelectorAll("link[rel=stylesheet]")) {
    const url = new URL(link.href);
    url.searchParams.set("v", Date.now());
    link.href = url;
  }
};
