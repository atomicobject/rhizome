import { Fragment, useState } from "react";
import { Section } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { useDeliveryPortfolio, useDeliverySelectedEfforts } from "./data.tsx";
import type { LiveSpec } from "./data.tsx";
import { field } from "./reads.ts";
import { identity, inventoryComplete } from "./lib/index.ts";
import { Notice, RecordLink, sourceLocator } from "./presentation.tsx";

function LoadedRelationships({ spec }: { spec: LiveSpec }) {
  const inventory = useDeliveryPortfolio();
  const details = useDeliverySelectedEfforts(
    inventory.data?.portfolio.efforts.map((effort) => effort.ref) ?? [],
    inventory.data?.portfolio.effortCoverage.graphGeneration ?? null,
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
    inventory.data &&
    !inventory.isFetching &&
    inventoryComplete([spec.coverage, inventory.data.coverage]) &&
    (!inventory.data.portfolio.efforts.length ||
      (details.data &&
        !details.isFetching &&
        inventoryComplete([inventory.data.coverage, details.data.coverage]) &&
        details.data.efforts.every((effort) => effort.scope.state === "known")));
  return (
    <>
      {pending && (
        <p role="status" className="ae-note">
          Reading authored effort sources…
        </p>
      )}
      {(inventory.error || details.error) && (
        <Notice tone="risk" role="alert">
          Effort relationships could not be read.{" "}
          <Button
            onClick={() => {
              inventory.refetch();
              details.refetch();
            }}
          >
            Read relationships again
          </Button>
        </Notice>
      )}
      {!pending && !complete && (
        <Notice role="status">
          Relationship evidence is incomplete. These are the links found in the loaded sources;
          missing links do not establish absent scope.
        </Notice>
      )}
      {rows.map(({ effort, metadata, frozen, selected }) => (
        <div className="ae-row" key={identity(effort.ref)}>
          <RecordLink record={effort} />
          <p className="ae-note">
            Recorded {effort.status}. Frozen membership {effort.scope.state}.
          </p>
          <dl className="ae-relations">
            {metadata.length > 0 && (
              <>
                <dt>Governing-specs metadata:</dt>
                <dd>
                  <code className="ae-path">{metadata.map((link) => link.value).join(", ")}</code>
                </dd>
              </>
            )}
            {frozen.map((occurrence, i) => (
              <Fragment key={`spec-${i}`}>
                <dt>Spec Set (Frozen):</dt>
                <dd>
                  <code className="ae-path">{occurrence.authored}</code>
                  <span className="ae-note">
                    Source <code className="ae-path">{sourceLocator(occurrence.source)}</code>
                  </span>
                </dd>
              </Fragment>
            ))}
            {selected.map((occurrence, i) => (
              <Fragment key={`selection-${i}`}>
                <dt>Stories In Scope (Frozen):</dt>
                <dd>
                  <code className="ae-path">{occurrence.authored}</code>
                  <span className="ae-note">
                    {occurrence.targetKind} · source{" "}
                    <code className="ae-path">{sourceLocator(occurrence.source)}</code>
                  </span>
                </dd>
              </Fragment>
            ))}
          </dl>
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
