import { compile, optimize } from "@tailwindcss/node";
import { Scanner } from "@tailwindcss/oxide";
import { readFileSync } from "node:fs";
import path from "node:path";

// The kit's precompiled stylesheet (SPEC-0136 R5), built by vite.kit.config.ts.

const web = import.meta.dirname;

const kitDir = path.join(web, "kit");

// The theme every frame starts from: fonts, tokens, the shadcn mapping, and the
// kit's components. tailwind.js hands the same text to the runtime compiler.
export const kitTheme = [
  path.join(web, "src/fonts.css"),
  path.join(web, "src/tokens.css"),
  path.join(kitDir, "theme.css"),
  path.join(kitDir, "components.css"),
  path.join(web, "src/markdown.css"),
  path.join(web, "src/diagram.css"),
]
  .map((file) => readFileSync(file, "utf8"))
  .join("\n");

// Utilities come from the kit and the bundled starters' views, so experiences
// with `tailwind: false` need no runtime compiler.
const sources = `
@source "./";
@source not "./**/*.test.{ts,tsx}";
@source "../../pkg/app/cli/init/templates/starters/*/module/views/**/*";
`;

async function kitCompiler() {
  return compile(`${kitTheme}\n${sources}`, { base: kitDir, onDependency: () => {} });
}

export async function kitCandidates() {
  return new Scanner({ sources: (await kitCompiler()).sources }).scan();
}

export async function buildKitStylesheet() {
  const compiler = await kitCompiler();

  const candidates = new Scanner({ sources: compiler.sources }).scan();

  return optimize(compiler.build(candidates), { minify: true }).code;
}
