import type { Plugin } from "vite";

import {
  APPEARANCES,
  APPEARANCE_KEY,
  DESKTOP_THEME_KEY,
  THEME_KEY,
} from "./src/lib/appearanceSettings";
import { THEMES } from "./src/lib/themes";

const settings = JSON.stringify({
  themes: THEMES.map((theme) => theme.id),
  appearances: APPEARANCES.filter((appearance) => appearance !== "auto"),
  appearanceKey: APPEARANCE_KEY,
  themeKey: THEME_KEY,
  desktopThemeKey: DESKTOP_THEME_KEY,
});

// Inline and synchronous: React and module scripts load after the first paint.
const bootstrap = `<script>
  (function (settings) {
    try {
      var root = document.documentElement;
      if (window.__RHIZOME_DESKTOP__) {
        var desktopTheme = window.__RHIZOME_DESKTOP_THEME__;
        var later = JSON.parse(localStorage.getItem(settings.desktopThemeKey) || "null");
        if (later && later.at > (window.__RHIZOME_DESKTOP_THEME_AT__ || 0)) {
          desktopTheme = later.theme;
        }
        if (settings.themes.includes(desktopTheme)) root.dataset.theme = desktopTheme;
        return;
      }
      var theme = localStorage.getItem(settings.themeKey);
      var appearance = localStorage.getItem(settings.appearanceKey);
      if (settings.themes.includes(theme)) root.dataset.theme = theme;
      if (settings.appearances.includes(appearance)) root.dataset.appearance = appearance;
    } catch (error) {}
  })(${settings});
</script>`;

export const themeBootstrapPlugin = {
  name: "rhizome-theme-bootstrap",
  transformIndexHtml: {
    order: "pre",
    handler(html: string) {
      return html.replace("<!-- rhizome-theme-bootstrap -->", bootstrap);
    },
  },
} satisfies Plugin;
