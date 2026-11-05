"use strict";

const TZ_KEY = "pgledger-viz.tz";
const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

function tzMode() {
  return localStorage.getItem(TZ_KEY) === "local" ? "local" : "utc";
}

const pad = (n, w = 2) => String(n).padStart(w, "0");

function formatLocal(d) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

// datetime-local value (YYYY-MM-DDTHH:MM:SS) for a Date, in local or UTC fields.
function inputValue(d, local) {
  if (local) {
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T` +
      `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  }
  return d.toISOString().slice(0, 19);
}

function applyTimezone(root) {
  const local = tzMode() === "local";
  for (const el of root.querySelectorAll("time[datetime]")) {
    if (!el.dataset.utc) el.dataset.utc = el.textContent;
    const localText = formatLocal(new Date(el.getAttribute("datetime")));
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
    const d = shown === "utc" ? new Date(input.value + "Z") : new Date(input.value);
    if (!isNaN(d)) input.value = inputValue(d, local);
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
document.addEventListener("submit", (e) => {
  const form = e.target;
  for (const input of form.querySelectorAll("input[data-tz-input]")) {
    if (!input.value || (input.dataset.tzShown || "utc") !== "local") continue;
    const d = new Date(input.value);
    if (isNaN(d)) continue;
    // Submit the UTC value, then restore the local display for bfcache/back.
    const shown = input.value;
    input.value = d.toISOString().slice(0, 19);
    setTimeout(() => { input.value = shown; }, 0);
  }
}, true);

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

document.addEventListener("DOMContentLoaded", () => {
  for (const form of document.querySelectorAll("form[data-live]")) liveSearch(form);
  applyTimezone(document);
});
