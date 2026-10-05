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

  // Same families the Rhizome UI loads in web/index.html.
  var fonts = document.createElement("link");
  fonts.rel = "stylesheet";
  fonts.href =
    "https://fonts.googleapis.com/css2?family=Merriweather:wght@400;700;900&family=Barlow:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap";
  document.head.appendChild(fonts);

  var theme = document.createElement("style");
  theme.type = "text/tailwindcss";
  theme.textContent = __THEME__;
  document.head.appendChild(theme);
})();
