import { cpSync, readFileSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { defineConfig, type Plugin } from "vite";

// Builds the custom view kit (SPEC-0105) into the embedded assets, after the
// main UI build because that build empties assets/dist. Every entry is an ES
// module named by boot.js's import map; shared chunks keep one React instance.

const require = createRequire(import.meta.url);

const kitDir = path.resolve(__dirname, "kit");

// The import map's one source, shared with `rzm validate views`: bare
// specifier to kit file. Each file is built from the entry module named below.
const importMap: Record<string, string> = JSON.parse(
  readFileSync(path.resolve(__dirname, "../pkg/viewscript/importmap.json"), "utf8"),
);

function kitEntry(name: string) {
  if (name === "ui") return path.join(kitDir, "ui/index.ts");

  if (name === "kit") return path.join(kitDir, "index.tsx");

  return path.join(kitDir, `entries/${name}.ts`);
}

// React ships CommonJS, whose named exports a browser import map cannot see.
// A `cjs:<package>` import re-exports the package's keys explicitly.
function cjsReexport(): Plugin {
  return {
    name: "rhizome-kit-cjs-reexport",
    resolveId: (id) => (id.startsWith("cjs:") ? `\0${id}` : null),
    load(id) {
      if (!id.startsWith("\0cjs:")) return null;

      const name = id.slice("\0cjs:".length);

      const keys = Object.keys(require(name)).filter(
        (key) => key !== "default" && /^[A-Za-z_$][\w$]*$/.test(key),
      );

      return `import pkg from ${JSON.stringify(name)};\nexport default pkg;\nexport const { ${keys.join(", ")} } = pkg;\n`;
    },
  };
}

function bootScript(): Plugin {
  return {
    name: "rhizome-kit-boot",
    generateBundle() {
      const tokens = /:root\s*\{[^}]*\}/.exec(
        readFileSync(path.resolve(__dirname, "src/base.css"), "utf8"),
      );

      if (!tokens) throw new Error("src/base.css has no :root token block");

      const theme = `${tokens[0]}\n${readFileSync(path.join(kitDir, "theme.css"), "utf8")}`;

      const boot = readFileSync(path.join(kitDir, "boot.js"), "utf8")
        .replace("__THEME__", () => JSON.stringify(theme))
        .replace("__MODULES__", () => JSON.stringify(importMap));

      const tailwind = readFileSync(require.resolve("@tailwindcss/browser"), "utf8");

      this.emitFile({ type: "asset", fileName: "boot.js", source: `${boot}\n${tailwind}` });
    },
  };
}

// Rhizome's own views (SPEC-0111) ship as ordinary view source beside the kit.
// The server serves this copy at /views/_bundled/; tests and fixtures stay out.
function bundledViews(): Plugin {
  const source = path.resolve(__dirname, "bundled-views");

  const target = path.resolve(__dirname, "../pkg/app/web/assets/dist/bundled-views");

  return {
    name: "rhizome-bundled-views",
    closeBundle() {
      rmSync(target, { recursive: true, force: true });
      cpSync(source, target, {
        recursive: true,
        filter: (file) => {
          const name = path.basename(file);

          return !/\.test\./.test(name) && name !== "__fixtures__" && name !== "fixtures";
        },
      });
    },
  };
}

export default defineConfig({
  root: kitDir,
  plugins: [cjsReexport(), bootScript(), bundledViews()],
  define: { "process.env.NODE_ENV": JSON.stringify("production") },
  build: {
    outDir: path.resolve(__dirname, "../pkg/app/web/assets/dist/kit/v1"),
    emptyOutDir: true,
    lib: {
      formats: ["es"],
      entry: Object.fromEntries(
        Object.values(importMap)
          .map((file) => file.replace(/\.js$/, ""))
          .map((name) => [name, kitEntry(name)]),
      ),
    },
    rollupOptions: {
      output: { entryFileNames: "[name].js", chunkFileNames: "chunks/[name]-[hash].js" },
    },
  },
});
