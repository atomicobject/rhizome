// The "Experience frames" measurement (SPEC-0136 R10, SPEC-0116 "Web UI
// evidence"): startup phase timings and frame-layer counts per experience per
// window. An experience is named by a hash of its key, never the key itself,
// because the key is a views folder path. Like every client counter this is
// module state, so recording cannot cause a render, and it records only while
// render profiling is on.
import type { ExperiencePhase } from "../lib/customViewMessages";
import { renderProfiling } from "./clientMetrics";

/** A phase the kit marks, or the host's own paint signal for a location. */
export type ExperienceMark = ExperiencePhase | "location-painted";

export type ExperienceCount = "frames_created" | "locations_reused" | "resumes" | "evictions";

type PhaseStats = { count: number; p50_ms: number; p95_ms: number };

export type ExperienceStats = Record<ExperienceCount, number> & {
  /** The most pages the experience's frame held at once in the window. */
  pages_retained: number;
  phases: Partial<Record<ExperienceMark, PhaseStats>>;
};

type Tally = Record<ExperienceCount, number> & {
  pages_retained: number;
  samples: Map<ExperienceMark, number[]>;
};

// Bounds a window: the batch route takes at most 32 experiences, and a phase
// past this many samples in ten seconds adds nothing to its percentiles.
const MAX_EXPERIENCES = 16;

const MAX_SAMPLES = 256;

let windows = new Map<string, Tally>();

const hashes = new Map<string, string>();

// ponytail: cyrb53, a 53-bit non-cryptographic hash; the identity space is a
// handful of folder names, so any hash of it is guessable, and this one only
// keeps the path itself out of the batch.
function experienceHash(key: string): string {
  let cached = hashes.get(key);

  if (cached) return cached;
  let h1 = 0xdeadbeef;
  let h2 = 0x41c6ce57;

  for (let i = 0; i < key.length; i++) {
    const ch = key.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }

  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  cached = (4294967296 * (2097151 & h2) + (h1 >>> 0)).toString(16).padStart(14, "0");
  hashes.set(key, cached);

  return cached;
}

function windowFor(experienceKey: string): Tally | null {
  if (!renderProfiling()) return null;
  const hash = experienceHash(experienceKey);
  let stats = windows.get(hash);

  if (!stats) {
    if (windows.size >= MAX_EXPERIENCES) return null;
    stats = {
      frames_created: 0,
      locations_reused: 0,
      pages_retained: 0,
      resumes: 0,
      evictions: 0,
      samples: new Map(),
    };
    windows.set(hash, stats);
  }

  return stats;
}

export function recordExperienceCount(experienceKey: string, count: ExperienceCount) {
  const stats = windowFor(experienceKey);

  if (stats) stats[count]++;
}

/** The number of pages the experience's frame holds now. */
export function recordPagesRetained(experienceKey: string, pages: number) {
  const stats = windowFor(experienceKey);

  if (stats) stats.pages_retained = Math.max(stats.pages_retained, pages);
}

/** One phase's time since its start: the frame's creation, or the page's navigation. */
export function recordExperiencePhase(experienceKey: string, mark: ExperienceMark, ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return;
  const stats = windowFor(experienceKey);

  if (!stats) return;
  const samples = stats.samples.get(mark) ?? [];

  if (samples.length < MAX_SAMPLES) samples.push(ms);
  stats.samples.set(mark, samples);
}

const round = (ms: number) => Math.round(ms * 100) / 100;

// Nearest rank: the smallest sample with at least p of the samples at or below it.
const percentile = (sorted: number[], p: number) =>
  sorted[Math.max(0, Math.ceil(p * sorted.length) - 1)] ?? 0;

/** Closes the window: each experience's counts and phase percentiles, by hash. */
export function takeExperiences(): Record<string, ExperienceStats> {
  const closed = windows;
  windows = new Map();

  return Object.fromEntries(
    [...closed].map(([hash, { samples, ...counts }]) => [
      hash,
      {
        ...counts,
        phases: Object.fromEntries(
          [...samples].map(([mark, values]) => {
            const sorted = [...values].sort((a, b) => a - b);

            return [
              mark,
              {
                count: sorted.length,
                p50_ms: round(percentile(sorted, 0.5)),
                p95_ms: round(percentile(sorted, 0.95)),
              },
            ];
          }),
        ),
      },
    ]),
  );
}

export function resetExperienceMetrics() {
  windows = new Map();
}
