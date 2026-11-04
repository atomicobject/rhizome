#!/usr/bin/env bash
# Frozen vault for design-loop screenshots: this repository at a pinned commit, so content never moves the scores.
#   snapshot.sh setup <dir> [commit]          create, index (no embeddings), restore file times from Git
#   snapshot.sh serve <dir> <port> <rzm-bin>  wipe saved view state, then serve with the given build
# Run serve outside the sandbox; the server listens on 127.0.0.1 only.
set -euo pipefail
cmd=$1 dir=$2
clean_env=(env -u VOYAGE_API_KEY -u OPENAI_API_KEY -u ATOMIC_RHIZOME_KEY -u TYPESAFE_API_KEY RZM_SKIP_REPO_DELEGATE=1)

case $cmd in
setup)
  commit=${3:-origin/main}
  repo=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
  git -C "$repo" worktree add --detach "$dir" "$commit"
  cd "$dir"
  # Never send note text to an embedding provider for a throwaway index.
  sed -i.bak -E '/^(note|code)Embeddings:/,/enabled:/ s/enabled: true/enabled: false/' .rhizome/config.yml && rm .rhizome/config.yml.bak
  git update-index --skip-worktree .rhizome/config.yml
  # Checkout stamps every file "now"; restore last-commit times so recency views look real.
  git log --format='T%ct' --name-only HEAD | python3 -I -c '
import os, sys
seen, t = set(), None
for line in sys.stdin:
    line = line.rstrip("\n")
    if line.startswith("T") and line[1:].isdigit():
        t = int(line[1:]); continue
    if line and line not in seen and os.path.exists(line):
        seen.add(line); os.utime(line, (t, t))'
  "${clean_env[@]}" "$(git -C "$repo" rev-parse --show-toplevel)/bin/$(go env GOOS)/rzm" index --in-process --rebuild
  ;;
serve)
  port=$3 rzm=$4
  cd "$dir"
  rm -f .rhizome/user-state.sqlite .rhizome/user-state.sqlite-shm .rhizome/user-state.sqlite-wal
  exec "${clean_env[@]}" "$rzm" serve --port "$port" --reuse-discovery-port=false
  ;;
*) echo "usage: snapshot.sh setup <dir> [commit] | serve <dir> <port> <rzm-bin>" >&2; exit 2 ;;
esac
