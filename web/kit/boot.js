// /kit/v1/<build>/boot.js: one classic script tag gives any page the kit
// environment. It must run before the first module script so the import map
// applies. The build fills in the import map.
//
// The page gets the precompiled stylesheet (kit.css) and, unless the tag says
// data-tailwind="false", Tailwind's runtime compiler (tailwind.js) for
// utilities the stylesheet lacks (SPEC-0136 R5).
(function () {
  var script = document.currentScript;
  var base = new URL(".", script.src).href;

  // pkg/viewscript/importmap.json, which `rzm validate views` also reads.
  var modules = __MODULES__;

  var imports = {};

  for (var name in modules) imports[name] = base + modules[name];

  var map = document.createElement("script");
  map.type = "importmap";
  map.textContent = JSON.stringify({ imports: imports });
  script.after(map);

  // The theme carries web/src/fonts.css, so frames load the same self-hosted
  // font files the Rhizome UI serves from /fonts/.
  var sheet = document.createElement("link");
  sheet.rel = "stylesheet";
  sheet.href = base + "kit.css";
  sheet.setAttribute("blocking", "render");
  document.head.appendChild(sheet);

  if (script.dataset.tailwind !== "false") {
    var compiler = document.createElement("script");
    compiler.src = base + "tailwind.js";
    document.head.appendChild(compiler);
  }

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
