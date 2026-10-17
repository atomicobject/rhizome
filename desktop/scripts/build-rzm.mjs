// Puts the rzm the app bundles at its Tauri sidecar path. The release build
// passes the exact binary GoReleaser archives in RZM_DESKTOP_PREBUILT_RZM;
// otherwise `make build` builds rzm with the workspace web UI it embeds
// (NO_WEB=1 skips the web build, as for make build).
import { execFileSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const rustInfo = execFileSync("rustc", ["-vV"], { encoding: "utf8" });
const host = rustInfo.match(/^host: (.+)$/m)?.[1];
const target = process.env.TAURI_ENV_TARGET_TRIPLE || host;
if (!target || target !== host)
  throw new Error("Build rzm on its target platform; cross compilation is not configured.");
const suffix = process.platform === "win32" ? ".exe" : "";
const output = resolve(root, `desktop/src-tauri/binaries/rzm-${target}${suffix}`);
mkdirSync(dirname(output), { recursive: true });
const prebuilt = process.env.RZM_DESKTOP_PREBUILT_RZM;
if (prebuilt) {
  copyFileSync(prebuilt, output);
  chmodSync(output, 0o755);
  console.log(`Bundled ${prebuilt} for ${target}`);
} else {
  // The Makefile reads $(PWD), which cwd alone does not update.
  execFileSync("make", ["build"], {
    cwd: root,
    stdio: "inherit",
    env: { ...process.env, PWD: root },
  });
  const goos = execFileSync("go", ["env", "GOOS"], { encoding: "utf8" }).trim();
  copyFileSync(resolve(root, `bin/${goos}/rzm${suffix}`), output);
  chmodSync(output, 0o755);
  console.log(`Built rzm for ${target}`);
}
