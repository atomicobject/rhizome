# Upstream

Vendored from https://github.com/dmmulroy/anti-slop at commit
`c44ef22ca116d0ba62a3ff663a0bd13a3f3fa40b` (2026-09-10),
`skills/install-anti-slop/assets/anti-slop`.

The previous copy matched the skill bundle installed on 2026-09-06 byte for byte once
formatted, so this update replaced it wholesale. The only local deviation is formatting by
this repository's oxfmt configuration. Policy lives in `web/.oxlintrc.json`:
`no-runtime-typeof` keeps `allowInTypeGuards`, and the Effect plugin stays off because
`web/` does not depend on Effect.
