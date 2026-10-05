import { type ComponentProps, Fragment, useEffect, useMemo } from "react";

import type { ViewExecuteResponse, ViewTableRow } from "../api/types";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import { ConfiguredViewCard, focusCard } from "./ConfiguredViewCard";
import { capabilityFor, groupValueLabel } from "./ConfiguredViewModel";
import { RecordBriefBody, RecordBriefHeader } from "./ConfiguredViewBrief";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { groupIdentity } from "./ConfiguredViewSave";
import { StatusMark } from "./StatusMark";
import { useRememberedExpansion } from "./useRememberedExpansion";
import { fieldValue } from "./ConfiguredTableCellHelpers";
import { type ReorderGroup, useViewReorder, type ViewReorder } from "./useViewReorder";

type Props = ComponentProps<typeof ConfiguredViewTable>;

type ViewGroup = NonNullable<ViewExecuteResponse["groups"]>[number];

export function ConfiguredViewCards(props: Props) {
  const groups = props.execution.groups ?? [];
  const grouped = groups.length > 0 && !(groups.length === 1 && !groups[0]?.value);

  const reorderGroups = useMemo(
    () => cardReorderGroups(grouped ? groups : [], props.rows, props.capabilities),
    [grouped, groups, props.capabilities, props.rows],
  );

  const reorder = useViewReorder({
    execution: props.execution,
    rows: props.rows,
    capabilities: props.capabilities,
    onStageOps: props.onStageOps,
    groups: reorderGroups,
    showStagedEdits: props.showStagedEdits,
    titleFor: (row) => fieldValue(row, props.execution.card?.title.field ?? "title"),
    axis: "horizontal",
  });

  const rowsFor = (group: ViewGroup) =>
    reorder.groups.find((candidate) => candidate.id === group.key)?.rows ??
    props.rows.slice(group.rowStart, group.rowEnd);

  const expansion = useRememberedExpansion(
    props.preferenceScope,
    props.vaultKey,
    "native.cardGroups",
    groupIdentity(props.execution.state.group),
  );

  const isCollapsed = (group: ViewGroup) =>
    !expansion.isExpanded(group.key, !group.collapsedByDefault);

  const collapsedCount = groups.filter(isCollapsed).length;

  useEffect(() => {
    props.onCollapsedGroupCountChange(collapsedCount);
  }, [collapsedCount, props.onCollapsedGroupCountChange]);

  function toggleGroup(group: ViewGroup) {
    expansion.toggle(group.key, !group.collapsedByDefault);
  }

  function handleGridKeyDown(event: React.KeyboardEvent<HTMLElement>) {
    // Alt+Arrow keys move the card itself; see ConfiguredViewCard.
    if (event.defaultPrevented || event.altKey) return;

    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    const target = event.target;

    if (!(target instanceof HTMLElement) || !target.matches("[data-configured-card]")) return;
    const cards = [...event.currentTarget.querySelectorAll<HTMLElement>("[data-configured-card]")];
    const index = cards.indexOf(target);
    const next = cards[index + (event.key === "ArrowRight" ? 1 : -1)];

    if (!next) return;
    event.preventDefault();
    next.focus();
  }

  const { profile, view } = props.execution;

  // An authored card layout wins; generated cards become record briefs (SPEC-0112).
  const briefFor =
    profile && (view.generated || !view.variants.card)
      ? (row: ViewTableRow) => ({
          className: " configured-card--brief",
          header: (
            <RecordBriefHeader
              row={row}
              profile={profile}
              capabilities={props.capabilities}
              editSession={props.editSession}
              onStageOps={props.onStageOps}
            />
          ),
          body: (
            <RecordBriefBody
              row={row}
              profile={profile}
              capabilities={props.capabilities}
              hiddenFields={[]}
              onOpenRow={props.onOpenRow}
            />
          ),
        })
      : undefined;

  const cardProps = {
    layout: props.execution.card,
    mode: "cards" as const,
    capabilities: props.capabilities,
    viewID: props.viewID,
    editSession: props.editSession,
    vaultKey: props.vaultKey,
    onOpenRow: props.onOpenRow,
    issueCounts: props.issueCounts,
    onOpenIssues: props.onOpenIssues,
  };

  return (
    <div className="configured-view__cards" onKeyDown={handleGridKeyDown}>
      {props.loading && (
        <div className="configured-view__loading-overlay" aria-live="polite">
          Refreshing view…
        </div>
      )}
      {reorder.error && (
        <div className="configured-view__status configured-view__status--error" role="alert">
          {reorder.error}
        </div>
      )}
      {grouped ? (
        groups.map((group) => {
          const collapsed = isCollapsed(group);
          const capability = capabilityFor(props.capabilities, group.field ?? "");
          const label = groupValueLabel(capability, group.value, group.label);

          const tone =
            group.tone ??
            capability?.enumValues?.find((option) => option.value === group.value)?.tone;

          return (
            <section className="configured-view__card-section" key={group.key}>
              <h3 className="configured-view__card-section-header">
                <button type="button" aria-expanded={!collapsed} onClick={() => toggleGroup(group)}>
                  <span className="configured-view__card-section-caret" aria-hidden />
                  {tone ? <StatusMark tone={tone} label={label} /> : <span>{label}</span>}
                  <span className="configured-view__card-section-count">
                    {group.totalCount ?? group.count}
                  </span>
                </button>
              </h3>
              {!collapsed &&
                (group.children && group.children.length > 0 ? (
                  flattenLeafGroups(group.children).map((child) => (
                    <Fragment key={child.key}>
                      <h4 className="configured-view__card-subheader">
                        {groupValueLabel(
                          capabilityFor(props.capabilities, child.field ?? ""),
                          child.value,
                          child.label,
                        )}
                      </h4>
                      <CardGrid
                        briefFor={briefFor}
                        rows={rowsFor(child)}
                        groupId={child.key}
                        reorder={reorder}
                        cardProps={{ ...cardProps, omitField: group.field }}
                      />
                    </Fragment>
                  ))
                ) : (
                  <CardGrid
                    briefFor={briefFor}
                    rows={rowsFor(group)}
                    groupId={group.key}
                    reorder={reorder}
                    // The section header already names the grouped value.
                    cardProps={{ ...cardProps, omitField: group.field }}
                  />
                ))}
            </section>
          );
        })
      ) : (
        <CardGrid
          briefFor={briefFor}
          rows={reorder.groups[0]?.rows ?? props.rows}
          groupId={UNGROUPED}
          reorder={reorder}
          cardProps={cardProps}
        />
      )}
      <div className="sr-only" aria-live="polite">
        {reorder.announcement}
      </div>
    </div>
  );
}

type CardGridProps = Pick<
  ComponentProps<typeof ConfiguredViewCard>,
  | "layout"
  | "mode"
  | "capabilities"
  | "viewID"
  | "editSession"
  | "vaultKey"
  | "onOpenRow"
  | "issueCounts"
  | "onOpenIssues"
  | "omitField"
>;

function CardGrid({
  rows,
  groupId,
  reorder,
  cardProps,
  briefFor,
}: {
  rows: ViewTableRow[];
  groupId: string;
  reorder: ViewReorder;
  cardProps: CardGridProps;
  briefFor?: (
    row: ViewTableRow,
  ) => Pick<ComponentProps<typeof ConfiguredViewCard>, "className" | "header" | "body">;
}) {
  return (
    <div
      className={`configured-view__card-grid${briefFor ? " configured-view__card-grid--briefs" : ""}${reorder.dropClass(groupId, null)}`}
      {...(reorder.dragKey ? reorder.dropTarget(groupId, null) : {})}
    >
      {rows.map((row) => {
        const rowKey = configuredTableRowKey(row);
        const source = reorder.dragSource(row);
        const brief = briefFor?.(row);

        return (
          <ConfiguredViewCard
            key={rowKey}
            row={row}
            {...cardProps}
            header={brief?.header}
            body={brief?.body}
            busy={reorder.pendingKeys.has(rowKey)}
            draggable={source.draggable}
            dragging={reorder.dragKey === rowKey}
            onDragStart={source.onDragStart}
            onDragEnd={source.onDragEnd}
            dropTarget={reorder.dragKey ? reorder.dropTarget(groupId, row) : undefined}
            className={`${brief?.className ?? ""}${reorder.dropClass(groupId, row)}`}
            onReorder={
              reorder.enabled
                ? (delta) => void reorder.moveByKeyboard(row, delta).then(() => focusCard(rowKey))
                : undefined
            }
          />
        );
      })}
    </div>
  );
}

const UNGROUPED = "__all__";

/** Leaf card groups with their group-field path, or one group holding every card. */
function cardReorderGroups(
  groups: ViewGroup[],
  rows: ViewTableRow[],
  capabilities: Props["capabilities"],
): ReorderGroup[] {
  if (groups.length === 0) return [{ id: UNGROUPED, label: "", path: [], rows }];

  const leaves: ReorderGroup[] = [];

  const visit = (group: ViewGroup, path: ReorderGroup["path"]) => {
    const here = [...path, { field: group.field ?? "", value: group.value }];

    if (group.children && group.children.length > 0) {
      for (const child of group.children) visit(child, here);

      return;
    }

    leaves.push({
      id: group.key,
      label: groupValueLabel(
        capabilityFor(capabilities, group.field ?? ""),
        group.value,
        group.label,
      ),
      path: here,
      rows: rows.slice(group.rowStart, group.rowEnd),
    });
  };

  for (const group of groups) visit(group, []);

  return leaves;
}

function flattenLeafGroups(groups: ViewGroup[]): ViewGroup[] {
  return groups.flatMap((group) =>
    group.children && group.children.length > 0 ? flattenLeafGroups(group.children) : [group],
  );
}
