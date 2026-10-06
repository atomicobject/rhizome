#!/bin/sh
# Rebuilds src-tauri/icons and src-tauri/icons-dev from the SVG masters in docs/brand.
# Each pixel size uses the master drawn for it: small (<=32px), medium (64px), large (the rest).
# macOS only: needs swift, sips, and iconutil.
set -eu
cd "$(dirname "$0")/.."
brand=../docs/brand
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# build <out-dir> [--dev]
build() {
  out=$1
  shift
  for m in small medium large; do
    suffix=-$m
    [ "$m" = large ] && suffix=
    swift scripts/render-icon.swift "$brand/rhizome-icon$suffix.svg" "$tmp/$m.png" "$@"
  done
  # resize <master> <px> <dest>
  resize() { sips -z "$2" "$2" "$tmp/$1.png" --out "$3" >/dev/null; }

  set=$tmp/icon.iconset
  rm -rf "$set"
  mkdir "$set"
  resize small 16 "$set/icon_16x16.png"
  resize small 32 "$set/icon_16x16@2x.png"
  resize small 32 "$set/icon_32x32.png"
  resize medium 64 "$set/icon_32x32@2x.png"
  resize large 128 "$set/icon_128x128.png"
  resize large 256 "$set/icon_128x128@2x.png"
  resize large 256 "$set/icon_256x256.png"
  resize large 512 "$set/icon_256x256@2x.png"
  resize large 512 "$set/icon_512x512.png"
  cp "$tmp/large.png" "$set/icon_512x512@2x.png"
  iconutil -c icns "$set" -o "$out/icon.icns"

  resize small 32 "$out/32x32.png"
  resize large 128 "$out/128x128.png"
  resize large 256 "$out/128x128@2x.png"
  resize large 512 "$out/icon.png"

  resize small 16 "$tmp/ico16.png"
  resize medium 48 "$tmp/ico48.png"
  resize medium 64 "$tmp/ico64.png"
  # ICO with embedded PNG entries (Windows Vista and later)
  python3 - "$out/icon.ico" "$tmp/ico16.png" "$out/32x32.png" "$tmp/ico48.png" "$tmp/ico64.png" "$out/128x128.png" "$out/128x128@2x.png" <<'EOF'
import struct, sys
out, files = sys.argv[1], sys.argv[2:]
pngs = [open(f, "rb").read() for f in files]
offset = 6 + 16 * len(pngs)
entries = b""
for png in pngs:
    w, h = struct.unpack(">II", png[16:24])
    entries += struct.pack("<BBBBHHII", w % 256, h % 256, 0, 0, 1, 32, len(png), offset)
    offset += len(png)
open(out, "wb").write(struct.pack("<HHH", 0, 1, len(pngs)) + entries + b"".join(pngs))
EOF
}

build src-tauri/icons
build src-tauri/icons-dev --dev
