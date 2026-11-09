import { useViewContext } from "@rhizome/kit";

export default function ExperiencePanel() {
  const context = useViewContext();

  return <p aria-label="Panel subject">{context.kind}</p>;
}
