import type { ReactNode } from "react";
import { useTypeAccentColor } from "../lib/typeAccent";

export function TypeChip({
  name,
  children,
  className = "",
}: {
  name: string;
  children: ReactNode;
  className?: string;
}) {
  const color = useTypeAccentColor(name);

  return (
    <span className={`type-chip ${className}${name ? "" : " is-untyped"}`}>
      <i className="type-chip__dot" aria-hidden="true" style={{ background: color }} />
      {children}
    </span>
  );
}
