import * as fs from "node:fs/promises";
import path from "node:path";
import { hash, splitSource } from "./inventory.mjs";
import { documentMetadata } from "./triggers.mjs";
import { findingsFor } from "./questions.mjs";
import { writeReport } from "./report.mjs";

// Recompose saved judgments without invoking Jev. Only files admitted by the
// original manifest and still allowed by current ignore rules are read.
export async function rebuildReport(rzm, output, options = {}) {
  output = path.resolve(output);
  try {
    await fs.stat(path.join(output, "running.lock"));
    throw new Error("Wait for the running audit before rebuilding its report");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  const lock = path.join(output, "running.lock");
  await fs.mkdir(lock);
  try {
    const manifest = JSON.parse(await fs.readFile(path.join(output, "manifest.json"), "utf8"));
    const summary = JSON.parse(await fs.readFile(path.join(output, "summary.json"), "utf8"));
    const root = await rzm.checkPaths({ paths: [{ path: manifest.root, isDir: true }] });
    if (!root.ok || root.payload?.paths?.[0]?.status !== "allowed" || root.payload.paths[0].path)
      throw new Error("Run from the audit vault root");
    const metadata = new Map();
    const currentChunks = new Set();
    const files = manifest.files.filter((f) => f.status === "inventoried");
    for (let start = 0; start < files.length; start += 500) {
      const page = files.slice(start, start + 500);
      const checks = await rzm.checkPaths({ paths: page.map((f) => ({ path: f.path })) });
      if (!checks.ok || checks.payload?.paths?.length !== page.length)
        throw new Error("Incomplete ignore check");
      for (let i = 0; i < page.length; i++) {
        const file = page[i],
          decision = checks.payload.paths[i];
        if (decision.input !== file.path) throw new Error("Ignore check correlation mismatch");
        if (decision.status !== "allowed") {
          metadata.set(file.path, { unavailable: true });
          continue;
        }
        try {
          const absolute = path.join(manifest.root, file.path);
          const stat = await fs.lstat(absolute);
          if (!stat.isFile() || stat.size > 1024 * 1024) {
            metadata.set(file.path, { unavailable: true });
            continue;
          }
          const content = await fs.readFile(absolute, "utf8");
          if (hash(content) === file.hash)
            for (const chunk of splitSource(file.path, file.kind, content))
              currentChunks.add(chunk.id);
          metadata.set(file.path, {
            ...documentMetadata(file.path, content),
            stale: hash(content) !== file.hash,
          });
        } catch {
          metadata.set(file.path, { unavailable: true });
        }
      }
    }
    const findings = [];
    let succeeded = 0,
      invalidated = 0;
    let selected;
    try {
      selected = new Set(
        JSON.parse(await fs.readFile(path.join(output, "selection.json"), "utf8")),
      );
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    for (const name of await fs.readdir(path.join(output, "checkpoints"))) {
      if (!name.endsWith(".json")) continue;
      const item = JSON.parse(await fs.readFile(path.join(output, "checkpoints", name), "utf8"));
      if (item.status !== "succeeded") continue;
      if (selected && !selected.has(item.packet.id)) continue;
      const { id, ...request } = item.packet;
      if (
        request.model !== summary.model ||
        hash({ questionVersion: summary.questionVersion, ...request }) !== id
      )
        continue;
      if (
        ![request.state.focus, ...request.state.candidates].every((source) =>
          currentChunks.has(source.id),
        )
      ) {
        invalidated++;
        continue;
      }
      succeeded++;
      findings.push(
        ...findingsFor(item.packet, item.response.answers).map((f) => ({
          ...f,
          packetId: item.packet.id,
        })),
      );
    }
    const result = await writeReport(
      output,
      {
        ...summary,
        recomposedAt: new Date().toISOString(),
        checkpointSuccessesRead: succeeded,
        invalidatedCheckpoints: invalidated,
      },
      findings,
      {
        metadata,
        exploration: options.exploration ?? summary.exploration ?? false,
        decisions: options.decisions,
        reviewLimit: options.reviewLimit ?? Number(process.env.AUDIT_REVIEW_LIMIT || 100),
      },
    );
    return {
      output,
      checkpointSuccessesRead: succeeded,
      invalidatedCheckpoints: invalidated,
      ...result.summary,
    };
  } finally {
    await fs.rmdir(lock);
  }
}
