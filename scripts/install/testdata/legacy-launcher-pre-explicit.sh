#!/usr/bin/env bash
# RZM MANAGED LAUNCHER
set -euo pipefail

BASE_URL="${RZM_RELEASE_BASE_URL:-https://releases.example.invalid}"
MANIFEST_URL="${RZM_UPDATE_MANIFEST_URL:-${BASE_URL}/latest.json}"

find_root() {
  local dir
  dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  while [[ "$dir" != "/" ]]; do
    if [[ -f "$dir/.rhizome/config.yml" ]]; then
      printf '%s\n' "$dir"
      return 0
    fi
    dir="$(dirname "$dir")"
  done
  echo "error: could not find .rhizome/config.yml above launcher" >&2
  return 1
}
need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: $1 is required" >&2
    exit 1
  fi
}
detect_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin\n' ;;
    Linux) printf 'linux\n' ;;
    MINGW*|MSYS*|CYGWIN*) printf 'windows\n' ;;
    *) echo "error: unsupported OS $(uname -s)" >&2; exit 1 ;;
  esac
}
detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64\n' ;;
    x86_64|amd64) printf 'amd64\n' ;;
    *) echo "error: unsupported architecture $(uname -m)" >&2; exit 1 ;;
  esac
}
repo_pin() {
  perl - "$1" <<'PL'
use strict; use warnings;
open my $fh, "<", $ARGV[0] or die "repo_pin: open $ARGV[0]: $!\n";
my $inside = 0;
my $child_indent;
while (my $line = <$fh>) {
  chomp $line;
  if ($line =~ /^rhizome:\s*(?:#.*)?$/) { $inside = 1; next; }
  if ($inside && $line =~ /^[^ \t#].*:/) { last; }
  next unless $inside;
  next if $line =~ /^\s*(?:#.*)?$/;
  $child_indent = $1 if !defined($child_indent) && $line =~ /^( +)\S/;
  if (defined($child_indent) && $line =~ /^\Q$child_indent\Eversion:\s*(\S+)(?:\s+#.*)?\s*$/) {
    my $v = $1;
    $v =~ s/^["']|["']$//g;
    print "$v\n";
    exit 0;
  }
}
die "rhizome.version missing in .rhizome/config.yml\n";
PL
}
repo_dev_binary_dir() {
  perl - "$1" <<'PL'
use strict; use warnings;
open my $fh, "<", $ARGV[0] or die "repo_dev_binary_dir: open $ARGV[0]: $!\n";
my $inside = 0;
my $child_indent;
while (my $line = <$fh>) {
  chomp $line;
  if ($line =~ /^rhizome:\s*(?:#.*)?$/) { $inside = 1; next; }
  if ($inside && $line =~ /^[^ \t#].*:/) { last; }
  next unless $inside;
  next if $line =~ /^\s*(?:#.*)?$/;
  $child_indent = $1 if !defined($child_indent) && $line =~ /^( +)\S/;
  if (defined($child_indent) && $line =~ /^\Q$child_indent\EdevBinaryDir:\s*(.+?)(?:\s+#.*)?\s*$/) {
    my $v = $1;
    $v =~ s/^["']|["']$//g;
    print "$v\n";
    exit 0;
  }
}
exit 1;
PL
}
configured_dev_binary() {
  local root="$1" os_name="$2" exe="$3" dev_dir base target
  if ! dev_dir="$(repo_dev_binary_dir "$root/.rhizome/config.yml" 2>/dev/null)"; then
    return 1
  fi
  if [[ "$dev_dir" = /* ]]; then
    base="$dev_dir"
  else
    base="$root/$dev_dir"
  fi
  target="$base/${os_name}/${exe}"
  if [[ -x "$target" ]]; then
    printf '%s\n' "$target"
    return 0
  fi
  if [[ -e "$target" ]]; then
    echo "error: configured rhizome.devBinaryDir target is not executable: $target" >&2
  else
    echo "error: configured rhizome.devBinaryDir target missing: $target; run make build" >&2
  fi
  return 2
}

json_get() {
  perl - "$1" "$2" <<'PL'
use strict; use warnings; use JSON::PP;
my ($path, $key) = @ARGV;
open my $fh, "<", $path or die "json_get: open $path: $!\n";
local $/;
my $data = decode_json(scalar <$fh>);
for my $part (split /\./, $key) {
  unless (ref($data) eq 'HASH' && exists $data->{$part}) {
    die "json_get: missing key '$key' (at '$part') in $path\n";
  }
  $data = $data->{$part};
}
print "$data\n";
PL
}

managed_launcher_path() {
  local dir base
  dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  base="$(basename "${BASH_SOURCE[0]}")"
  printf '%s/%s\n' "$dir" "$base"
}

rhizome_dev_binary() {
  local dir="$1" os_name="$2" exe="$3" target
  while [[ "$dir" != "/" ]]; do
    if [[ -f "$dir/go.mod" ]] && grep -q '^module github.com/atomicobject/rhizome$' "$dir/go.mod" 2>/dev/null; then
      target="$dir/bin/${os_name}/${exe}"
      [[ -x "$target" ]] && printf '%s\n' "$target" && return 0
      return 1
    fi
    dir="$(dirname "$dir")"
  done
  return 1
}

normalize_version() {
  local version="$1"
  if [[ "$version" == v* ]]; then
    printf '%s\n' "$version"
  else
    printf 'v%s\n' "$version"
  fi
}

rzm_debug() {
  [[ "${RZM_DEBUG:-0}" == "1" ]] || return 0
  printf '[rzm-shim] %s\n' "$*" >&2
}

pinned_binary_current() {
  local target="$1" version="$2" output normalized last
  if [[ ! -x "$target" ]]; then
    rzm_debug "no cached binary at $target"
    return 1
  fi
  version="$(normalize_version "$version")"
  if ! output="$("$target" --version 2>/dev/null)"; then
    rzm_debug "cached $target --version failed; will redownload"
    return 1
  fi
  normalized="$(normalize_version "$output")"
  last="$(normalize_version "${output##* }")"
  if [[ "$normalized" == "$version" || "$last" == "$version" ]]; then
    rzm_debug "cached binary matches pin $version (raw='$output')"
    return 0
  fi
  rzm_debug "cached binary $target=$output (normalized='$normalized', last='$last') != pin $version"
  return 1
}

install_pinned() {
  local root="$1" version="$2" target="$3" tmp_dir manifest platform url sha archive actual extract bin staged
  need curl
  need tar
  need perl
  if command -v shasum >/dev/null 2>&1; then
    sha_cmd=(shasum -a 256)
  elif command -v sha256sum >/dev/null 2>&1; then
    sha_cmd=(sha256sum)
  else
    echo "error: shasum or sha256sum is required" >&2
    exit 1
  fi
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' RETURN
  manifest="$tmp_dir/latest.json"
  version="$(normalize_version "$version")"
  curl -fsSL "${BASE_URL}/${version}/latest.json" -o "$manifest"
  platform="$(detect_os)/$(detect_arch)"
  url="$(json_get "$manifest" "artifacts.${platform}.url")"
  sha="$(json_get "$manifest" "artifacts.${platform}.sha256")"
  archive="$tmp_dir/rzm.tbz"
  curl -fsSL "$url" -o "$archive"
  actual="$("${sha_cmd[@]}" "$archive" | awk '{print $1}')"
  if [[ "$actual" != "$sha" ]]; then
    echo "error: sha256 mismatch for $url" >&2
    exit 1
  fi
  extract="$tmp_dir/extract"
  mkdir -p "$extract" "$(dirname "$target")"
  tar -xjf "$archive" -C "$extract"
  bin="$extract/rzm"
  if [[ ! -f "$bin" ]]; then
    bin="$extract/rzm.exe"
  fi
  if [[ ! -f "$bin" ]]; then
    echo "error: archive did not contain rzm" >&2
    exit 1
  fi
  chmod 755 "$bin"
  "$bin" --version >/dev/null
  staged="${target}.new"
  install -m 0755 "$bin" "$staged"
  mv "$staged" "$target"
}

update_launcher() {
  local root="$1" self="$2" tmp_dir manifest url sha installer actual
  need curl
  need perl
  if command -v shasum >/dev/null 2>&1; then
    sha_cmd=(shasum -a 256)
  elif command -v sha256sum >/dev/null 2>&1; then
    sha_cmd=(sha256sum)
  else
    echo "error: shasum or sha256sum is required" >&2
    exit 1
  fi
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' RETURN
  manifest="$tmp_dir/latest.json"
  curl -fsSL "$MANIFEST_URL" -o "$manifest"
  url="$(json_get "$manifest" "installer.url")"
  sha="$(json_get "$manifest" "installer.sha256")"
  installer="$tmp_dir/install-rzm.sh"
  curl -fsSL "$url" -o "$installer"
  actual="$("${sha_cmd[@]}" "$installer" | awk '{print $1}')"
  if [[ "$actual" != "$sha" ]]; then
    echo "error: sha256 mismatch for $url" >&2
    exit 1
  fi
  chmod 755 "$installer"
  (cd "$root" && "$installer" --yes "$self")
}

root="$(find_root)"
os_name="$(detect_os)"
arch_name="$(detect_arch)"
exe="rzm"
if [[ "$os_name" == "windows" ]]; then
  exe="rzm.exe"
fi
if dev_target="$(configured_dev_binary "$root" "$os_name" "$exe")"; then
  rzm_debug "using configured dev binary $dev_target"
  exec "$dev_target" "$@"
else
  dev_status=$?
  if [[ "$dev_status" == "2" ]]; then
    exit 1
  fi
fi
# Any pinned `rzm update [...]` invocation refreshes the managed launcher itself
# first so users picking up new shim behavior (download semantics, debug
# breadcrumbs, new flags) don't have to remember a separate command. A configured
# dev binary is authoritative and intercepts update before this point.
if [[ "${1:-}" == "update" ]]; then
  update_launcher "$root" "$(managed_launcher_path)"
fi
version="$(repo_pin "$root/.rhizome/config.yml")"
if [[ -z "$version" ]]; then
  echo "error: rhizome.version is empty in $root/.rhizome/config.yml" >&2
  exit 1
fi
rzm_debug "root=$root pin=$version"
target="$root/.rhizome/bin/${os_name}-${arch_name}/${exe}"
if [[ "${1:-}" != "update" ]] && dev_target="$(rhizome_dev_binary "$root" "$os_name" "$exe" 2>/dev/null)"; then
  rzm_debug "using dev binary $dev_target (skipping pin check)"
  exec "$dev_target" "$@"
fi
if ! pinned_binary_current "$target" "$version" || ( [[ "${1:-}" == "update" ]] && [[ "${2:-}" == "--pinned" ]] ); then
  install_pinned "$root" "$version" "$target"
  if [[ "${1:-}" == "update" && "${2:-}" == "--pinned" ]]; then
    echo "Updated $target to Rhizome $version"
    exit 0
  fi
fi
exec "$target" "$@"
