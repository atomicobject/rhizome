import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import type { ComponentProps } from "react";

import { cn } from "../lib/utils";

export const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-sm border px-1.5 py-px text-xs font-semibold whitespace-nowrap [&>svg]:pointer-events-none [&>svg]:size-3",
  {
    variants: {
      variant: {
        // Neutral, like the host's type chip. `destructive` marks risk.
        default: "border-border bg-sunk text-muted-foreground [a&]:hover:bg-accent",
        secondary: "border-border bg-sunk text-muted-foreground [a&]:hover:bg-accent",
        destructive:
          "border-[var(--accent-line)] bg-[var(--accent-soft)] text-risk [a&]:hover:bg-[var(--accent-soft)]",
        outline: "border-border text-foreground [a&]:hover:bg-accent",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export function Badge({
  className,
  variant,
  asChild = false,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span";

  return (
    <Comp data-slot="badge" className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}
