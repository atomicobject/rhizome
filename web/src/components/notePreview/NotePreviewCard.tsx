import { displayTitle } from "../../lib/labels";
import { publicTypeName } from "../../lib/typeNames";
import type { NodePreview, NodePreviewField } from "../../api/types";
import { NoteLinkPreview, type OpenNote } from "./NoteLinkPreview";

function FieldValues({
  field,
  from,
  open,
}: {
  field: NodePreviewField;
  from: string;
  open: OpenNote;
}) {
  return (
    <>
      {field.values.map((value, index) => (
        <span key={`${value.text}-${value.target || "text"}-${index}`}>
          {index > 0 && ", "}
          {field.kind === "link" && value.target ? (
            <NoteLinkPreview target={value.target} from={from} open={open}>
              {value.text}
            </NoteLinkPreview>
          ) : (
            value.text
          )}
        </span>
      ))}
      {Boolean(field.truncated) && (
        <span className="note-preview__more">+{field.truncated} more</span>
      )}
    </>
  );
}

function formatUpdatedAt(updatedAt?: number): string | null {
  if (!updatedAt) return null;

  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(updatedAt * 1000);
}

type Props = {
  target: string;
  open: OpenNote;
  preview?: NodePreview;
  loading?: boolean;
  error?: boolean;
};

export function NotePreviewCard({ target, open, preview, loading = false, error = false }: Props) {
  if (loading) {
    return (
      <div className="note-preview__status" role="status">
        Loading preview…
      </div>
    );
  }

  if (error || !preview) {
    return <div className="note-preview__status">Preview unavailable for {target}</div>;
  }

  const keyFields = preview.fields.filter((field) => field.importance === "KEY");
  const isTyped = Boolean(publicTypeName(preview.typeName));
  const hasTagChips = !isTyped && Boolean(preview.tags?.length);

  // Typed previews use their type label; plain notes may use tag chips.
  const remainingFields = preview.fields.filter(
    (field) => field.importance !== "KEY" && !(hasTagChips && field.name.toLowerCase() === "tags"),
  );

  const updatedAt = formatUpdatedAt(preview.updatedAt);

  return (
    <>
      <header className="note-preview__head">
        <div className="note-preview__meta">
          <span className="note-preview__type type-chip">
            {publicTypeName(preview.typeName)
              ? publicTypeName(preview.typeLabel) || publicTypeName(preview.typeName)
              : "Note"}
          </span>
          {preview.identifier && <code className="note-preview__id">{preview.identifier}</code>}
          {keyFields.map((field) => (
            <span
              className="note-preview__chip"
              key={field.name}
              title={field.label}
              aria-label={field.label}
            >
              <FieldValues field={field} from={preview.path} open={open} />
            </span>
          ))}
          {preview.hasIssues && <span className="note-preview__issue">Issues</span>}
        </div>
        <div className="note-preview__title" title={preview.path}>
          {displayTitle(preview.title)}
        </div>
        {preview.fragment && (
          <div className="note-preview__fragment">
            <span aria-hidden="true">§</span>
            <span>{preview.fragment.text}</span>
          </div>
        )}
        {preview.summary && (
          <blockquote className="note-preview__summary">{preview.summary}</blockquote>
        )}
        {preview.fragmentResolved === false && (
          <p className="note-preview__fragment-note">Heading not found; showing note.</p>
        )}
      </header>
      {remainingFields.length > 0 && (
        <dl className="note-preview__fields">
          {remainingFields.map((field) => (
            <div key={field.name}>
              <dt>{field.label}</dt>
              <dd>
                <FieldValues field={field} from={preview.path} open={open} />
              </dd>
            </div>
          ))}
        </dl>
      )}
      {hasTagChips && (
        <div className="note-preview__tags" aria-label="Tags">
          {preview.tags?.map((tag) => (
            <span key={tag}>#{tag}</span>
          ))}
        </div>
      )}
      {!isTyped && (
        <footer className="note-preview__foot">
          <span title={preview.path}>{preview.path}</span>
          {updatedAt && (
            <time dateTime={new Date((preview.updatedAt || 0) * 1000).toISOString()}>
              {updatedAt}
            </time>
          )}
        </footer>
      )}
    </>
  );
}
