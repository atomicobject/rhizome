import { publicTypeName } from "../lib/typeNames";
import { useEffect, useMemo, useRef, useState } from "react";

import { diffOntologyEditSession } from "../api/publicClient";
import type {
  ModifiedNoteEntry,
  ModifiedNoteOpView,
  ModifiedNotesResponse,
  OntologyEditOp,
  OntologyEditSessionResponse,
} from "../api/types";
import { normalizeNotePath } from "./notesRoute";
import { relationDisplayValue } from "./ConfiguredTableCellReadOnly";
import { UnifiedDiff } from "./UnifiedDiff";

type Props = {
  session: OntologyEditSessionResponse | null;
  onOpenNote: (path: string) => void;
  onReplaceOps: (ops: OntologyEditOp[]) => void;
};

const OP_LABEL = new Map([
  ["setField", "Field"],
  ["setLinkField", "Links"],
  ["setNarrative", "Narrative"],
  ["setSource", "Source"],
  ["insertNarrative", "Narrative"],
  ["addEmbeddedNode", "Add"],
  ["deleteNode", "Delete"],
  ["reorderCollection", "Reorder"],
  ["appendAlias", "Alias"],
  ["setFrontmatter", "Frontmatter"],
  ["rewriteBrokenLink", "Retarget"],
  ["addSectionScaffold", "Section"],
]);

function plural(count: number, word: string): string {
  return `${count} ${word}${count === 1 ? "" : "s"}`;
}

function refLocator(op: ModifiedNoteOpView): string {
  const ref = op.nodeRef;

  if (!ref) return "";

  if (ref.fragment) return `#${ref.fragment}`;

  return "";
}

function snippet(value: string | undefined, limit = 80): string {
  if (!value) return "";
  const trimmed = value.trim().replace(/\s+/g, " ");

  if (trimmed.length <= limit) return trimmed;

  return `${trimmed.slice(0, limit - 1)}…`;
}

function renderFragments(previous: string[] | undefined, next: string[] | undefined) {
  const prevSet = new Set(previous || []);
  const nextSet = new Set(next || []);
  const added = (next || []).filter((frag) => !prevSet.has(frag));
  const removed = (previous || []).filter((frag) => !nextSet.has(frag));

  return { added, removed };
}

function fieldWitnessForConflict(
  op: OntologyEditOp,
  currentValues: string[] | undefined,
  currentValueKind: "unset" | "scalar" | "list" | undefined,
) {
  const values = currentValues || [];

  if (currentValueKind === "list") return { kind: "list" as const, items: values };

  if (currentValueKind === "unset") return { kind: "unset" as const };

  return { kind: "scalar" as const, scalar: values[0] || "" };
}

function DiscardButton({
  label,
  className,
  onClick,
}: {
  label: string;
  className: string;
  onClick: () => void;
}) {
  return (
    <button type="button" className={className} aria-label={label} title={label} onClick={onClick}>
      ×
    </button>
  );
}

/** Names the item inside the note that an op changes, such as a checkbox. */
function OpTarget({ op }: { op: ModifiedNoteOpView }) {
  if (!op.nodeTitle) return null;

  return (
    <span className="changes-op__hint" title={op.nodeTitle}>
      {snippet(op.nodeTitle, 48)}
    </span>
  );
}

function OpBody({ op }: { op: ModifiedNoteOpView }) {
  switch (op.kind) {
    case "setField": {
      return (
        <div className="changes-op__body">
          <OpTarget op={op} />
          <span className="changes-op__field">{op.field}</span>
          <span className="changes-op__old">
            {op.previousValue ? snippet(op.previousValue) : "—"}
          </span>
          <span className="changes-op__arrow">→</span>
          <span className="changes-op__new">{op.value ? snippet(op.value) : "—"}</span>
        </div>
      );
    }

    case "setLinkField": {
      // Same shape as a field change: the old links struck through, then the new ones.
      const links = (values: string[] | undefined) =>
        (values ?? []).map(relationDisplayValue).join(", ");

      return (
        <div className="changes-op__body">
          <OpTarget op={op} />
          <span className="changes-op__field">{op.field}</span>
          <span className="changes-op__old">{snippet(links(op.previousValues)) || "—"}</span>
          <span className="changes-op__arrow">→</span>
          <span className="changes-op__new">{snippet(links(op.values)) || "—"}</span>
        </div>
      );
    }

    case "setNarrative":
    case "insertNarrative": {
      return (
        <div className="changes-op__body changes-op__body--stack">
          {op.previousMarkdown && (
            <pre className="changes-op__block changes-op__block--old">{op.previousMarkdown}</pre>
          )}
          {op.markdown && <pre className="changes-op__block">{op.markdown}</pre>}
        </div>
      );
    }

    case "setSource": {
      return (
        <div className="changes-op__body">
          <span className="changes-op__new">whole file replaced</span>
          <span className="changes-op__hint">review the unified diff before saving</span>
        </div>
      );
    }

    case "addEmbeddedNode": {
      return (
        <div className="changes-op__body changes-op__body--stack">
          <span className="changes-op__field">{op.collection}</span>
          {op.heading && <span className="changes-op__new">{op.heading}</span>}
          {op.body && <pre className="changes-op__block">{snippet(op.body, 200)}</pre>}
        </div>
      );
    }

    case "deleteNode": {
      return (
        <div className="changes-op__body">
          <span className="changes-op__field">
            {op.nodeTitle ? snippet(op.nodeTitle) : refLocator(op) || "node"}
          </span>
        </div>
      );
    }

    case "reorderCollection": {
      const { added, removed } = renderFragments(op.previousFragments, op.orderedFragments);

      const moved =
        added.length === 0 && removed.length === 0 && (op.orderedFragments?.length || 0) > 0;

      return (
        <div className="changes-op__body">
          <span className="changes-op__field">{op.collection}</span>
          <span className="changes-op__hint">
            {moved ? "order changed" : `${added.length} added · ${removed.length} removed`}
          </span>
        </div>
      );
    }

    case "appendAlias": {
      return (
        <div className="changes-op__body">
          <span className="changes-op__field">aliases</span>
          <span className="changes-op__chip changes-op__chip--add">+{snippet(op.value, 60)}</span>
        </div>
      );
    }

    case "setFrontmatter": {
      const property = op.property || op.field || "";
      const hasList = (op.values?.length ?? 0) > 0;

      return (
        <div className="changes-op__body">
          <span className="changes-op__field">{property}</span>
          <span className="changes-op__new">
            {hasList ? snippet((op.values || []).join(", "), 100) : snippet(op.value, 100) || "—"}
          </span>
        </div>
      );
    }

    case "rewriteBrokenLink": {
      return (
        <div className="changes-op__body">
          <span className="changes-op__old">[[{snippet(op.oldTarget, 40)}]]</span>
          <span className="changes-op__arrow">→</span>
          <span className="changes-op__new">[[{snippet(op.newTarget, 40)}]]</span>
        </div>
      );
    }

    case "addSectionScaffold": {
      const level = (op.level || "").toUpperCase();

      return (
        <div className="changes-op__body">
          {level && <span className="changes-op__field">{level}</span>}
          <span className="changes-op__new">{snippet(op.value || op.heading, 80)}</span>
        </div>
      );
    }

    default:
      return <div className="changes-op__body" />;
  }
}

function NoteSection({
  entry,
  onOpen,
  onDiscardFile,
  onDiscardOp,
}: {
  entry: ModifiedNoteEntry;
  onOpen: (path: string) => void;
  onDiscardFile: (path: string) => void;
  onDiscardOp: (path: string, indexAmongPath: number, operationId?: string) => void;
}) {
  const ops = entry.ops || [];
  const containsWholeFileEdit = ops.some((op) => op.kind === "setSource");
  const [diffOpen, setDiffOpen] = useState(containsWholeFileEdit);

  return (
    <section className="changes-note">
      <div className="changes-note__head">
        <button type="button" className="changes-note__title" onClick={() => onOpen(entry.path)}>
          <span className="changes-note__name">{entry.title || entry.path}</span>
          <span className="changes-note__path">{entry.path}</span>
        </button>
        <div className="changes-note__meta">
          {entry.rebased && (
            <span
              className="changes-note__flag"
              title="The note changed on disk after these edits were staged; they were replayed onto the current version."
            >
              rebased
            </span>
          )}
          {publicTypeName(entry.resolvedType) && (
            <span className="changes-note__type">{publicTypeName(entry.resolvedType)}</span>
          )}
          <DiscardButton
            className="changes-note__discard"
            label={`Discard all changes to ${entry.path}`}
            onClick={() => onDiscardFile(entry.path)}
          />
        </div>
      </div>

      {ops.map((op, idx) => (
        <div
          key={`${op.kind}:${op.nodeRef?.notePath ?? ""}:${op.nodeRef?.fragment ?? ""}:${op.field ?? op.collection ?? idx}`}
          className="changes-op"
        >
          <span className="changes-op__kind">{OP_LABEL.get(op.kind) ?? op.kind}</span>
          <OpBody op={op} />
          <DiscardButton
            className="changes-op__discard"
            label="Discard this change"
            onClick={() => onDiscardOp(entry.path, idx, op.id)}
          />
        </div>
      ))}

      {entry.diff && (
        <details
          className="changes-note__diff"
          open={diffOpen}
          onToggle={(event) => setDiffOpen(event.currentTarget.open)}
        >
          <summary>Diff</summary>
          {diffOpen && <UnifiedDiff diff={entry.diff} />}
        </details>
      )}
    </section>
  );
}

export function NotesModifiedHome({ session, onOpenNote, onReplaceOps }: Props) {
  const sessionId = session?.sessionId ?? "";

  const snapshot = useMemo(() => {
    if (!session?.sessionId) return undefined;

    return {
      version: 3,
      revision: session.revision || 0,
      sessionId: session.sessionId,
      ops: session.ops || [],
      baseFingerprints: session.baseFingerprints || {},
      baseDocuments: session.baseDocuments || [],
    };
  }, [session]);

  const [data, setData] = useState<ModifiedNotesResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const latestRequest = useRef(0);

  useEffect(() => {
    if (!sessionId || !snapshot) {
      setData(null);

      return;
    }

    const ticket = ++latestRequest.current;
    setLoading(true);
    setError(null);
    diffOntologyEditSession(sessionId, snapshot)
      .then((response) => {
        if (latestRequest.current !== ticket) return;
        setData(response);
      })
      .catch((err) => {
        if (latestRequest.current !== ticket) return;
        setError(err instanceof Error ? err.message : "Failed to load diff");
      })
      .finally(() => {
        if (latestRequest.current !== ticket) return;
        setLoading(false);
      });
  }, [sessionId, snapshot]);

  const totals = data?.totals;
  const notes = data?.notes ?? [];
  const conflicts = data?.conflicts ?? [];

  const fieldCount = (totals?.setField ?? 0) + (totals?.setLinkField ?? 0);

  const status =
    notes.length === 0
      ? "Nothing staged"
      : [
          plural(notes.length, "note"),
          plural(totals?.ops ?? 0, "change"),
          totals?.setNarrative ? `${totals.setNarrative} narrative` : "",
          fieldCount ? plural(fieldCount, "field") : "",
        ]
          .filter(Boolean)
          .join(" · ");

  return (
    <section className="changes-workspace" aria-label="Modified home">
      <header className="changes-workspace__header">
        <p className="changes-workspace__meta">staged edits</p>
        <h2>Changes</h2>
        <p className="changes-workspace__status">
          {status}
          {conflicts.length > 0 && (
            <span className="changes-workspace__status-conflicts">
              {` · ${plural(conflicts.length, "conflict")}`}
            </span>
          )}
        </p>
      </header>

      {!loading && !error && data && notes.length === 0 && (
        <p className="changes-workspace__empty">
          Edits you stage in notes and views collect here for review before you save them.
        </p>
      )}

      {loading && (
        <p className="changes-workspace__notice" role="status">
          Computing diff…
        </p>
      )}
      {error && (
        <p className="changes-workspace__notice changes-workspace__notice--error" role="alert">
          {error}
        </p>
      )}

      {conflicts.length > 0 && (
        <div className="changes-conflicts" role="alert">
          <h3>Conflicts</h3>
          <ul>
            {conflicts.map((conflict) => (
              <li
                key={`${conflict.notePath}:${conflict.nodeRef ?? ""}:${conflict.kind}:${conflict.message}`}
              >
                <strong>{conflict.notePath}</strong>
                <span>{conflict.message}</span>
                {conflict.operationId ? (
                  <span className="changes-conflicts__actions">
                    <button
                      type="button"
                      onClick={() =>
                        onReplaceOps(
                          (session?.ops || []).filter((op) => op.id !== conflict.operationId),
                        )
                      }
                    >
                      Keep current
                    </button>
                    {conflict.kind === "FIELD_CHANGED" ? (
                      <button
                        type="button"
                        onClick={() =>
                          onReplaceOps(
                            (session?.ops || []).map((op) =>
                              op.id === conflict.operationId
                                ? {
                                    ...op,
                                    expected: {
                                      ...op.expected,
                                      field: fieldWitnessForConflict(
                                        op,
                                        conflict.currentValues,
                                        conflict.currentValueKind,
                                      ),
                                    },
                                  }
                                : op,
                            ),
                          )
                        }
                      >
                        Keep mine
                      </button>
                    ) : null}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      )}

      {notes.map((entry) => (
        <NoteSection
          key={entry.path}
          entry={entry}
          onOpen={onOpenNote}
          onDiscardFile={(path) => {
            const current = session?.ops ?? [];
            const canonicalPath = normalizeNotePath(path.split("#", 1)[0] || path);
            onReplaceOps(
              current.filter(
                (op) => normalizeNotePath(op.path.split("#", 1)[0] || op.path) !== canonicalPath,
              ),
            );
          }}
          onDiscardOp={(path, indexAmongPath, operationId) => {
            const current = session?.ops ?? [];

            if (operationId) {
              onReplaceOps(current.filter((op) => op.id !== operationId));

              return;
            }

            const canonicalPath = normalizeNotePath(path.split("#", 1)[0] || path);
            let seen = 0;

            const next = current.filter((op) => {
              if (normalizeNotePath(op.path.split("#", 1)[0] || op.path) !== canonicalPath)
                return true;
              const keep = seen !== indexAmongPath;
              seen += 1;

              return keep;
            });

            onReplaceOps(next);
          }}
        />
      ))}
    </section>
  );
}
