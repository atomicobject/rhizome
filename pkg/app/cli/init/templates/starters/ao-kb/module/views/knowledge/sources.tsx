// Sources (SPEC-0135 R2a): every source, by folder or gap, with its date and the basis of that date,
// and its recorded extraction, from the orientation query.
import { useEffect } from "react";
import { SectionBody, sectionStatus, useMarkReady, useViewParams } from "@rhizome/kit";
import { GAPS, folderCounts, folderOf, gap } from "./lib/site.ts";
import type { Site } from "./lib/site.ts";
import { cmp } from "./lib/kinds.ts";
import {
  FolderFilter,
  Incomplete,
  ListHead,
  ListLink,
  RecordTitle,
  label,
  useSite,
} from "./site/data.tsx";
import "./knowledge.css";

const BASIS = { recorded: "Recorded date", file: "Date in file name", none: "No recorded date" };

function SourcesBody({ site, folder, gapId }: { site: Site; folder: string; gapId: string }) {
  const g = gapId ? gap(site, gapId) : undefined;
  const known = !gapId || GAPS.some((d) => d.id === gapId && d.list === "sources");
  const pool = g ? new Set(g.items) : null;
  const rows = site.sources
    .filter(
      (s) => (!folder || folderOf(s.path) === folder) && (!gapId || (known && pool!.has(s.path))),
    )
    .sort((a, b) => cmp(label(a), label(b)) || cmp(a.path, b.path));
  return (
    <>
      <ListHead
        title="Sources"
        count={rows.length}
        about={
          g
            ? `${g.label}. ${g.rule}`
            : !known
              ? `No source gap is named ${gapId}.`
              : "Each source with the basis of its date and its recorded extraction."
        }
      />
      <Incomplete site={site} />
      {gapId && (
        <p className="ak-filter-note">
          Showing one gap.{" "}
          <ListLink view="ao-kb.sources" params={folder ? { recordFolder: folder } : undefined}>
            Show every source
          </ListLink>
        </p>
      )}
      <FolderFilter
        view="ao-kb.sources"
        label="Source folder"
        folders={folderCounts(site.sources.map((s) => s.path))}
        current={folder}
        keep={gapId ? { gap: gapId } : undefined}
      />
      {rows.length ? (
        <div className="ak-table-wrap">
          <table className="ak-table">
            <thead>
              <tr>
                <th scope="col">Source</th>
                <th scope="col">Date</th>
                <th scope="col">Recorded extraction</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => {
                const d = site.dates.get(s.path)!,
                  x = site.extraction.get(s.path)!;
                return (
                  <tr key={s.path}>
                    <td>
                      <RecordTitle site={site} path={s.path} />
                      <span className="ak-path">{s.path}</span>
                    </td>
                    <td data-label="Date">
                      {d.date ?? "No date"}
                      <span className="ak-meta">
                        {d.unreadable
                          ? `${BASIS[d.basis]}; unreadable recorded date`
                          : BASIS[d.basis]}
                      </span>
                    </td>
                    <td data-label="Recorded extraction">
                      {x.resolved.length + x.unresolved.length
                        ? `${x.resolved.length} resolved`
                        : "None recorded"}
                      {x.unresolved.length > 0 && (
                        <span className="ak-meta">
                          {x.unresolved.length} resolve to no assertion
                        </span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="ak-empty">No source matches.</p>
      )}
    </>
  );
}

export default function Sources() {
  const { query, site } = useSite(),
    params = useViewParams(),
    markReady = useMarkReady();
  useEffect(() => {
    if (site) markReady();
  }, [site, markReady]);
  const folder = typeof params.recordFolder === "string" ? params.recordFolder : "";
  const gapId = typeof params.gap === "string" ? params.gap : "";
  return (
    <main className="ak-page ak-site">
      <SectionBody status={sectionStatus(query)} what="sources">
        {() => site && <SourcesBody site={site} folder={folder} gapId={gapId} />}
      </SectionBody>
    </main>
  );
}
