import { useEffect, useRef, useState } from "react";
import { openNote, openSearch, openView, typeLabel } from "@rhizome/kit";
import { type Boundary } from "./activity.ts";
import { readableTitle } from "./briefing-activity.tsx";
import { Block, MoreToggle, lower, plural } from "./briefing-parts.tsx";
import { fieldLabel, type GroupModel } from "./model.ts";
import { useRememberedOpen } from "./remembered.ts";

const folderOf = (path: string) => path.split("/").slice(0, -1).join("/");

/** A note type's label, lowercased and counted. */
function typeName(model: GroupModel, type: string, count: number) {
  const labels = model.typeLabels.get(type);

  return lower(labels ? typeLabel(labels, { count }) : fieldLabel(type));
}

/** A label at the start of a line, in sentence case: "Technical spec". */
const sentence = (label: string) => label.charAt(0).toUpperCase() + label.slice(1);

/** A note type's label outside the records, or "untyped note". */
function outsideLabel(model: GroupModel, type: string | null, count: number) {
  return type === null
    ? plural(count, "untyped note", "untyped notes")
    : typeName(model, type, count);
}

/**
 * Notes outside the records that link with them, as `boundary()` finds them:
 * counts by type, then the most connected notes.
 */
export function OutsideNotes({
  model,
  outside,
  title,
  info,
  empty,
  limit = 3,
}: {
  model: GroupModel;
  outside: Boundary;
  title: string;
  info: string;
  empty: string;
  /** Notes listed before "Show N more". */
  limit?: number;
}) {
  const expanded = useRememberedOpen("briefing.expanded");
  const open = expanded.isOpen("outside", false);
  const setOpen = (next: boolean) => expanded.setOpen("outside", next);
  const shown = open ? outside.notes : outside.notes.slice(0, limit);

  return (
    <Block
      title={title}
      count={`${outside.incomplete ? "at least " : ""}${outside.notes.length} ${plural(outside.notes.length, "note", "notes")}`}
      info={{
        text: `${info}${
          outside.incomplete
            ? " More notes may link to this group than are listed. Viewed records and their links are limited."
            : ""
        }`,
        display: "title",
      }}
    >
      {outside.byType.length > 0 && (
        <p className="gv-outsum">
          {outside.byType.map((entry) => (
            <span key={entry.type ?? ""}>
              <b>{entry.count}</b> {outsideLabel(model, entry.type, entry.count)}
            </span>
          ))}
        </p>
      )}
      {outside.notes.length === 0 && <p className="gv-quiet">{empty}</p>}
      <ul className="gv-list gv-changes">
        {shown.map((note) => {
          // A timestamped file name reads by its slug; the file name stays as evidence.
          const readable = readableTitle(note.title);

          return (
            <li key={note.path} className="gv-change">
              <span className="gv-change-main">
                <button
                  type="button"
                  className="gv-inline gv-change-title"
                  title={note.path}
                  onClick={() => openNote(note.path)}
                >
                  {readable ?? note.title}
                </button>
                <span className="gv-change-meta">
                  {sentence(outsideLabel(model, note.type, 1))}
                  {(readable || folderOf(note.path)) && (
                    <>
                      {" · "}
                      <span className="gv-path">
                        {readable ? note.path : `${folderOf(note.path)}/`}
                      </span>
                    </>
                  )}
                </span>
              </span>
              <span className="gv-change-age">
                {note.touches.length}
                <span className="sr-only">
                  {" "}
                  linked {plural(note.touches.length, "record", "records")}
                </span>
              </span>
            </li>
          );
        })}
      </ul>
      <div className="gv-outside-actions">
        {outside.notes.length > limit &&
          (!open && limit === 0 ? (
            <button type="button" className="gv-link" onClick={() => setOpen(true)}>
              Show {outside.notes.length} {plural(outside.notes.length, "note", "notes")}
            </button>
          ) : (
            <MoreToggle
              hidden={outside.notes.length - limit}
              open={open}
              onToggle={() => setOpen(!open)}
            />
          ))}
        {outside.incomplete && (
          <>
            {outside.notes.length > limit && <span aria-hidden="true"> · </span>}
            <button type="button" className="gv-link" onClick={() => openSearch({ folder: "" })}>
              Search all notes
            </button>
          </>
        )}
      </div>
    </Block>
  );
}

export function GuideAndViews({ model }: { model: GroupModel }) {
  const { guide, views } = model;

  return (
    <Block title="Guide and views" className="gv-overview-guide">
      {!guide && !views.length && (
        <span className="gv-quiet">No guide note or authored views.</span>
      )}
      {guide && (
        <>
          <button
            type="button"
            className="gv-link"
            title={guide.summary || undefined}
            onClick={() => openNote(guide.path)}
          >
            {guide.title}
          </button>
          <span className="gv-sub">
            Guide shared by {guide.sharedBy} of {model.members.length}{" "}
            {plural(model.members.length, "type", "types")}
          </span>
        </>
      )}
      {views.map((view) => (
        <button
          key={view.id}
          type="button"
          className="gv-link"
          title={view.description || undefined}
          onClick={() => openView(view.id, { kind: "group", group: model.name })}
        >
          {view.name}
        </button>
      ))}
    </Block>
  );
}

export function fitSummary(text: string, width: number, measure: (value: string) => number) {
  if (measure(text) <= width) return text;
  const words = text.split(" ");

  while (words.length && measure(words.join(" ") + "…") > width) words.pop();

  return words.join(" ") + "…";
}

/** Keep complete words visible; the full relationship summary remains in the tooltip. */
export function LinkSummary({ text }: { text: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const [shown, setShown] = useState(text);

  useEffect(() => {
    const element = ref.current;

    if (!element) return;

    const measure = () => {
      const probe = document.createElement("span");
      probe.style.cssText = "position:absolute;visibility:hidden;white-space:nowrap";
      element.append(probe);

      const next = fitSummary(text, element.clientWidth, (value) => {
        probe.textContent = value;

        return probe.getBoundingClientRect().width;
      });

      probe.remove();
      setShown(next);
    };

    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    void document.fonts?.ready.then(measure);

    return () => observer.disconnect();
  }, [text]);

  return (
    <span ref={ref} className="gv-ov-links-text" title={text}>
      {shown}
    </span>
  );
}
