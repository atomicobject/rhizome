import assert from "node:assert/strict";
import test from "node:test";
import { inlineText } from "../../templates/starters/ao-kb/module/views/knowledge/lib/prose.ts";
import {
  collapseContradictions,
  makeResolver,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/relations.ts";

const resolve = makeResolver([
  { key: "concepts/cancellation.md", notePath: "concepts/cancellation.md", aoId: "cpt-1" },
  { key: "assertions/fee.md", notePath: "assertions/fee.md", aoId: "a-1" },
]);

test("inline field values preserve prose and only interpret AO wikilinks", () => {
  const text =
    "Context: [[cancellation|cancellation policy]], [[a-1]] and [[missing|guardian policy]].";
  assert.deepEqual(inlineText(text, resolve), [
    { kind: "text", text: "Context: " },
    { kind: "link", label: "cancellation policy", path: "concepts/cancellation.md" },
    { kind: "text", text: ", " },
    { kind: "link", label: "a-1", path: "assertions/fee.md" },
    { kind: "text", text: " and " },
    { kind: "unresolved", label: "guardian policy", target: "missing" },
    { kind: "text", text: "." },
  ]);
  const literal = "<script>literal</script> **literal**";
  assert.deepEqual(inlineText(literal, resolve), [{ kind: "text", text: literal }]);
  assert.deepEqual(inlineText("[[not closed", resolve), [{ kind: "text", text: "[[not closed" }]);
});

test("record display collapses a symmetric contradiction without losing other directions", () => {
  const edge = (from, rel, to) => ({ from, rel, to, declaredBy: [] });
  const first = edge("fee", "contradicts", "free");
  assert.deepEqual(
    collapseContradictions([
      first,
      edge("free", "contradicts", "fee"),
      edge("fee", "supports", "free"),
      edge("free", "supports", "fee"),
    ]),
    [
      { edge: first, bothEnds: true },
      { edge: edge("fee", "supports", "free"), bothEnds: false },
      { edge: edge("free", "supports", "fee"), bothEnds: false },
    ],
  );
  assert.deepEqual(collapseContradictions([first]), [{ edge: first, bothEnds: false }]);
});
