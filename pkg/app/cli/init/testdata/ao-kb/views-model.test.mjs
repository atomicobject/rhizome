import assert from "node:assert/strict";
import test from "node:test";
import { owner, recordRef } from "../../templates/starters/ao-kb/module/views/knowledge/lib/refs.ts";
import {
  codePath,
  declarer,
  fieldRows,
  modelObservations,
  syncCompare,
  typeSpec,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/model.ts";

const str = (scalar) => ({ kind: "string", scalar });
const list = (...items) => ({ kind: "array", items: items.map(str) });
const obj = (entries) => ({
  kind: "object",
  entries: Object.entries(entries).map(([key, value]) => ({
    key,
    value: typeof value === "string" ? str(value) : value,
  })),
});
const FEATURES = "artifacts/_assets/_model/features.yaml";

test("endpoints open as canonical refs; positional records never guess one", () => {
  const keyed = recordRef({ notePath: FEATURES, kind: "EMBEDDED", fragment: null, nodeId: "record-v1-abc" });
  assert.equal(keyed.key, `${FEATURES}#record:v1:abc`);
  assert.equal(keyed.ref.fragment, "record:v1:abc");
  const given = recordRef({ notePath: FEATURES, kind: "EMBEDDED", fragment: "record:v1:def", nodeId: "record-v1-def" });
  assert.equal(given.key, `${FEATURES}#record:v1:def`);
  const positional = recordRef({ notePath: FEATURES, kind: "EMBEDDED", nodeId: "record-pos-v1-123" });
  assert.equal(positional.positional, true);
  assert.equal(positional.ref, undefined);
  assert.deepEqual(positional.host, { notePath: FEATURES, kind: "NOTE" });
  const section = owner({ notePath: "assertions/a.md", kind: "EMBEDDED", fragment: "connections-805", nodeId: "assertions/a.md#connections-805" });
  assert.equal(section.key, "assertions/a.md", "a Markdown section belongs to its note");
  assert.equal(owner({ notePath: "assertions/a.md", kind: "NOTE" }).key, "assertions/a.md");
});

const spec = typeSpec(
  {
    type: {
      label: "Use case",
      annotations: { source: { key: { field: "id" }, title: ["name", "t"] } },
      fields: [
        { name: "v_name", sourcePath: "name", annotations: { field: { path: "name" } } },
        { name: "v_screens", annotations: { field: { path: "screens" }, references: { to: ["AOKBModel_screen"], values: "ELEMENTS", edge: { name: "serves", direction: "INBOUND" } } } },
        { name: "v_user", annotations: { field: { path: "user" }, references: { to: ["AOKBModel_user"], values: "VALUE", edge: { name: "serves", direction: "OUTBOUND" } } } },
        { name: "v_next", annotations: { field: { path: "next" }, references: { to: ["AOKBModel_flow"], values: "ELEMENTS" } } },
        { name: "v_supported_5f_by", annotations: { field: { path: "supported_by" }, references: { to: ["Assertion"], targetKey: "aoId", values: "ELEMENTS", edge: { name: "evidenced-by", direction: "OUTBOUND" } } } },
      ],
    },
  },
  "AOKBModel_flow_single",
);

test("fields stay in authored order and each reference value states how it resolved", () => {
  assert.equal(spec.label, "Use case");
  const screen = { relation: "serves", out: false, key: "s#1", ref: {}, title: "Book screen", typeName: "AOKBModel_screen_single", aoValue: "SCR-1" };
  const twin = (key) => ({ relation: "serves", out: true, key, ref: {}, title: key, typeName: "AOKBModel_user_single", aoValue: "PAT" });
  const claim = { relation: "evidenced-by", out: true, key: "assertions/a.md", ref: {}, title: "A claim", typeName: "Assertion", aoValue: "a-1" };
  const rows = fieldRows(
    obj({ id: "FLOW-1", name: "Book", trigger: "Patient taps", screens: list("SCR-1", "SCR-gone", "*"), user: "PAT", next: list("FLOW-2"), supported_by: list("a-1", "a-missing"), implements: list("src/x.go") }),
    spec,
    [screen, twin("u#1"), twin("u#2"), claim],
  );
  assert.deepEqual(rows.map((r) => r.key), ["trigger", "screens", "user", "next", "supported_by"], "identity, title and code paths are shown elsewhere");
  assert.equal(rows[0].atoms, undefined, "a plain field is authored text");
  assert.deepEqual(rows[1].atoms.map((a) => a.target?.title ?? a.state), ["Book screen", "unresolved", "wildcard"]);
  assert.deepEqual(rows[2].atoms.map((a) => a.state), ["ambiguous"], "two records with one key: none chosen");
  assert.deepEqual(rows[3].atoms.map((a) => a.state), ["no edge"]);
  assert.deepEqual(rows[4].atoms.map((a) => a.target?.title ?? a.state), ["A claim", "unresolved"]);
});

test("the declaring end of a stored edge is the one whose authored value names the other", () => {
  const flow = { key: "flow", aoValue: "FLOW-1", value: obj({ id: "FLOW-1", screens: list("SCR-1") }) };
  const screen = { key: "screen", aoValue: "SCR-1", value: obj({ id: "SCR-1" }) };
  assert.equal(declarer(screen, flow), "flow", "an INBOUND field stores screen → flow, authored by the flow");
  const feature = { key: "feature", aoValue: "F-1", value: obj({ flows: list("FLOW-1") }) };
  assert.equal(declarer(feature, flow), "feature");
  assert.equal(declarer({ key: "a" }, { key: "b" }), "a", "unknown values keep the stored source");
});

test("code paths are exact and never escape the configured roots", () => {
  assert.equal(codePath(" src/booking.go "), "src/booking.go");
  for (const raw of ["/etc/passwd", "../other/x.go", "src/../../x.go", "https://example.com/x.go", "C:\\x.go", ""])
    assert.equal(codePath(raw), undefined, raw);
});

test("model sync groups by file, names each row and changed claim, and missing dates are unknown", () => {
  const claim = (path, aoId, updatedAt) => ({ path, aoId, updatedAt, title: path, ref: { notePath: path, kind: "NOTE" } });
  const changed = claim("assertions/changed.md", "a-1", "2026-11-04");
  const older = claim("assertions/older.md", "a-2", "2026-10-01");
  const undated = claim("assertions/undated.md", "a-3", null);
  const row = (key, notePath, evidence, supported) => ({
    key, ref: { notePath, kind: "EMBEDDED", fragment: key }, title: key, typeName: "AOKBModel_flow_single", aoValue: key,
    value: obj({ id: key, supported_by: list(...supported) }), evidence,
  });
  const rows = [
    row("F-1", FEATURES, [changed.path], ["a-1"]),
    row("F-2", FEATURES, [older.path], ["a-2"]),
    row("F-3", FEATURES, [undated.path, changed.path], ["a-3", "a-1", "a-gone"]),
    row("W-1", "flows.yaml", [changed.path], ["a-1"]),
  ];
  const result = modelObservations(
    [{ notePath: FEATURES, synced: "2026-10-20T09:00:00Z" }, { notePath: "flows.yaml", synced: null }],
    rows,
    { AOKBModel_flow_single: spec },
    [changed, older, undated],
  );
  assert.equal(result.sync.length, 1, "one entry per file, not per row");
  assert.deepEqual(result.sync[0].rows.map((r) => [r.record.key, r.changed.map((a) => a.aoId)]), [["F-1", ["a-1"]], ["F-3", ["a-1"]]]);
  assert.deepEqual(result.unknown.map((g) => [g.file.notePath, g.rows.map((r) => r.record.key)]), [[FEATURES, ["F-3"]], ["flows.yaml", ["W-1"]]]);
  assert.deepEqual(result.dangling.map((d) => [d.record.key, d.field, d.id]), [["F-3", "supported_by", "a-gone"]]);
  const capped = { ...row("F-4", FEATURES, [changed.path], ["a-1", "a-unread"]), evidenceTruncated: true };
  const partial = modelObservations([], [capped], { AOKBModel_flow_single: spec }, [changed]);
  assert.deepEqual(partial.dangling, [], "an ID beyond a capped evidence read is not called dangling");
  assert.deepEqual(partial.incomplete.map((r) => r.key), ["F-4"], "the capped read is disclosed");
  assert.equal(syncCompare("2026-11-04", "2026-11-04T00:00:00Z"), "not later");
  assert.equal(syncCompare("not a date", "2026-11-04"), "unknown");
});
