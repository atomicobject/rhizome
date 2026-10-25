import { Profiler, type ReactNode } from "react";

import { recordCommit, type RegionName } from "./clientMetrics";

/**
 * Counts commits and render time for a fixed, source-defined region during
 * development. Production builds omit it: React's profiling build added about
 * 50% scripting time to a heavy type-view table (SPEC-0116 D3), and the normal
 * production build never reports Profiler timings.
 */
export function DiagnosticRegion({ name, children }: { name: RegionName; children: ReactNode }) {
  if (!import.meta.env.DEV) return children;

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
