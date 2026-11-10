import { useState } from "react";
import { Section } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { useDeliveryPortfolio, useDeliverySelectedEfforts } from "./data.tsx";
import type { LiveSpec } from "./data.tsx";
import { field } from "./reads.ts";
import { identity } from "./lib/index.ts";
import { RecordLink } from "./presentation.tsx";

function LoadedRelationships({ spec }: { spec: LiveSpec }) {
  const inventory = useDeliveryPortfolio();
  const details = useDeliverySelectedEfforts(
    inventory.data?.portfolio.efforts.map((effort) => effort.ref) ?? [],
  );
  const members = new Set(
    [
      spec.ref,
      ...spec.stories.flatMap((story) => [
        story.ref,
        ...story.criteria.map((criterion) => criterion.ref),
      ]),
    ].map(identity),
  );
  const rows =
    details.data?.efforts.flatMap((effort) => {
      const metadata =
        field(effort.source, "governingSpecs")?.links?.filter(
          (link) => link.ref && identity(link.ref) === identity(spec.ref),
        ) ?? [];
      const frozen = effort.scope.specs.filter(
        (occurrence) => occurrence.target && identity(occurrence.target) === identity(spec.ref),
      );
      const selected = effort.scope.selections.filter(
        (occurrence) => occurrence.target && members.has(identity(occurrence.target)),
      );
      return metadata.length || frozen.length || selected.length
        ? [{ effort, metadata, frozen, selected }]
        : [];
    }) ?? [];
  const pending =
    inventory.isPending || (!!inventory.data?.portfolio.efforts.length && details.isPending);
  const complete =
    inventory.data?.coverage.complete &&
    (!inventory.data.portfolio.efforts.length || details.data?.coverage.complete);
  return (
    <>
      {pending && <p role="status">Reading authored effort sources…</p>}
      {(inventory.error || details.error) && (
        <p role="alert">
          Effort relationships could not be read.{" "}
          <Button
            onClick={() => {
              inventory.refetch();
              details.refetch();
            }}
          >
            Read relationships again
          </Button>
        </p>
      )}
      {!pending && !complete && (
        <p role="status">
          Relationship evidence is incomplete. These are the links found in the loaded sources;
          missing links do not establish absent scope.
        </p>
      )}
      {rows.map(({ effort, metadata, frozen, selected }) => (
        <div className="ae-row" key={identity(effort.ref)}>
          <RecordLink record={effort} />
          <p className="ae-note">
            Recorded {effort.status}. Frozen membership {effort.scope.state}.
          </p>
          {metadata.length > 0 && (
            <p>Governing-specs metadata: {metadata.map((link) => link.value).join(", ")}</p>
          )}
          {frozen.map((occurrence, i) => (
            <p key={`spec-${i}`}>
              Spec Set (Frozen): {occurrence.authored}
              <br />
              <span className="ae-note">
                Source {occurrence.source.notePath}#{occurrence.source.fragment}
              </span>
            </p>
          ))}
          {selected.map((occurrence, i) => (
            <p key={`selection-${i}`}>
              Stories In Scope (Frozen): {occurrence.authored}
              <br />
              <span className="ae-note">
                {occurrence.targetKind} · source {occurrence.source.notePath}#
                {occurrence.source.fragment}
              </span>
            </p>
          ))}
        </div>
      ))}
      {!pending && !rows.length && (
        <p className="ae-note">No matching authored relationships returned in this read.</p>
      )}
      <p className="ae-note">
        General inbound mentions do not establish frozen selection. Open an effort for unresolved
        occurrences, entry mentions, and separate log snapshots.
      </p>
    </>
  );
}

export function EffortRelationships({ spec }: { spec: LiveSpec }) {
  const [open, setOpen] = useState(false);
  return (
    <Section
      title="Effort relationships"
      caption="Load exact authored sources separately from the contract. This does not decide coverage or delivery."
    >
      {open ? (
        <LoadedRelationships spec={spec} />
      ) : (
        <Button onClick={() => setOpen(true)}>Load effort relationships</Button>
      )}
    </Section>
  );
}
