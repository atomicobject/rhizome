import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

export function desktopTeamAPIOrigin(env = process.env, readOrigin = productionOrigin) {
  const override = env.RZM_DESKTOP_TEAM_API_ORIGIN?.trim();
  const value = override || readOrigin().trim();
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error("Desktop team API origin must be a root HTTPS URL or loopback HTTP URL.");
  }
  const local = ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
  if (
    (url.protocol !== "https:" && !(override && local && url.protocol === "http:")) ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    value.includes("?") ||
    value.includes("#") ||
    !/^[a-zA-Z0-9:/.[\]-]+$/.test(value)
  ) {
    throw new Error(
      "Desktop team API origin must be a root HTTPS URL or explicit loopback HTTP URL.",
    );
  }
  return url.origin;
}

export function releaseTeamAPIOrigin(
  readConfig = () => readFileSync(new URL("../../.goreleaser.yml", import.meta.url), "utf8"),
) {
  const prefix = "- -X github.com/atomicobject/rhizome/pkg/teamapi/session.DefaultOrigin=";
  const values = readConfig()
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith(prefix))
    .map((line) => line.slice(prefix.length).trim());
  if (values.length !== 3 || new Set(values).size !== 1) {
    throw new Error("Release builds must define the same team API origin for all three platforms.");
  }
  return desktopTeamAPIOrigin({}, () => values[0]);
}

function productionOrigin() {
  try {
    return execFileSync(
      "gh",
      [
        "api",
        "repos/atomicobject/rhizome/environments/production/variables/TEAM_API_ORIGIN",
        "--jq",
        ".value",
      ],
      { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
    );
  } catch {
    throw new Error(
      "Cannot read the desktop team API default. Sign in with gh and grant access to the production TEAM_API_ORIGIN variable, or set RZM_DESKTOP_TEAM_API_ORIGIN explicitly for this build.",
    );
  }
}
