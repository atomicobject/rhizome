import path from "node:path";
import { defineConfig } from "vite";

import { rhizomeDevProxyPlugin } from "./devProxyConfig";

export default defineConfig(({ command }) => {
  return {
    root: path.resolve(__dirname),
    plugins: command === "serve" ? [rhizomeDevProxyPlugin({ cwd: __dirname })] : undefined,
    build: {
      outDir: path.resolve(__dirname, "../pkg/app/web/assets/dist"),
      emptyOutDir: true,
    },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["./vitest.setup.ts"],
      include: [
        "src/**/*.test.ts",
        "src/**/*.test.tsx",
        "kit/**/*.test.ts",
        "kit/**/*.test.tsx",
        "bundled-views/**/*.test.ts",
        "bundled-views/**/*.test.tsx",
        "*.test.ts",
      ],
      exclude: ["tests/e2e/**"],
      // Bundled views import the kit by its import-map names, as the browser
      // resolves them; tests resolve those names to the kit sources.
      alias: {
        "@rhizome/kit": path.resolve(__dirname, "kit/index.tsx"),
        "@rhizome/ui": path.resolve(__dirname, "kit/ui/index.ts"),
      },
      server: {
        deps: {
          // @atomic-editor/editor publishes extensionless internal ESM
          // imports. Inline it so Vitest lets Vite rewrite those imports.
          inline: ["@atomic-editor/editor"],
        },
      },
      deps: {
        optimizer: {
          web: {
            include: ["@atomic-editor/editor"],
          },
        },
      },
    },
  };
});
