import { ChildSectionInline } from "./ChildSectionInline";
import { ChildSectionDisclosure } from "./ChildSectionDisclosure";
import type { BodyRendererProps } from "./registry";

/**
 * Routes a `child_section` block to the appropriate renderer based on the
 * schema-declared `sectionDisplay` on the parent's `@contains` binding.
 * This decision stays in one place so the registry can point both view
 * and edit modes at the same router — the INLINE vs PANE split is purely
 * a schema decision, not a mode decision.
 *
 * The PANE default belongs to `@contains` declarations and reads as a
 * collapsed disclosure. A heading no field declares (no `fieldName`) is
 * ordinary document structure and reads inline.
 */
export function ChildSectionRouter(props: BodyRendererProps) {
  const { fieldName, sectionDisplay } = props.block;

  if (sectionDisplay === "INLINE" || !fieldName) {
    return <ChildSectionInline {...props} />;
  }

  return <ChildSectionDisclosure {...props} />;
}
