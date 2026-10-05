import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(__dirname, "..");

const ignoredSuffixes = [".test.ts", ".test.tsx", ".d.ts", path.join("api", "generated.ts")];

const apiLiteralPattern = /["'`]((?:\/api\/)[^"'`\s)$}]+)/g;

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const fullPath = path.join(dir, entry);
    const stat = statSync(fullPath);

    if (stat.isDirectory()) return walk(fullPath);

    return stat.isFile() ? [fullPath] : [];
  });
}

function normalizeApiLiteral(literal: string) {
  return literal.split(/[?#]/, 1)[0];
}

function isPublicOrInternalAgentRoute(route: string) {
  return (
    route === "/api/v2/validate" ||
    route === "/api/v2/validate/refresh" ||
    route === "/api/v1" ||
    route.startsWith("/api/v1/")
  );
}

function isInternalAgentRoute(route: string) {
  return route === "/api/agent" || route.startsWith("/api/agent/");
}

describe("frontend API boundary", () => {
  it("rejects every unversioned non-agent API call", () => {
    const violations = walk(srcRoot).flatMap((file) => {
      if (!/\.(ts|tsx)$/.test(file)) return [];

      if (ignoredSuffixes.some((suffix) => file.endsWith(suffix))) return [];

      if (file.includes(`${path.sep}assets${path.sep}`)) return [];

      const relative = path.relative(srcRoot, file);
      const source = readFileSync(file, "utf8");

      return Array.from(source.matchAll(apiLiteralPattern)).flatMap((match) => {
        const route = normalizeApiLiteral(match[1]);

        if (isPublicOrInternalAgentRoute(route) || isInternalAgentRoute(route)) {
          return [];
        }

        return [`${relative}: ${route}`];
      });
    });

    expect(violations).toEqual([]);
  });
});
