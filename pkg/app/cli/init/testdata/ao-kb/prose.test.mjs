import assert from "node:assert/strict";
import test from "node:test";
import {
  inlineText,
  paragraphs,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/prose.ts";
import {
  collapseContradictions,
  makeResolver,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/relations.ts";

const resolve = makeResolver([
  { key: "concepts/cancellation.md", notePath: "concepts/cancellation.md", aoId: "cpt-1" },
  { key: "assertions/fee.md", notePath: "assertions/fee.md", aoId: "a-1" },
]);

test("claim paragraphs preserve prose and only interpret AO wikilinks", () => {
  const text =
    "First paragraph.\r\n\r\nContext: [[cancellation|cancellation policy]], [[a-1]] and [[missing|guardian policy]].\n\n<script>literal</script> **literal**";
  const blocks = paragraphs(text);
  assert.equal(blocks.length, 3);
  assert.deepEqual(inlineText(blocks[1], resolve), [
    { kind: "text", text: "Context: " },
    { kind: "link", label: "cancellation policy", path: "concepts/cancellation.md" },
    { kind: "text", text: ", " },
    { kind: "link", label: "a-1", path: "assertions/fee.md" },
    { kind: "text", text: " and " },
    { kind: "unresolved", label: "guardian policy", target: "missing" },
    { kind: "text", text: "." },
  ]);
  assert.deepEqual(inlineText(blocks[2], resolve), [{ kind: "text", text: blocks[2] }]);
  assert.deepEqual(inlineText("[[not closed", resolve), [{ kind: "text", text: "[[not closed" }]);
  assert.deepEqual(paragraphs(" \n\n "), []);
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
