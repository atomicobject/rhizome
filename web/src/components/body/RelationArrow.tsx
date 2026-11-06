import { ArrowLeft, ArrowRight, ArrowLeftRight } from "lucide-react";

export function RelationArrow({ direction }: { direction: "incoming" | "outgoing" | "both" }) {
  const Icon = { incoming: ArrowLeft, outgoing: ArrowRight, both: ArrowLeftRight }[direction];

  return (
    <Icon
      size={16}
      strokeWidth={1.5}
      absoluteStrokeWidth
      aria-hidden="true"
      className="relation-arrow"
    />
  );
}
