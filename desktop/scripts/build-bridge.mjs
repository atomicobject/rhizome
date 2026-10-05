import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const rustInfo = execFileSync("rustc", ["-vV"], { encoding: "utf8" });
const host = rustInfo.match(/^host: (.+)$/m)?.[1];
const target = process.env.TAURI_ENV_TARGET_TRIPLE || host;
if (!target || target !== host)
  throw new Error(
    "Build the desktop bridge on its target platform; cross compilation is not configured.",
  );
const suffix = process.platform === "win32" ? ".exe" : "";
const output = resolve(
  root,
  `desktop/src-tauri/binaries/rhizome-desktop-bridge-${target}${suffix}`,
);
mkdirSync(dirname(output), { recursive: true });
execFileSync("go", ["build", "-tags", "fts5", "-o", output, "./desktop/bridge"], {
  cwd: root,
  stdio: "inherit",
});
console.log(`Built desktop bridge for ${target}`);
