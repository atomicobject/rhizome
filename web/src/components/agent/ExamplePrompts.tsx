import { ArrowRight } from "lucide-react";

const EXAMPLE_PROMPTS = [
  "What changed in this repository this week?",
  "Which notes explain how search ranks results?",
  "List the open action items and who owns them.",
];

/** An empty thread offers a few starting questions; picking one fills the composer. */
export function ExamplePrompts({ onPick }: { onPick: (text: string) => void }) {
  return (
    <div className="agent-examples">
      <h2 className="agent-examples__head section-heading">Try asking</h2>
      <ul>
        {EXAMPLE_PROMPTS.map((prompt) => (
          <li key={prompt}>
            <button type="button" onClick={() => onPick(prompt)}>
              <span>{prompt}</span>
              <ArrowRight size={16} aria-hidden="true" className="agent-examples__arrow" />
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
