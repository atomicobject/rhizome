<?php
namespace Polyglot\Todo;

/**
 * SyncClient pushes task-flow updates to downstream consumers.
 * See [[notes/sync-strategy]] for the cross-language behavior contract.
 */
class SyncClient {
    public function pushUpdates(string $payload): void {
        // keep the PHP sync client behavior in sync with the shared docs
        Logger::info($payload);
    }
}
