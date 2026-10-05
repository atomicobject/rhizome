<?php
namespace Polyglot\Todo;

// Registration-style API call. The string argument 'theme_handler' names a function
// defined in theme.php; without string-argument callback resolution this would be a
// graph orphan. With it, hooks.php carries an outgoing call edge to theme_handler.
function bootstrap_theme(): void {
    add_action('init', 'theme_handler');
}
