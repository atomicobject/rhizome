<?php
namespace Polyglot\Todo;

/**
 * Service that mutates the global task list.
 */
class TaskService {
    /**
     * AddTask creates a new task and persists it.
     */
    public function addTask(string $title): void {
        $task = new Task($title);
        $this->save($task);
    }

    private function save(Task $t): void {
        // persistence elided
    }
}

class Task {
    public string $title;

    public function __construct(string $title) {
        $this->title = $title;
    }
}
