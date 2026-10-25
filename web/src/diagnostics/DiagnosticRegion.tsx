import { Profiler, type ReactNode } from "react";

import { recordCommit, type RegionName } from "./clientMetrics";

/**
 * Counts commits and render time for a fixed, source-defined region. Production
 * builds use React's profiling build so this also works in the desktop app.
 */
export function DiagnosticRegion({ name, children }: { name: RegionName; children: ReactNode }) {
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
