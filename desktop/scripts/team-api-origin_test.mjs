import assert from "node:assert/strict";
import { test } from "node:test";
import { desktopTeamAPIOrigin, releaseTeamAPIOrigin } from "./team-api-origin.mjs";

test("desktop builds discover and canonicalize the configured server", () => {
  assert.equal(
    desktopTeamAPIOrigin({}, () => "https://Team.Example.test:443/\n"),
    "https://team.example.test",
  );
});

test("an explicit local build needs no GitHub access", () => {
  assert.equal(
    desktopTeamAPIOrigin({ RZM_DESKTOP_TEAM_API_ORIGIN: "http://127.0.0.1:8787" }, () => {
      throw new Error("must not discover a server for an explicit build");
    }),
    "http://127.0.0.1:8787",
  );
});

test("discovery cannot silently fall back to a fake or missing gateway", () => {
  for (const value of ["", "http://127.0.0.1:8787", "http://team.example.test"]) {
    assert.throws(() => desktopTeamAPIOrigin({}, () => value));
  }
});

test("build origins reject paths, credentials and linker or shell syntax", () => {
  for (const value of [
    "https://team.example.test/api",
    "https://user:secret@team.example.test",
    "https://team.example.test?x=1",
    "https://team.example.test#",
    'https://team.example.test/"',
    "https://$(example).test",
    "https://team.example.test\\path",
  ]) {
    assert.throws(() => desktopTeamAPIOrigin({ RZM_DESKTOP_TEAM_API_ORIGIN: value }));
  }
});

test("candidate defaults come from matching checked-in release origins", () => {
  const line = "  - -X github.com/atomicobject/rhizome/pkg/teamapi/session.DefaultOrigin=";
  const config = Array(3).fill(`${line}https://Canonical.Example.test:443/`).join("\r\n");
  assert.equal(
    releaseTeamAPIOrigin(() => config),
    "https://canonical.example.test",
  );
  assert.equal(
    desktopTeamAPIOrigin({}, () => releaseTeamAPIOrigin(() => config)),
    "https://canonical.example.test",
  );
  for (const invalid of [
    "",
    config.split("\r\n").slice(0, 2).join("\n"),
    config.replace("Canonical.Example.test", "other.example.test"),
    Array(3).fill(`${line}http://127.0.0.1:8787`).join("\n"),
    Array(3).fill(`${line}https://canonical.example.test/path`).join("\n"),
  ]) {
    assert.throws(() => releaseTeamAPIOrigin(() => invalid));
  }
});

test("candidate manual overrides do not read release configuration or protected settings", () => {
  assert.equal(
    desktopTeamAPIOrigin({ RZM_DESKTOP_TEAM_API_ORIGIN: "https://manual.example.test" }, () => {
      throw new Error("must not discover an origin for an explicit candidate override");
    }),
    "https://manual.example.test",
  );
});
