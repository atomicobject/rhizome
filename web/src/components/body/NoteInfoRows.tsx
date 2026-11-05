import { useState } from "react";
import { useNotePreviewTrigger } from "../notePreview/NoteLinkPreview";

type Direction = "outgoing" | "incoming" | "both";

const DIRECTION_GLYPH: Record<Direction, string> = { outgoing: "→", incoming: "←", both: "↔" };

const DIRECTION_LABEL: Record<Direction, string> = {
  outgoing: "Links to",
  incoming: "Links here",
  both: "Linked both ways",
};

export type RailRow = {
  key: string;
  target: string;
  label: string;
  title: string;
  typeName: string;
  direction: Direction;
  current?: boolean;
  openable: boolean;
  /** Folder shown beside an untyped note, in place of a type. */
  folder?: string;
};

export type RailBucket = {
  key: string;
  label: string;
  hint?: string;
  direction?: Direction;
  rows: RailRow[];
};

function SidebarPreviewButton({
  target,
  label,
  title,
  current,
  from,
  onOpen,
}: {
  target: string;
  label: string;
  title?: string;
  current?: boolean;
  from: string;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
}) {
  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    from,
    placement: "left",
    placementBoundary: (trigger) =>
      trigger.closest(".ontology-sidebar-info")?.getBoundingClientRect() ?? null,
    open: (path, mode) => onOpen(path, mode),
  });

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="link-quiet link-title"
        title={title || target}
        aria-current={current ? "page" : undefined}
        onClick={(event) => {
          previewTrigger.close();
          onOpen(target, event.metaKey || event.ctrlKey ? "beside" : "stack");
        }}
      >
        {label}
      </button>
      {previewTrigger.preview}
    </>
  );
}

export type OpenFn = (path: string, target?: "current" | "stack" | "beside") => void;

export function RailRowItem({
  row,
  showDirection,
  from,
  onOpen,
}: {
  row: RailRow;
  showDirection: boolean;
  from: string;
  onOpen: OpenFn;
}) {
  return (
    <li className="rail-row">
      {showDirection && (
        <span
          className={`rail-row__dir rail-row__dir--${row.direction}`}
          role="img"
          aria-label={DIRECTION_LABEL[row.direction]}
          title={DIRECTION_LABEL[row.direction]}
        >
          {DIRECTION_GLYPH[row.direction]}
        </span>
      )}
      {row.openable ? (
        <SidebarPreviewButton
          target={row.target}
          label={row.label}
          title={row.title}
          current={row.current}
          from={from}
          onOpen={onOpen}
        />
      ) : (
        <span className="rail-row__label" title={row.title}>
          {row.label}
        </span>
      )}
      {row.typeName && <span className="rail-row__type">{row.typeName}</span>}
      {row.folder && (
        // Truncates from the start (CSS rtl) so the deepest folder stays; the
        // marks keep the path itself in left-to-right order.
        <span className="rail-row__path" title={row.folder}>
          {`\u200e${row.folder}\u200e`}
        </span>
      )}
    </li>
  );
}

const BUCKET_LIMIT = 5;

export function RailBucketList({
  bucket,
  showDirection,
  from,
  onOpen,
}: {
  bucket: RailBucket;
  showDirection: boolean;
  from: string;
  onOpen: OpenFn;
}) {
  const [expanded, setExpanded] = useState(false);
  // Show everything when only one row would hide behind the toggle.
  const limit = bucket.rows.length <= BUCKET_LIMIT + 1 ? bucket.rows.length : BUCKET_LIMIT;
  const rows = expanded ? bucket.rows : bucket.rows.slice(0, limit);
  const hidden = bucket.rows.length - rows.length;

  return (
    <div className="rail-bucket">
      <div className="rail-bucket__head">
        <span className="rail-bucket__label">{bucket.label}</span>
        {bucket.hint && <span className="rail-bucket__hint">{bucket.hint}</span>}
        <span className="rail-bucket__count num">{bucket.rows.length}</span>
      </div>
      <ul>
        {rows.map((row) => (
          <RailRowItem
            key={row.key}
            row={row}
            showDirection={showDirection}
            from={from}
            onOpen={onOpen}
          />
        ))}
      </ul>
      {bucket.rows.length > limit && (
        <button
          type="button"
          className="rail-bucket__more"
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
        >
          {expanded ? "Show fewer" : `${hidden} more`}
        </button>
      )}
    </div>
  );
}

// Names the arrows that appear in a list, once, above it.
export function DirectionLegend({ buckets }: { buckets: RailBucket[] }) {
  const present = new Set(buckets.flatMap((bucket) => bucket.rows.map((row) => row.direction)));

  return (
    <p className="rail-legend">
      {(["incoming", "outgoing", "both"] as const).flatMap((direction) =>
        present.has(direction)
          ? [
              <span key={direction}>
                <span className={`rail-legend__dir rail-row__dir--${direction}`} aria-hidden="true">
                  {DIRECTION_GLYPH[direction]}
                </span>
                {DIRECTION_LABEL[direction]}
              </span>,
            ]
          : [],
      )}
    </p>
  );
}
