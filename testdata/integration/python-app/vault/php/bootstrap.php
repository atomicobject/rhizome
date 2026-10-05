<?php
// Dynamic include patterns surfaced by EFF-0035 catalog (real-gap):
//   ABSPATH . 'wp-cron.php'        -> {ABSPATH}wp-cron.php
//   __DIR__  . '/inc/' . 'helpers' -> {__DIR__}/inc/helpers.php
//   __DIR__  . '/inc/' . $name     -> {__DIR__}/inc/{var}
// The string-literal-only v1 indexer silently dropped these; with phpExtractIncludePath
// they surface as partial-info ImportEdge so the include graph isn't blank.

require __DIR__ . '/Container.php';
require ABSPATH . 'wp-cron.php';
require __DIR__ . '/inc/' . 'helpers.php';

$name = 'theme.php';
require __DIR__ . '/inc/' . $name;
