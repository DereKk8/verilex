// theme.js runs before the first paint, so a chosen theme never flashes the other one.
(function () {
  try {
    const t = localStorage.getItem("vx-theme");
    if (t === "light" || t === "dark") document.documentElement.setAttribute("data-theme", t);
  } catch (e) {
    /* storage can be blocked; the system theme still applies */
  }
})();
