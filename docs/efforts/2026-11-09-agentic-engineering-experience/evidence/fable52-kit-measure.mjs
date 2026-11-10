import path from "node:path";
import { gzipSync } from "node:zlib";
import { createHash } from "node:crypto";
import { pathToFileURL } from "node:url";
const roots = { before: path.resolve(process.argv[2]), after: path.resolve(process.argv[3]) };
const { build } = await import(
  pathToFileURL(path.join(roots.after, "web/node_modules/vite/dist/node/index.js")).href
);
const rows = [];
for (let round = 0; round < 5; round++)
  for (const label of ["before", "after"]) {
    const root = roots[label];
    process.chdir(path.join(root, "web"));
    const result = await build({
      configFile: path.join(root, "web/vite.kit.config.ts"),
      logLevel: "silent",
    });
    const outputs = (Array.isArray(result) ? result : [result]).flatMap((r) => r.output);
    const chunks = new Map(outputs.filter((c) => c.type === "chunk").map((c) => [c.fileName, c]));
    const closure = new Set();
    const add = (name) => {
      if (closure.has(name)) return;
      closure.add(name);
      for (const imported of chunks.get(name)?.imports ?? [])
        if (chunks.has(imported)) add(imported);
    };
    add("kit.js");
    const files = [...closure].sort().map((n) => chunks.get(n));
    const modules = files.flatMap((c) => Object.keys(c.modules));
    rows.push({
      label,
      round,
      entryBytes: Buffer.byteLength(chunks.get("kit.js").code),
      entryGzip: gzipSync(chunks.get("kit.js").code).length,
      staticBytes: files.reduce((n, c) => n + Buffer.byteLength(c.code), 0),
      staticGzip: files.reduce((n, c) => n + gzipSync(c.code).length, 0),
      markdownModules: modules.filter((m) =>
        /\/(?:remark[^/]*|micromark[^/]*|mdast-util[^/]*|react-markdown|unified)\//.test(m),
      ).length,
      hash: createHash("sha256")
        .update(files.map((c) => c.code).join(""))
        .digest("hex"),
    });
  }
console.log(
  JSON.stringify(
    { claim: "Static module bytes, identical dependencies; no latency claim.", rows },
    null,
    2,
  ),
);
