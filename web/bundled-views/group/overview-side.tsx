// The Overview's scope-only blocks (SPEC-0117 US4, US6). At All notes:
// untyped notes by folder and the coverage line. On a group: relations
// declared but unused, the notes outside the group linking to the most of its
// records, and the group's guide and views.
import { openSearch } from "@rhizome/kit";
import { Fragment, useEffect, useRef, useState, useMemo, type ReactNode } from "react";

import { boundary } from "./activity.ts";
import { LinkSummary, OutsideNotes } from "./overview-side-parts.tsx";
import { GuideAndViews } from "./overview-side-parts.tsx";
import { Block, BlockBody, ModelBlock, MoreToggle, Rebuilding } from "./briefing-parts.tsx";
import { useGroupModel, type GroupRead } from "./load.ts";
import { coverage, folderLines } from "./members.ts";
import { fieldLabel, type GroupModel } from "./model.ts";
import { useRememberedOpen } from "./remembered.ts";
import { declaredRelations, type ScopeModel } from "./scope.ts";
import {
  blockStatus,
  useAggregatePart,
  useScopeDocs,
  useTypeSummaries,
  type Read,
} from "./scope-load.ts";

const fmt = (count: number) => count.toLocaleString("en-US");

/** Folders listed before "+N more", so the block stays about as tall as the map beside it. */
const FOLDER_LINES = 8;

const linksText = (targets: readonly { label: string; count: number }[]) =>
  targets.map((target) => `${target.label} ${fmt(target.count)}`).join(", ") || undefined;

export function FolderBlock() {
  const folders = useAggregatePart("folders");
  const summaries = useTypeSummaries();
  const status = blockStatus([folders.read, summaries.read]);
  const facts = folders.data?.facts;
  // Its own preference: overview.expanded is keyed by group name.
  const expanded = useRememberedOpen("overview.folders");
  const open = expanded.isOpen("all", false);

  return (
    <Block
      title="Untyped notes by folder"
      count={
        facts && !facts.rebuilding
          ? `${fmt(facts.untypedNotes - facts.ambiguousNotes)} untyped + ${facts.ambiguousNotes} ambiguous`
          : undefined
      }
      info={{
        text: "Top-level folders by untyped notes. Activate one to search it.",
        display: "title",
      }}
      className="gv-slot-side"
    >
      <BlockBody status={status} what="folders">
        {() => {
          if (facts?.rebuilding) return <Rebuilding />;
          const lines = folderLines(folders.data?.folders ?? [], summaries.data ?? new Map());

          if (!lines.length) return <p className="gv-quiet">Every note has a type.</p>;
          const shown = open ? lines : lines.slice(0, FOLDER_LINES);

          return (
            <>
              <table className="gv-table gv-ov-folders">
                <thead>
                  <tr>
                    <th scope="col">Folder</th>
                    <th scope="col" className="gv-right">
                      Untyped
                    </th>
                    <th scope="col">Share</th>
                    <th scope="col">Untyped notes link to</th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((line) => {
                    const percent = Math.round(line.share * 100);

                    return (
                      <tr key={line.folder}>
                        <td>
                          <button
                            type="button"
                            className="gv-link gv-ov-folder"
                            // Notes at the vault root sit in no folder; "/" names the root.
                            onClick={() => openSearch({ folder: line.folder || "/" })}
                          >
                            {line.folder}/
                          </button>
                        </td>
                        <td className="gv-right gv-num">{fmt(line.untyped)}</td>
                        <td
                          title={`${fmt(line.untyped)} of ${fmt(line.total)} notes in ${line.folder}/`}
                        >
                          {line.share < 1 ? (
                            <span className="gv-ov-share">
                              <span className="gv-num num">{percent}%</span>
                              <span className="gv-ov-mini" data-kind="untyped" aria-hidden="true">
                                <i style={{ width: `${percent}%` }} />
                              </span>
                            </span>
                          ) : (
                            <span className="gv-faint">—</span>
                          )}
                        </td>
                        <td className="gv-ov-links" title={linksText(line.linksTo)}>
                          {line.linksTo.length ? (
                            <LinkSummary text={linksText(line.linksTo) ?? ""} />
                          ) : (
                            <span className="gv-faint">no typed notes</span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              {lines.length > FOLDER_LINES && (
                <MoreToggle
                  hidden={lines.length - FOLDER_LINES}
                  open={open}
                  onToggle={() => expanded.setOpen("all", !open)}
                />
              )}
            </>
          );
        }}
      </BlockBody>
    </Block>
  );
}

function CoverageItem({ term, children }: { term: string; children: ReactNode }) {
  return (
    <>
      <dt>{term}</dt>
      <dd>{children}</dd>
    </>
  );
}

/** Types the map leaves out or cannot explain: empty, unlinked, and embedded types. */
export function CoverageLine() {
  const members = useAggregatePart("members");
  const links = useAggregatePart("links");
  const summaries = useTypeSummaries();
  const status = blockStatus([members.read, links.read, summaries.read]);

  return (
    <Block title="Coverage" className="gv-slot-coverage">
      <BlockBody status={status} what="coverage">
        {() => {
          if (members.data?.facts.rebuilding) return <Rebuilding />;
          const figures = members.data?.members;

          if (!figures) return null;
          const found = coverage(figures, links.data?.links ?? [], summaries.data ?? new Map());

          if (!found.empty.length && !found.unlinked.length && !found.embedded.length)
            return <p className="gv-quiet">Every note type has records and links.</p>;

          return (
            <dl className="gv-ov-kv">
              {found.empty.length > 0 && (
                <CoverageItem term="No records">
                  {found.empty.map((entry) => entry.label).join(", ")}
                </CoverageItem>
              )}
              {found.unlinked.length > 0 && (
                <CoverageItem term="No links">
                  {found.unlinked.map((entry, index) => (
                    <Fragment key={entry.name}>
                      {index > 0 && ", "}
                      {entry.label} <span className="gv-num">{fmt(entry.count)}</span>
                    </Fragment>
                  ))}
                </CoverageItem>
              )}
              {found.embedded.length > 0 && (
                <CoverageItem term="Embedded">
                  {found.embedded.map((entry, index) => (
                    <Fragment key={entry.name}>
                      {index > 0 && ", "}
                      {entry.label} <span className="gv-num">{fmt(entry.count)}</span>
                    </Fragment>
                  ))}{" "}
                  <span className="gv-faint">· items inside notes, not drawn</span>
                </CoverageItem>
              )}
            </dl>
          );
        }}
      </BlockBody>
    </Block>
  );
}

/** Each member's relation fields toward another member that no record uses. */
export function DeclaredUnused({
  model,
  reads,
}: {
  model: ScopeModel | null;
  reads: readonly Read[];
}) {
  const links = useAggregatePart("links");
  const summaries = useTypeSummaries();
  const docs = useScopeDocs(model);
  const status = blockStatus([...reads, links.read, summaries.read, docs.read]);

  const unused = useMemo(
    () =>
      model && links.data?.links
        ? declaredRelations(model, docs.docs, links.data.links).filter(
            (relation) => relation.count === 0,
          )
        : [],
    [model, links.data, docs.docs],
  );

  const label = (id: string) => model?.nodeIndex.get(id)?.label ?? id;

  return (
    <Block
      title="Declared relations with no links"
      count={status.status === "ready" ? unused.length : undefined}
      info={`Relations the schema allows between members that no record uses. Schema identifiers: ${unused.map((relation) => `${relation.member}.${relation.field} → ${relation.target}`).join("; ")}`}
    >
      <BlockBody status={status} what="relations">
        {() =>
          unused.length ? (
            <>
              <table className="gv-table gv-unused-table">
                {model && model.nodes.some((node) => node.count === 0) && (
                  <caption className="gv-quiet">
                    {model.nodes
                      .filter((node) => node.count === 0)
                      .map((node) => `${node.label}: no records`)
                      .join(" · ")}
                  </caption>
                )}
                <thead>
                  <tr>
                    <th>Owner type</th>
                    <th>Field</th>
                    <th>Target type</th>
                  </tr>
                </thead>
                <tbody>
                  {unused.map((relation) => (
                    <tr key={`${relation.member}.${relation.field}.${relation.target}`}>
                      <td>{label(relation.member)}</td>
                      <td>{fieldLabel(relation.field)}</td>
                      <td>{label(relation.target)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : (
            <p className="gv-quiet">Records use every relation declared between members.</p>
          )
        }
      </BlockBody>
    </Block>
  );
}

/**
 * The neighbors of each member's newest records, at most 200 notes per member
 * however many records it has, plus their typed links and the guide to leave out.
 */
const OUTSIDE_READ: GroupRead = {
  scalars: false,
  perMember: 20,
  neighborCap: 10,
};

/** Outside notes shown before expanding the preview. */
const OVERVIEW_OUTSIDE_NOTES = 3;

/** Only the guide note. */
const GUIDE_READ: GroupRead = { records: false };

function GroupOutside({ model }: { model: GroupModel }) {
  const ref = useRef<HTMLDivElement>(null);
  const [limit, setLimit] = useState(OVERVIEW_OUTSIDE_NOTES);
  useEffect(() => {
    const element = ref.current;
    const main = element?.closest(".gv-ov")?.querySelector(".gv-ov-main");

    if (!element || !main || typeof ResizeObserver === "undefined") return;

    const measure = () => {
      if (main.getBoundingClientRect().height === 0) return;
      const available = main.getBoundingClientRect().bottom - element.getBoundingClientRect().top;
      setLimit(Math.max(0, Math.min(3, Math.floor((available - 155) / 64))));
    };

    const observer = new ResizeObserver(measure);
    observer.observe(main);

    if (element.previousElementSibling) observer.observe(element.previousElementSibling);
    measure();

    return () => observer.disconnect();
  }, []);

  const outside = useMemo(() => {
    const found = boundary(model);

    return { ...found, incomplete: found.incomplete || model.truncated };
  }, [model]);

  return (
    <div ref={ref} className="gv-overview-outside">
      <OutsideNotes
        title="Outside the group"
        outside={outside}
        model={model}
        info={`Notes that link to or from ${model.name} records, most connected first.`}
        empty="No notes outside the group link to or from its records."
        limit={limit}
      />
    </div>
  );
}

export function GroupOutsideSummary() {
  const state = useGroupModel({ views: false, read: OUTSIDE_READ });

  return (
    <ModelBlock state={state} title="Outside the group" what="outside notes">
      {(model) => <GroupOutside model={model} />}
    </ModelBlock>
  );
}

/** "Outside the group" and "Guide and views", each from its own read. */
export function GroupRecordsBlocks() {
  const guide = useGroupModel({ read: GUIDE_READ });

  return (
    <>
      <ModelBlock state={guide} title="Guide and views" what="the guide and views">
        {(model) => <GuideAndViews model={model} />}
      </ModelBlock>
    </>
  );
}
