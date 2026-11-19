// Loaded synchronously in <head> so a saved theme applies before first paint.
// app.js owns the toggle button.
try {
  const theme = localStorage.getItem("pgledger-ui.theme");
  if (theme === "light" || theme === "dark") document.documentElement.dataset.theme = theme;
} catch {
  // Storage blocked: follow the OS.
}
