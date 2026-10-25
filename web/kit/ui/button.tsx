import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import type { ComponentProps } from "react";

import { cn } from "../lib/utils";

export const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md text-sm font-medium whitespace-nowrap transition-colors disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-3.5",
  {
    variants: {
      variant: {
        // The neutral toolbar button. Red is opt-in: `primary` for commit,
        // run, or save; `destructive` for removal.
        default:
          "border border-border bg-surface text-foreground hover:border-input hover:bg-accent",
        primary: "bg-primary text-primary-foreground hover:bg-[var(--accent-fill-hover)]",
        destructive: "bg-destructive text-primary-foreground hover:bg-[var(--accent-fill-hover)]",
        outline:
          "border border-border bg-surface text-foreground hover:border-input hover:bg-accent",
        secondary: "bg-secondary text-secondary-foreground hover:bg-accent",
        ghost: "text-foreground hover:bg-accent",
        link: "text-link underline decoration-[var(--link-line)] underline-offset-2 hover:decoration-current",
      },
      size: {
        default: "h-[26px] px-2.5 has-[>svg]:px-2",
        sm: "h-[22px] gap-1 px-2 text-xs has-[>svg]:px-1.5",
        lg: "h-[30px] px-3.5 has-[>svg]:px-3",
        icon: "size-[26px]",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: ComponentProps<"button"> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "button";

  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}
