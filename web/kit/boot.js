// /kit/v1/boot.js: one classic script tag gives any page the kit environment.
// It must run before the first module script so the import map applies. The
// build fills in the theme text and appends Tailwind's browser compiler below.
(function () {
  var base = new URL(".", document.currentScript.src).href;

  // pkg/viewscript/importmap.json, which `rzm validate views` also reads.
  var modules = __MODULES__;

  var imports = {};

  for (var name in modules) imports[name] = base + modules[name];

  var map = document.createElement("script");
  map.type = "importmap";
  map.textContent = JSON.stringify({ imports: imports });
  document.currentScript.after(map);

  // The theme carries web/src/fonts.css, so frames load the same self-hosted
  // font files the Rhizome UI serves from /fonts/.
  var theme = document.createElement("style");
  theme.type = "text/tailwindcss";
  theme.textContent = __THEME__;
  document.head.appendChild(theme);

  // Follow the host's appearance and theme (data-appearance, data-theme on
  // <html>, see web/src/lib/appearance.ts) before first paint and as they
  // change. A view page opened on its own reads the stored choice instead.
  var root = document.documentElement;
  var keys = ["appearance", "theme"];
  var host = null;

  try {
    if (window.parent !== window) host = window.parent.document.documentElement;
  } catch {
    host = null;
  }

  function mirror() {
    for (var i = 0; i < keys.length; i++) {
      var value = host.dataset[keys[i]];

      if (value) root.dataset[keys[i]] = value;
      else delete root.dataset[keys[i]];
    }
  }

  if (host) {
    mirror();
    new MutationObserver(mirror).observe(host, {
      attributes: true,
      attributeFilter: ["data-appearance", "data-theme"],
    });
  } else {
    try {
      var appearance = localStorage.getItem("rhizome:appearance");

      if (appearance === "light" || appearance === "dark") root.dataset.appearance = appearance;
    } catch {
      // No storage: follow the system appearance.
    }
  }
})();
