export const title = 'Which Rhizome runs for this folder?';
export function state({s, scenario}) {
  switch (s.phase) {
    case 'inspect': return 'Choose a configured folder';
    case 'trust': return scenario === 22 ? 'App-owned Dev build still asks for trust' : 'Confirm checkout trust';
    case 'select': return 'Resolve executable authority';
    case 'install': return 'Prepare the managed pin';
    case 'missing':
      return !s.offer ? 'Pin missing; only retry is offered'
        : scenario === 18 ? 'Advance offered; sign-in will be lost'
        : 'Advance offered; index and sign-in incompatible';
    case 'choose': return 'Choose an external executable';
    case 'prepared': return 'Executable selected';
    case 'replace': return 'Replace a different headless build';
    case 'stopAttached': return 'Stop the attached runtime first';
    case 'spawn': return 'Start selected runtime';
    case 'attach': return 'Attach to an existing runtime';
    case 'initializing': return 'Listener is live; index readiness pending';
    case 'preparingForever': return 'Preparing index stays on screen';
    case 'providerUnavailable': return 'Runtime opens; team provider unavailable';
    case 'ready': return scenario === 6 && s.frontend === 'Desktop' && s.exe !== 'dev' ? 'Desktop silently differs from terminal' : 'Folder is usable';
  }
}
const labels = {
  Inspect: 'inspect without running repository code',
  GrantTrust: 'you trust this checkout',
  Install: 'fetch exact release [not cached]',
  AdvancePin: 'you advance pin to the app version',
  ChooseExecutable: 'you choose the development executable',
  ProbeAndEnsure: 'probe version and headless support; ensure runtime',
  ReplaceHeadless: 'stop the old headless runtime',
  StopAttached: 'you stop it and reopen',
  Spawn: 'start the selected executable',
  Attach: 'verify identity and use the live runtime',
  FinishInitialization: 'index or provider initialization finishes',
  OtherFrontendOpen: 'you open the same folder from the other front end',
  Done: 'no automatic recovery',
};
export function step(action, from, to) {
  if (action === 'Resolve') return to.s.phase === 'install' ? 'use managed pin [cache missing]'
    : to.s.phase === 'attach' ? 'external uses a verified live runtime'
    : to.s.phase === 'choose' ? 'external has no executable or live runtime'
    : 'select external, development, cached pin, or global';
  return labels[action] ?? action;
}
export function group({s}) {
  return ['inspect','select','trust'].includes(s.phase) ? 'selection'
    : ['missing','choose','stopAttached'].includes(s.phase) ? 'decision'
    : ['ready','providerUnavailable'].includes(s.phase) ? 'running' : 'startup';
}
export const colors = {selection:'blue', decision:'orange', running:'teal', startup:'purple'};
