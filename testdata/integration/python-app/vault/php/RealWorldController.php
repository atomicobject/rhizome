<?php
namespace Polyglot\Todo\Http;

use Polyglot\Todo\SyncClient;

#[Route('/tasks/{id}', methods: ['POST'])]
class TaskController extends BaseController implements SyncGateway {
    public function __construct(private SyncClient $client) {}

    /**
     * Store a submitted task.
     * See [[notes/task-flow]] and @release-plan for rollout context.
     * Contact support@example.com only in human-facing docs.
     */
    public function store(TaskStatus $status): void {
        if ($status === TaskStatus::Pending) {
            $this->client->pushUpdates("controller-store");
            SyncClient::fromContainer()->pushUpdates("static-factory");
            TaskEvent::dispatch($status);
            $ignored = "@release-plan";
        }
    }
}

class BaseController {}

interface SyncGateway {}

class Route {
    public function __construct(string $path, array $methods = []) {}
}

class TaskEvent {
    public static function dispatch(TaskStatus $status): void {}
}

enum TaskStatus: string {
    case Pending = 'pending';
    case Done = 'done';
}
