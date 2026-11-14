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
      applyTimezone(target);
      applyColumnPickers();
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

// Column picker: hides list-table columns by their data-column key, saved per page.
const columnsKey = (picker) => `pgledger-visualizer.hidden-columns.${picker.dataset.columnPicker}`;

function hiddenColumns(picker) {
  try {
    return new Set(JSON.parse(localStorage.getItem(columnsKey(picker))) || []);
  } catch {
    return new Set();
  }
}

function applyColumnPicker(picker) {
  const table = document.querySelector(`${picker.dataset.target} table.list`);
  if (!table) return;
  const hidden = hiddenColumns(picker);
  const options = picker.querySelector(".column-picker-options");
  if (!options.children.length) buildColumnOptions(options, table, hidden);
  for (const cell of table.querySelectorAll("[data-column]")) {
    cell.hidden = hidden.has(cell.dataset.column);
  }
}

function buildColumnOptions(options, table, hidden) {
  for (const th of table.querySelectorAll("thead th[data-column]")) {
    const key = th.dataset.column;
    const box = document.createElement("input");
    box.type = "checkbox";
    box.value = key;
    box.checked = !hidden.has(key);
    const label = document.createElement("label");
    label.append(box, th.textContent.trim());
    options.append(label);
  }
}

function applyColumnPickers() {
  for (const picker of document.querySelectorAll("[data-column-picker]")) applyColumnPicker(picker);
}

document.addEventListener("change", (e) => {
  const picker = e.target.closest("[data-column-picker]");
  if (!picker) return;
  const hidden = [...picker.querySelectorAll("input:not(:checked)")].map((box) => box.value);
  localStorage.setItem(columnsKey(picker), JSON.stringify(hidden));
  applyColumnPicker(picker);
});

document.addEventListener("keydown", (e) => {
  if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
  const active = document.activeElement;
  if (active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA" || active.tagName === "SELECT")) return;
  const input = document.querySelector("input[type=search]");
  if (!input) return;
  e.preventDefault();
  input.focus();
  input.select();
});

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
  let linked = [];
  const highlight = (p) => {
    for (const row of linked) row.classList.remove("linked");
    linked = [];
    if (p?.e) linked = [...document.querySelectorAll(`tr[data-entry="${CSS.escape(p.e)}"]`)];
    if (p?.id && !linked.length) linked = [...document.querySelectorAll(`tr[data-transfer="${CSS.escape(p.id)}"]`)];
    for (const row of linked) row.classList.add("linked");
  };

  plot.addEventListener("mousemove", (e) => {
    const p = nearestPoint(plot, points, e);
    if (p.id) link.href = `/transfers/${encodeURIComponent(p.id)}`;
    else link.removeAttribute("href");
    show(p);
    highlight(p);
  });
  plot.addEventListener("mouseleave", () => {
    hide();
    highlight(null);
  });

  // Hovering a table row marks its point on the chart.
  const rowPoint = (row) =>
    row.dataset.entry
      ? points.find((p) => p.e === row.dataset.entry)
      : points.findLast((p) => p.id === row.dataset.transfer);
  document.addEventListener("mouseover", (e) => {
    const row = e.target.closest("tr[data-entry], tr[data-transfer]");
    const p = row && rowPoint(row);
    if (p) show(p);
  });
  document.addEventListener("mouseout", (e) => {
    const row = e.target.closest("tr[data-entry], tr[data-transfer]");
    if (row && !row.contains(e.relatedTarget)) hide();
  });

  let downX = null;
  plot.addEventListener("mousedown", (e) => {
    downX = e.clientX;
  });
  plot.addEventListener("click", (e) => {
    if (downX !== null && Math.abs(e.clientX - downX) >= clickSlop) e.preventDefault();
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
  applyColumnPickers();
});
