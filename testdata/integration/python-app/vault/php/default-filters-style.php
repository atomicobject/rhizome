<?php
namespace Polyglot\Todo;

// File-scope hook-registration shape, mirroring WordPress's default-filters.php.
// Surfaced as a gap by EFF-0037: v1.6.0 walker invoked phpCollectCalls only from inside
// function/method bodies, so file-scope add_action() / add_filter() and their string-arg
// callees produced zero CallSites. The two-pass walker (with stopAtBoundaries=true on
// the file-scope pass) extracts the outer registration call AND the string-arg callee,
// so polyglot_theme_setup gains an inbound edge from this file.
add_action('init', 'polyglot_theme_setup');

function polyglot_theme_setup(): void {
    // Body intentionally empty; the integration test only needs to confirm the call edge
    // from default-filters-style.php (file scope) to polyglot_theme_setup resolves.
}
