// Model profile resolution (SPEC-0124, "Model profile"). Pure apart from the
// injected probe, which makes one tiny generate call and returns the model
// that actually ran.

const REQUIRED = new Set(["extract", "verify"]);

// Merge shipped, local, and run-flag profiles. An override replaces a role's
// whole candidate list. Flags look like `extract=claude:claude-haiku-5-5:xhigh`.
export function mergeProfile(shipped, local = null, flags = []) {
  const roles = {};
  for (const [role, list] of Object.entries(shipped.roles)) roles[role] = list.map((c) => ({ ...c, source: "default" }));
  for (const [role, list] of Object.entries(local?.roles ?? {})) roles[role] = list.map((c) => ({ ...c, source: "local" }));
  for (const flag of flags) {
    const m = flag.match(/^(\w+)=(\w+):([^:]+):(\w*)$/);
    if (!m) throw new Error(`--model ${flag}: expected <role>=<provider>:<model>:<effort>`);
    roles[m[1]] = [{ provider: m[2], model: m[3], effort: m[4], source: "flag" }];
  }
  return { version: shipped.version, providers: local?.providers ?? null, roles };
}

export function versionOf(id) {
  return (String(id).replace(/\[.*\]$/, "").match(/\d+/g) ?? []).slice(0, 2).map(Number);
}

function compare(a, b) {
  for (let i = 0; i < Math.max(a.length, b.length); i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) - (b[i] ?? 0);
  return 0;
}

// The newest catalog model of the candidate's family at or above its measured
// version; else the family alias (Claude lists only aliases); else the
// candidate's own model, for the probe to settle.
function request(candidate, models) {
  if (!candidate.family) return { id: candidate.model };
  const matches = models.filter((m) => m.id.split(/[-.\[]/).includes(candidate.family));
  const versioned = matches.filter((m) => versionOf(m.id).length).sort((a, b) => compare(versionOf(b.id), versionOf(a.id)));
  if (versioned[0] && compare(versionOf(versioned[0].id), versionOf(candidate.version)) >= 0) return versioned[0];
  return matches.find((m) => !versionOf(m.id).length) ?? { id: candidate.model };
}

export async function resolve(profile, { providers, statuses, probe, cache = new Map() }) {
  const bindings = {};
  for (const [role, candidates] of Object.entries(profile.roles)) {
    const skipped = [];
    let binding = null;
    for (const candidate of candidates) {
      if (candidate.provider !== "typesafe" && !providers.includes(candidate.provider)) continue;
      if (candidate.provider === "typesafe") {
        binding = { provider: "typesafe", requested: candidate.model, model: candidate.model, effort: candidate.effort, choice: candidate.source };
        break;
      }
      const status = statuses.find((s) => s.kind === candidate.provider);
      if (!status?.installed || !status.loggedIn) {
        skipped.push({ model: candidate.model, reason: status?.installed ? "not signed in" : "not installed" });
        continue;
      }
      const option = request(candidate, status.models ?? []);
      if (option.efforts?.length && candidate.effort && !option.efforts.includes(candidate.effort)) {
        skipped.push({ model: option.id, reason: `effort ${candidate.effort} not offered` });
        continue;
      }
      const key = [candidate.provider, option.id, candidate.effort, status.version].join("|");
      let model = cache.get(key);
      if (!model) {
        try {
          model = await probe({ provider: candidate.provider, model: option.id, effort: candidate.effort, candidate });
          cache.set(key, model);
        } catch (err) {
          skipped.push({ model: option.id, reason: `probe failed: ${err.message}` });
          continue;
        }
      }
      // A newer version with its own profile entry uses that entry's effort.
      const order = candidate.version ? compare(versionOf(model), versionOf(candidate.version)) : 0;
      const entry = order > 0 && candidates.find((c) => c.provider === candidate.provider && c.family === candidate.family && compare(versionOf(c.version), versionOf(model)) === 0);
      binding = {
        provider: candidate.provider,
        requested: option.id,
        model,
        effort: entry ? entry.effort : candidate.effort,
        choice: skipped.length ? "fallback" : order > 0 && !entry ? "newer-version" : candidate.source,
        unmeasured: order !== 0 && !entry,
      };
      break;
    }
    if (!binding && REQUIRED.has(role)) {
      const why = skipped.map((s) => `${s.model}: ${s.reason}`).join("; ") || `no candidate for ${providers.join(", ")}`;
      throw new Error(`no model for ${role} (${why})`);
    }
    bindings[role] = binding && { ...binding, skipped };
  }
  return { profileVersion: profile.version, providers, bindings };
}
