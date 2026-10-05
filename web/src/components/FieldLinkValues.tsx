import { Fragment } from "react";
import type { NodeFieldLink } from "../api/types";
import { publicWorkspaceRef } from "../api/client";
import { displayTitle } from "../lib/labels";
import { relationDisplayValue } from "./ConfiguredTableCellReadOnly";
import { NoteLinkPreview } from "./notePreview/NoteLinkPreview";

/** A relation value's label: its target's title, else the authored link text. */
export function fieldLinkLabel(link: NodeFieldLink): string {
  return displayTitle(link.title) || relationDisplayValue(link.value);
}

/**
 * Pairs each value with its resolved link, so a staged draft that adds or
 * reorders values still shows the titles the server resolved.
 */
export function fieldLinksForValues(
  values: string[],
  links: NodeFieldLink[] = [],
): NodeFieldLink[] {
  return values.map((value) => links.find((link) => link.value === value) ?? { value });
}

/**
 * Relation-field values as titled note links with hover previews, the same
 * presentation configured views give relation cells. Without `open`, or for an
 * unresolved value, the label renders as plain text.
 */
export function FieldLinkValues({
  links,
  from,
  open,
}: {
  links: NodeFieldLink[];
  from?: string;
  open?: (target: string, mode: "stack" | "beside") => void;
}) {
  return links.map((link, index) => {
    const target = link.ref ? publicWorkspaceRef(link.ref) : "";
    const label = fieldLinkLabel(link);

    return (
      <Fragment key={`${link.value}:${index}`}>
        {index > 0 && ", "}
        {target && open ? (
          <NoteLinkPreview target={target} from={from} open={open}>
            {label}
          </NoteLinkPreview>
        ) : (
          label
        )}
      </Fragment>
    );
  });
}
