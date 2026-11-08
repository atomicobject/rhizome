#!/bin/sh
# Regenerate the chart and report: ./regenerate.sh <new output directory>
# Needs the t3-pstack statechart skill; set STATECHART to its scripts/statechart.
here=$(cd "$(dirname "$0")" && pwd)
out=${1:?Pass a new output directory}
statechart=${STATECHART:-statechart}
exec "$statechart" "$here/Selection.tla" --config "$here/Selection.cfg" --view "$here/view.mjs" --out "$out"
