// /kit/v1/<build>/tailwind.js: Tailwind's runtime compiler, which boot.js loads
// unless the page opts out (SPEC-0136 R5). It compiles the theme below and
// every utility the page uses, so a view's classes work whether or not the
// precompiled kit.css has them. The build fills in the theme and appends
// @tailwindcss/browser, which reads this style tag.
(function () {
  var theme = document.createElement("style");
  theme.type = "text/tailwindcss";
  theme.textContent = __THEME__;
  document.head.appendChild(theme);
})();
