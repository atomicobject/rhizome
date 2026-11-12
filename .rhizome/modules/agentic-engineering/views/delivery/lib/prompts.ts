import { eligibility, groupResult, type Stage, type RowSort } from './groups.ts';
import { identity, type FrozenScope, type Portfolio } from './model.ts';
import { inventoryComplete } from './coverage.ts';

export type Prompt = { text: string; copyable: boolean; reasons: readonly string[] };

/** Unrecorded or partly read frozen scope is context for the agent to inspect, not a reason to refuse. */
const scopeNotes: Record<FrozenScope['state'], string> = {
  known: '',
  unknown: 'frozen scope not recorded, inspect the effort',
  incomplete: 'frozen scope incomplete in this read, inspect the effort',
  deferred: '',
};

/** Rows arrive in displayed order. Selection keys never supply ordering or scope. */
export function promptFor(stage: Stage, input: Portfolio, selectedKeys: readonly string[], sort: RowSort = 'source'): Prompt {
  const result = groupResult(stage, input, sort);
  if (result.state !== 'records') return { text: '', copyable: false, reasons: ['Invalid route'] };
  const rows = result.rows;
  const selected = new Set(selectedKeys);
  const seen = new Set<string>();
  const chosen = rows.filter(row => {
    const key = identity(row.record.ref);
    if (!selected.has(key) || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  const reasons = [...new Set(chosen.flatMap(row => {
    const result = eligibility(row);
    return result.reason ? [result.reason] : [];
  }))];
  if (!chosen.length) reasons.push('Select records to compose a prompt.');
  if (chosen.length && !inventoryComplete(chosen.map(row => row.record.coverage))) {
    reasons.push('Selected records do not share complete evidence from one graph generation');
  }
  if (seen.size !== selected.size) reasons.push('Selection changed or is no longer available');
  if (reasons.length) return { text: '', copyable: false, reasons };
  const lines: string[] = [];
  const locators = new Set<string>();
  function add(locator: string | null, label: string) {
    if (locator && !locators.has(locator)) {
      lines.push(`- ${locator}${label ? ` (${label})` : ''}`);
      locators.add(locator);
    }
  }
  for (const row of chosen) {
    if (row.kind === 'story') add(row.parent.locator,
      row.parent.status === 'proposed' ? 'proposed parent spec' : 'parent spec');
    const status = row.kind === 'story' ? 'selected story' : row.record.status ?? 'recorded status unavailable';
    const scope = row.kind === 'effort' || row.kind === 'review' ? scopeNotes[row.record.scope.state] : '';
    add(row.record.locator, scope ? `${status}; ${scope}` : status);
    if (row.kind === 'review') {
      for (const selected of row.selected) add(selected.locator, 'selected pending contract');
    }
    if (row.kind === 'effort' || row.kind === 'review') {
      for (const source of row.record.contextSources ?? []) {
        if (source.locator) add(source.locator, source.role);
        else lines.push(`- Recorded ${source.role}: ${source.authored} (reference not verified in this read)`);
      }
    }
  }
  const these = chosen.length === 1 ? 'this effort' : 'these efforts';
  const routes: Record<Stage, string> = {
    shaping: 'Use the installed agentic-engineering specify route to shape the selected contracts.',
    ready: 'Use the installed agentic-engineering effort route, then plan, to evaluate this proposed slice and existing frozen selections. Settle any proposed parent through specify before freezing delivery scope. Selection remains proposed until the workflow freezes it. Do not assume one effort per spec.',
    planned: `Use the installed agentic-engineering classify route to inspect authority, scope, and the plan before resuming ${these}.`,
    active: `Use the installed agentic-engineering implement route to inspect authority and resume ${these} from ${chosen.length === 1 ? "its" : "their"} recorded context.`,
    history: 'Use the installed agentic-engineering finish or reconcile route to inspect recorded delivery evidence. Preserve historical records.',
    contracts: 'Use the installed agentic-engineering specify route to inspect these normative contracts.',
    review: 'Use the installed agentic-engineering finish or reconcile route to compare actual delivery, deviations, and the exact selected contract. Investigate the recorded observations without presuming acceptance, an error, or a required state edit.',
  };
  return { copyable: true, reasons: [], text: `${routes[stage]}\n\n${lines.join('\n')}\n\nRead the exact sources and team policy. Preserve existing authorization. Missing approver metadata does not establish missing approval. Resolve material authority conflicts before dependent work. Choose the next work from the full context and dependencies.` };
}

export function firstDayPrompt(outcome = '<describe the outcome>'): Prompt {
  return { copyable: true, reasons: [], text: `Use the installed agentic-engineering specify route to shape this outcome into an appropriate contract.\n\n${outcome.trim() || '<describe the outcome>'}\n\nInspect existing contracts and team policy first. Preserve the user\'s intent and identify material uncertainty.` };
}
