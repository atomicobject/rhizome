import assert from "node:assert/strict";
import { test } from "node:test";
import { desktopTeamAPIOrigin } from "./team-api-origin.mjs";

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
