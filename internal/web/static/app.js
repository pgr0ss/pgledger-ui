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

// Balance chart hover: snap to the nearest entry and show its balance.
function chartHover(plot) {
  const points = JSON.parse(plot.dataset.chart);
  const [guide, dot, readout] = ["guide", "dot", "readout"].map((c) => plot.querySelector(`.${c}`));
  const currency = plot.closest(".chart").dataset.currency || "";

  plot.addEventListener("mousemove", (e) => {
    const rect = plot.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * 100;
    let p = points[0];
    for (const q of points) if (Math.abs(q.x - x) < Math.abs(p.x - x)) p = q;
    guide.style.left = dot.style.left = `${p.x}%`;
    dot.style.top = `${p.y}%`;
    readout.textContent = `${p.b} ${currency} · ${formatTime(new Date(p.t), tzMode() === "local")}`;
    readout.style.left = p.x > 50 ? "" : `calc(${p.x}% + 8px)`;
    readout.style.right = p.x > 50 ? `calc(${100 - p.x}% + 8px)` : "";
    guide.hidden = dot.hidden = readout.hidden = false;
  });
  plot.addEventListener("mouseleave", () => {
    guide.hidden = dot.hidden = readout.hidden = true;
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
    if ((b - a) * plot.getBoundingClientRect().width < 4) return;
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
});
