import { useState } from "react";
import { isString } from "../api/parse";
import type { NativeSourceContent, NativeSourceValue } from "../api/nativeSource";
import type { NodeWorkspace } from "../api/types";
import { isContentNode } from "../api/types";
import { DisclosureChevron } from "./DisclosureChevron";

type Props = { source: NativeSourceContent; workspace: NodeWorkspace };

function keyLabel(key: string | number): string {
  return isString(key) && (!key || /^\d+$/.test(key)) ? JSON.stringify(key) : String(key);
}

function Value({ value, depth = 0 }: { value: NativeSourceValue; depth?: number }) {
  switch (value.kind) {
    case "object": {
      const entries = value.entries || [];

      if (!entries.length) return <code aria-label="Empty map">{"{}"}</code>;

      return (
        <dl className="native-record__map">
          {entries.map((entry, index) => (
            <div key={index} className="native-record__property">
              <dt>
                <code>{entry.keyText || keyLabel(entry.key)}</code>
              </dt>
              <dd>
                <Branch value={entry.value} depth={depth} />
              </dd>
            </div>
          ))}
        </dl>
      );
    }

    case "array": {
      const items = value.items || [];

      if (!items.length) return <code aria-label="Empty list">[]</code>;

      return (
        <ol className="native-record__list">
          {items.map((item, index) => (
            <li key={index}>
              <Branch value={item} depth={depth} />
            </li>
          ))}
        </ol>
      );
    }

    case "null":
      return <code className="native-record__null">null</code>;
    case "string":
    case "date":
    case "timestamp":
      return value.scalar === "" ? (
        <code aria-label="Empty string">{'""'}</code>
      ) : (
        <span className="native-record__text">{value.scalar}</span>
      );
    case "boolean":
      return <code>{String(value.scalar)}</code>;
    case "integer":
    case "float":
      return <code>{value.scalarText || String(value.scalar)}</code>;
    default: {
      const exhaustive: never = value;

      return exhaustive;
    }
  }
}

function Branch({ value, depth }: { value: NativeSourceValue; depth: number }) {
  const count =
    value.kind === "object"
      ? value.entries?.length
      : value.kind === "array"
        ? value.items?.length
        : 0;

  if (!count) return <Value value={value} depth={depth + 1} />;

  return (
    <details
      className="native-record__branch"
      open={depth < 2 && (value.kind === "object" || count <= 3)}
    >
      <summary>
        <DisclosureChevron />
        <span>
          {count}{" "}
          {value.kind === "object"
            ? count === 1
              ? "property"
              : "properties"
            : count === 1
              ? "item"
              : "items"}
        </span>
      </summary>
      <Value value={value} depth={depth + 1} />
    </details>
  );
}

// Hide only exact scalar matches and complete child collections; null/empty
// values and aliases stay visible because typed fields can lose those details.
function remainingValue({ source, workspace }: Props): NativeSourceValue {
  if (source.value.kind !== "object") return source.value;

  return {
    ...source.value,
    entries: (source.value.entries || []).filter(({ key, value }) => {
      if (
        value.kind !== "object" &&
        value.kind !== "array" &&
        value.kind !== "null" &&
        value.scalar !== ""
      ) {
        if (
          workspace.fields?.some(
            (field) =>
              field.name === key &&
              field.present &&
              field.values?.length === 1 &&
              field.values[0] === String(value.scalar),
          )
        )
          return false;
      }

      if (value.kind === "array" && value.items?.length) {
        const collection = workspace.collections?.find((collection) => collection.name === key);
        const address = `${source.address || ""}/${String(key).replaceAll("~", "~0").replaceAll("/", "~1")}`;

        if (
          collection?.items?.length === value.items.length &&
          collection.items.every((item, index) =>
            workspace.nodes?.some(
              (node) =>
                isContentNode(node) &&
                node.ref.nodeId === item.ref.nodeId &&
                node.data.source?.address === `${address}/${index}`,
            ),
          )
        )
          return false;
      }

      return true;
    }),
  };
}

export function NativeRecordValue({ source, workspace }: Props) {
  const [completeOpen, setCompleteOpen] = useState(false);
  const remaining = remainingValue({ source, workspace });

  const omitted =
    source.value.kind === "object" &&
    remaining.kind === "object" &&
    (remaining.entries?.length || 0) < (source.value.entries?.length || 0);

  const hasRemaining = remaining.kind !== "object" || Boolean(remaining.entries?.length);

  return (
    <section className="native-record" aria-label="Record value">
      {hasRemaining && (
        <>
          <h2 className="section-heading">
            {omitted ? "Additional properties" : "Record properties"}
          </h2>
          <Value value={remaining} />
        </>
      )}
      <details
        className="native-record__complete"
        onToggle={(event) => setCompleteOpen(event.currentTarget.open)}
      >
        <summary>
          <DisclosureChevron />
          {omitted ? "All record properties" : "Source location"}
        </summary>
        <p className="native-record__location">
          <code>{source.address || "/"}</code>
        </p>
        {omitted && completeOpen && <Value value={source.value} />}
      </details>
    </section>
  );
}
