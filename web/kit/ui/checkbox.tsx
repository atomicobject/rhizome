import { CheckIcon } from "lucide-react";
import { Checkbox as CheckboxPrimitive } from "radix-ui";
import type { ComponentProps } from "react";

import { cn } from "../lib/utils";

export function Checkbox({ className, ...props }: ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer size-[14px] shrink-0 rounded-sm border border-input bg-surface disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive data-[state=checked]:border-foreground data-[state=checked]:bg-foreground data-[state=checked]:text-background",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="flex items-center justify-center text-current transition-none"
      >
        <CheckIcon className="size-3" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}
