import { Profiler, type ReactNode } from "react";

import { recordCommit, type RegionName } from "./clientMetrics";

declare global {
  interface Window {
    /** Set by Rhizome Desktop when render timing is on (always in Rhizome Dev). */
    __RHIZOME_RENDER_PROFILING__?: boolean;
  }
}

// Read once at load: switching between a Profiler and a bare subtree mid-session
// would remount everything below it. Production ships React's profiling build,
// which costs about 4.5% scripting time with no regions mounted but about 50%
// with them (SPEC-0116 D3), so regions stay off unless asked for.
const regionsEnabled = import.meta.env.DEV || window.__RHIZOME_RENDER_PROFILING__ === true;

/** Counts commits and render time for a fixed, source-defined region. */
export function DiagnosticRegion({ name, children }: { name: RegionName; children: ReactNode }) {
  if (!regionsEnabled) return children;

  return (
    <Profiler
      id={name}
      onRender={(_id, phase, actualDuration, baseDuration) =>
        recordCommit(name, phase, actualDuration, baseDuration)
      }
    >
      {children}
    </Profiler>
  );
}
