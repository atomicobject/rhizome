<?php
namespace Polyglot\Todo\Worker;

use Polyglot\Todo\SyncClient;

class Worker {
    public function run(): void {
        $client = new SyncClient();
        $client->pushUpdates("worker-run");
    }
}
