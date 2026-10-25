import type { ComponentProps } from "react";

import { cn } from "../lib/utils";

export function Table({ className, ...props }: ComponentProps<"table">) {
  return (
    <div data-slot="table-container" className="relative w-full overflow-x-auto">
      <table
        data-slot="table"
        className={cn("w-full caption-bottom text-base", className)}
        {...props}
      />
    </div>
  );
}

export function TableHeader({ className, ...props }: ComponentProps<"thead">) {
  return (
    <thead
      data-slot="table-header"
      className={cn("[&_tr]:border-b [&_tr]:border-border [&_tr]:hover:bg-transparent", className)}
      {...props}
    />
  );
}

export function TableBody({ className, ...props }: ComponentProps<"tbody">) {
  return (
    <tbody
      data-slot="table-body"
      className={cn("[&_tr:last-child]:border-0", className)}
      {...props}
    />
  );
}

export function TableFooter({ className, ...props }: ComponentProps<"tfoot">) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn("border-t bg-muted/50 font-medium [&>tr]:last:border-b-0", className)}
      {...props}
    />
  );
}

export function TableRow({ className, ...props }: ComponentProps<"tr">) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        "border-b border-[var(--border-faint)] hover:bg-accent/60 data-[state=selected]:bg-accent",
        className,
      )}
      {...props}
    />
  );
}

export function TableHead({ className, ...props }: ComponentProps<"th">) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        "h-[28px] px-2 text-left align-middle text-xs font-bold whitespace-nowrap text-muted-foreground [&:has([role=checkbox])]:pr-0",
        className,
      )}
      {...props}
    />
  );
}

export function TableCell({ className, ...props }: ComponentProps<"td">) {
  return (
    <td
      data-slot="table-cell"
      className={cn(
        "h-[28px] px-2 py-1 align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0",
        className,
      )}
      {...props}
    />
  );
}

export function TableCaption({ className, ...props }: ComponentProps<"caption">) {
  return (
    <caption
      data-slot="table-caption"
      className={cn("mt-2 text-sm text-faint", className)}
      {...props}
    />
  );
}
