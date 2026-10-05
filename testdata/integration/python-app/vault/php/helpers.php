<?php
// Procedural / global-scope file. Mimics legacy PHP without namespaces.

require_once 'shared.php';

function format_payload(string $title): string {
    return "todo: " . $title;
}

function notify(string $payload): void {
    format_payload($payload);
}
