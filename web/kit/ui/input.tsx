import type { ComponentProps } from "react";

import { cn } from "../lib/utils";

export function Input({ className, type, ...props }: ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "flex h-[26px] w-full min-w-0 rounded-md border border-input bg-surface px-2 py-0.5 text-base selection:bg-[var(--accent-soft)] file:inline-flex file:h-5 file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-faint focus-visible:outline-offset-0 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
        "aria-invalid:border-destructive",
        className,
      )}
      {...props}
    />
  );
}
