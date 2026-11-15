const TZ_KEY = "pgledger-visualizer.tz";
const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

function tzMode() {
  return localStorage.getItem(TZ_KEY) === "local" ? "local" : "utc";
}

const pad = (n, w = 2) => String(n).padStart(w, "0");

function formatLocal(d) {
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
  );
}

function formatTime(d, local) {
  return local ? formatLocal(d) : d.toISOString().slice(0, 23).replace("T", " ");
}

// datetime-local value (YYYY-MM-DDTHH:MM:SS) for a Date, in local or UTC fields.
function inputValue(d, local) {
  if (local) {
    return (
      `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T` +
      `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
    );
  }
  return d.toISOString().slice(0, 19);
}

function applyTimezone(root) {
  const local = tzMode() === "local";
  for (const el of root.querySelectorAll("time[datetime]")) {
    if (!el.dataset.utc) el.dataset.utc = el.textContent;
    const full = formatLocal(new Date(el.getAttribute("datetime")));
    const localText = el.dataset.fmt === "minute" ? full.slice(0, 16) : full;
    el.textContent = local ? localText : el.dataset.utc;
    el.title = local ? `${el.dataset.utc} UTC` : `${localText} ${localZone}`;
  }
  for (const el of root.querySelectorAll("[data-tz-label]")) {
    el.textContent = local ? localZone : "UTC";
  }
  for (const input of root.querySelectorAll("input[data-tz-input]")) {
    if (!input.value) continue;
    const shown = input.dataset.tzShown || "utc";
    const want = local ? "local" : "utc";
    if (shown === want) continue;
    const d = shown === "utc" ? new Date(`${input.value}Z`) : new Date(input.value);
    if (!Number.isNaN(d.getTime())) input.value = inputValue(d, local);
    input.dataset.tzShown = want;
  }
  const toggle = document.querySelector("[data-tz-toggle]");
  if (toggle) toggle.textContent = local ? `Times: Local (${localZone})` : "Times: UTC";
}

document.addEventListener("click", (e) => {
  if (!e.target.closest("[data-tz-toggle]")) return;
  localStorage.setItem(TZ_KEY, tzMode() === "local" ? "utc" : "local");
  applyTimezone(document);
});

// Theme: System follows the OS; Light and Dark set data-theme, which tokens.css
// maps to color-scheme. theme.js applies the saved choice before first paint.
const THEME_KEY = "pgledger-visualizer.theme";
const themes = ["system", "light", "dark"];
const themeLabels = { system: "System", light: "Light", dark: "Dark" };

function themeMode() {
  const t = localStorage.getItem(THEME_KEY);
  return themes.includes(t) ? t : "system";
}

function applyTheme() {
  const mode = themeMode();
  if (mode === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = mode;
  const toggle = document.querySelector("[data-theme-toggle]");
  if (toggle) toggle.textContent = `Theme: ${themeLabels[mode]}`;
}

document.addEventListener("click", (e) => {
  if (!e.target.closest("[data-theme-toggle]")) return;
  const next = themes[(themes.indexOf(themeMode()) + 1) % themes.length];
  localStorage.setItem(THEME_KEY, next);
  applyTheme();
});

// The server always reads `at` as UTC, so convert a local entry before submit.
document.addEventListener(
  "submit",
  (e) => {
    const form = e.target;
    for (const input of form.querySelectorAll("input[data-tz-input]")) {
      if (!input.value || (input.dataset.tzShown || "utc") !== "local") continue;
      const d = new Date(input.value);
      if (Number.isNaN(d.getTime())) continue;
      // Submit the UTC value, then restore the local display for bfcache/back.
      const shown = input.value;
      input.value = d.toISOString().slice(0, 19);
      setTimeout(() => {
        input.value = shown;
      }, 0);
    }
  },
  true,
);

function liveSearch(form) {
  const target = document.querySelector(form.dataset.live);
  let timer;
  let controller;

  async function run() {
    clearTimeout(timer);
    const params = new URLSearchParams(new FormData(form));
    history.replaceState(null, "", `${form.action}?${params}`);
    controller?.abort();
    controller = new AbortController();
    params.set("partial", "1");
    try {
      const res = await fetch(`${form.action}?${params}`, { signal: controller.signal });
      target.innerHTML = await res.text();
      setCursor(null);
      applyTimezone(target);
    } catch (err) {
      if (err.name !== "AbortError") target.textContent = `Error: ${err.message}`;
    }
  }

  form.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(run, 200);
  });
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    run();
  });
}

// Copy buttons: copy data-copy, then show a check mark briefly.
document.addEventListener("click", async (e) => {
  const button = e.target.closest("button[data-copy]");
  if (!button) return;
  e.preventDefault();
  try {
    await navigator.clipboard.writeText(button.dataset.copy);
  } catch {
    return;
  }
  button.classList.add("copied");
  clearTimeout(button.copiedTimer);
  button.copiedTimer = setTimeout(() => button.classList.remove("copied"), 1200);
});

document.addEventListener("keydown", (e) => {
  if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
  const active = document.activeElement;
  if (active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA" || active.tagName === "SELECT")) return;
  // The page's own filter first, else the top bar's search.
  const input = document.querySelector("main input[type=search]") || document.querySelector(".topbar-search input");
  if (!input) return;
  e.preventDefault();
  input.focus();
  input.select();
});

// Row cursor: j/k move through the rows of every list and grid on the page, a click
// on a row (not on a link) selects it, Enter opens it, Escape clears it. On wide
// screens the selected row's record shows in a preview panel beside the page.
const rowSelector = "main table.list tbody tr, main table.grid tbody tr";
const previewQuery = matchMedia("(min-width: 1400px)");
let cursor = null;

// A row's own record: its entry or transfer when the row says which, else its first link.
const rowLink = (row) => {
  const { entry, transfer } = row.dataset;
  const own = entry ? `/entries/${entry}` : transfer ? `/transfers/${transfer}` : null;
  return (own && row.querySelector(`a[href="${CSS.escape(own)}"]`)) || row.querySelector("a[href]:not(.pill)");
};
const typing = (el) => el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT");

// Listeners (the balance chart) hear every change as a "cursorchange" event.
function setCursor(row, scroll = true) {
  cursor?.classList.remove("cursor");
  cursor = row;
  if (row) {
    row.classList.add("cursor");
    if (scroll) row.scrollIntoView({ block: "nearest" });
    showPreview(row);
  } else {
    closePreview();
  }
  document.dispatchEvent(new Event("cursorchange"));
}

function moveCursor(step) {
  const rows = [...document.querySelectorAll(rowSelector)].filter((r) => r.offsetParent);
  if (!rows.length) return;
  const i = cursor?.isConnected ? rows.indexOf(cursor) : -1;
  const next = i < 0 ? (step > 0 ? 0 : rows.length - 1) : Math.min(Math.max(i + step, 0), rows.length - 1);
  setCursor(rows[next]);
}

document.addEventListener("keydown", (e) => {
  if (e.metaKey || e.ctrlKey || e.altKey || typing(document.activeElement)) return;
  if (e.key === "j" || e.key === "k") {
    e.preventDefault();
    moveCursor(e.key === "j" ? 1 : -1);
  } else if (e.key === "Enter" && cursor?.isConnected && !document.activeElement?.matches("a, button")) {
    const link = rowLink(cursor);
    if (link) location.assign(link.href);
  } else if (e.key === "Escape" && cursor) {
    setCursor(null);
  }
});

document.addEventListener("click", (e) => {
  const row = e.target.closest(rowSelector);
  if (!row || e.target.closest("a, button, input, select, summary")) return;
  setCursor(row === cursor ? null : row, false);
});

const previewCache = new Map();
let previewPanel = null;

function closePreview() {
  previewPanel?.remove();
  previewPanel = null;
  document.body.classList.remove("has-preview");
}

// The record's own page, cut down to its heading, flow line and fields.
async function previewContent(href) {
  if (!previewCache.has(href)) {
    previewCache.set(
      href,
      fetch(href)
        .then((res) => res.text())
        .then((html) => {
          const page = new DOMParser().parseFromString(html, "text/html").querySelector("main");
          if (!page) return "";
          const parts = [page.querySelector(":scope > h1"), page.querySelector(".flow"), page.querySelector(".fields")];
          // Tools on the page (balance at) don't belong in a preview.
          for (const form of parts[2]?.querySelectorAll("form") ?? []) {
            const dd = form.closest("dd");
            dd.previousElementSibling.remove();
            dd.remove();
          }
          return parts.map((el) => el?.outerHTML ?? "").join("");
        }),
    );
  }
  return previewCache.get(href);
}

async function showPreview(row) {
  const link = rowLink(row);
  if (!previewQuery.matches || !link) {
    closePreview();
    return;
  }
  if (!previewPanel) {
    previewPanel = document.createElement("aside");
    previewPanel.className = "preview";
    previewPanel.innerHTML =
      '<div class="preview-bar"><a class="preview-open">Open →</a>' +
      '<button type="button" class="preview-close" title="close (Esc)">Close</button></div>' +
      '<div class="preview-body"></div>';
    previewPanel.querySelector(".preview-close").addEventListener("click", () => setCursor(null));
    document.body.append(previewPanel);
    document.body.classList.add("has-preview");
  }
  const href = link.href;
  previewPanel.querySelector(".preview-open").href = href;
  previewPanel.dataset.href = href;
  const body = previewPanel.querySelector(".preview-body");
  try {
    const html = await previewContent(href);
    if (previewPanel?.dataset.href !== href) return;
    body.innerHTML = html;
    applyTimezone(body);
  } catch (err) {
    body.textContent = `Error: ${err.message}`;
  }
}

previewQuery.addEventListener("change", () => (cursor?.isConnected ? showPreview(cursor) : closePreview()));

function toggleColumn(e, on) {
  const cell = e.target.closest("table.grid [data-col]");
  if (!cell) return;
  const table = cell.closest("table.grid");
  for (const el of table.querySelectorAll(`[data-col="${cell.dataset.col}"]`)) {
    el.classList.toggle("col-hover", on);
  }
}
document.addEventListener("mouseover", (e) => toggleColumn(e, true));
document.addEventListener("mouseout", (e) => toggleColumn(e, false));

// Hovering a row also highlights the other rows created in its database transaction.
function toggleTx(e, on) {
  const row = e.target.closest("table.grid tbody tr[data-tx]");
  if (!row || row.dataset.tx === "0") return;
  const table = row.closest("table.grid");
  for (const el of table.querySelectorAll(`tbody tr[data-tx="${row.dataset.tx}"]`)) {
    el.classList.toggle("tx-hover", on);
  }
}
document.addEventListener("mouseover", (e) => toggleTx(e, true));
document.addEventListener("mouseout", (e) => toggleTx(e, false));

// Pointer travel, in pixels, below which a press and release is a click rather than a drag.
const clickSlop = 4;

// The point nearest the pointer; on ties the later one, so an entry wins over the balance drawn just before it.
function nearestPoint(plot, points, e) {
  const rect = plot.getBoundingClientRect();
  const x = ((e.clientX - rect.left) / rect.width) * 100;
  let p = points[0];
  for (const q of points) if (Math.abs(q.x - x) <= Math.abs(p.x - x)) p = q;
  return p;
}

// Balance chart hover: snap to the nearest entry and show its balance. The plot's
// link points at that entry's transfer, so clicks, including into a new tab, open it.
function chartHover(plot) {
  const points = JSON.parse(plot.dataset.chart);
  const [guide, dot, readout, link] = ["guide", "dot", "readout", "hit"].map((c) => plot.querySelector(`.${c}`));
  const currency = plot.closest(".chart").dataset.currency || "";

  const show = (p) => {
    guide.style.left = dot.style.left = `${p.x}%`;
    dot.style.top = `${p.y}%`;
    readout.textContent = `${p.b} ${currency} · ${formatTime(new Date(p.t), tzMode() === "local")}`;
    readout.style.left = p.x > 50 ? "" : `calc(${p.x}% + 8px)`;
    readout.style.right = p.x > 50 ? `calc(${100 - p.x}% + 8px)` : "";
    guide.hidden = dot.hidden = readout.hidden = false;
  };
  const hide = () => {
    guide.hidden = dot.hidden = readout.hidden = true;
  };

  // The table rows for a point: its entry's row, else its transfer's grid row.
  const rowsFor = (p) => {
    let rows = [];
    if (p?.e) rows = [...document.querySelectorAll(`tr[data-entry="${CSS.escape(p.e)}"]`)];
    if (p?.id && !rows.length) rows = [...document.querySelectorAll(`tr[data-transfer="${CSS.escape(p.id)}"]`)];
    return rows;
  };
  let linked = [];
  const highlight = (p) => {
    for (const row of linked) row.classList.remove("linked");
    linked = rowsFor(p);
    for (const row of linked) row.classList.add("linked");
  };

  // The point for a table row: its entry, else the last point of its transfer.
  const rowPoint = (row) =>
    row.dataset.entry
      ? points.find((p) => p.e === row.dataset.entry)
      : points.findLast((p) => p.id === row.dataset.transfer);

  // At rest the chart marks the cursor row's point, if it has one.
  const rest = () => {
    const p = cursor?.isConnected && rowPoint(cursor);
    if (p) show(p);
    else hide();
  };
  document.addEventListener("cursorchange", rest);

  plot.addEventListener("mousemove", (e) => {
    const p = nearestPoint(plot, points, e);
    if (p.id) link.href = `/transfers/${encodeURIComponent(p.id)}`;
    else link.removeAttribute("href");
    show(p);
    highlight(p);
  });
  plot.addEventListener("mouseleave", () => {
    rest();
    highlight(null);
  });

  // Hovering a table row marks its point on the chart.
  document.addEventListener("mouseover", (e) => {
    const row = e.target.closest("tr[data-entry], tr[data-transfer]");
    const p = row && rowPoint(row);
    if (p) show(p);
  });
  document.addEventListener("mouseout", (e) => {
    const row = e.target.closest("tr[data-entry], tr[data-transfer]");
    if (row && !row.contains(e.relatedTarget)) rest();
  });

  // A click selects the point's row (cursor and preview), like clicking the row.
  // A drag zooms instead (chartZoom). Modifier clicks still open the transfer
  // in a new tab; a point whose row isn't on the page opens its transfer.
  let downX = null;
  plot.addEventListener("mousedown", (e) => {
    downX = e.clientX;
  });
  plot.addEventListener("click", (e) => {
    if (downX !== null && Math.abs(e.clientX - downX) >= clickSlop) {
      e.preventDefault();
      return;
    }
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const [row] = rowsFor(nearestPoint(plot, points, e));
    if (!row) return;
    e.preventDefault();
    setCursor(row);
  });
}

// Balance chart drag: zoom the page to the selected created_at range.
function chartZoom(plot) {
  const points = JSON.parse(plot.dataset.chart);
  const t0 = Date.parse(points[0].t);
  const t1 = Date.parse(points[points.length - 1].t);
  if (!(t1 > t0)) return;
  const selection = plot.querySelector(".selection");
  let start = null;

  const frac = (e) => {
    const rect = plot.getBoundingClientRect();
    return Math.min(Math.max((e.clientX - rect.left) / rect.width, 0), 1);
  };
  const draw = (a, b) => {
    selection.style.left = `${Math.min(a, b) * 100}%`;
    selection.style.width = `${Math.abs(b - a) * 100}%`;
  };

  plot.addEventListener("mousedown", (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    start = frac(e);
    draw(start, start);
    selection.hidden = false;
  });
  window.addEventListener("mousemove", (e) => {
    if (start !== null) draw(start, frac(e));
  });
  window.addEventListener("mouseup", (e) => {
    if (start === null) return;
    const end = frac(e);
    const [a, b] = [Math.min(start, end), Math.max(start, end)];
    start = null;
    selection.hidden = true;
    if ((b - a) * plot.getBoundingClientRect().width < clickSlop) return;
    const at = (f) => t0 + f * (t1 - t0);
    const url = new URL(location.href);
    url.searchParams.set("from", new Date(Math.floor(at(a))).toISOString());
    // Entry times have microseconds but Date only milliseconds, so round the end up to keep the last selected entry.
    url.searchParams.set("to", new Date(Math.floor(at(b)) + 1).toISOString());
    url.searchParams.delete("before");
    location.assign(url);
  });
}

document.addEventListener("DOMContentLoaded", () => {
  for (const form of document.querySelectorAll("form[data-live]")) liveSearch(form);
  for (const plot of document.querySelectorAll(".plot[data-chart]")) {
    chartHover(plot);
    chartZoom(plot);
  }
  applyTimezone(document);
  applyTheme();
});
