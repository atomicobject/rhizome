// Artifacts (SPEC-0135 R2a): every artifact, by folder, from the orientation query.
import { useEffect } from "react";
import { SectionBody, sectionStatus, useMarkReady, useViewParams } from "@rhizome/kit";
import { folderCounts, folderOf } from "./lib/site.ts";
import type { Site } from "./lib/site.ts";
import { cmp } from "./lib/kinds.ts";
import { FolderFilter, Incomplete, ListHead, RecordTitle, label, useSite } from "./site/data.tsx";
import "./knowledge.css";

function ArtifactsBody({ site, folder }: { site: Site; folder: string }) {
  const rows = site.artifacts
    .filter((a) => !folder || folderOf(a.path) === folder)
    .sort((a, b) => cmp(label(a), label(b)) || cmp(a.path, b.path));
  return (
    <>
      <ListHead
        title="Artifacts"
        count={rows.length}
        about="Deliverables the KB records, by artifact folder."
      />
      <Incomplete site={site} />
      <FolderFilter
        view="ao-kb.artifacts"
        label="Artifact folder"
        folders={folderCounts(site.artifacts.map((a) => a.path))}
        current={folder}
      />
      {rows.length ? (
        <ul className="ak-rows">
          {rows.map((a) => (
            <li key={a.path}>
              <RecordTitle site={site} path={a.path} />
              <span className="ak-path">{a.path}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="ak-empty">No artifact {folder ? "in this folder" : "recorded"}.</p>
      )}
    </>
  );
}

export default function Artifacts() {
  const { query, site } = useSite(),
    params = useViewParams(),
    markReady = useMarkReady();
  useEffect(() => {
    if (site) markReady();
  }, [site, markReady]);
  const folder = typeof params.recordFolder === "string" ? params.recordFolder : "";
  return (
    <main className="ak-page ak-site">
      <SectionBody status={sectionStatus(query)} what="artifacts">
        {() => site && <ArtifactsBody site={site} folder={folder} />}
      </SectionBody>
    </main>
  );
}
