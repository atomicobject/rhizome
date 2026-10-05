---
title: "PHP anchor - Task Flow"
summary: "PHP code anchors for the Task Flow docs: symbols and call-sites in the PHP fixture."
tags: [type/reference, subsystem/codeanchor]
code-anchors:
  php:
    - label: php-add-task
      symbol: Polyglot\Todo\TaskService::addTask
    - label: php-push-updates
      symbol: Polyglot\Todo\SyncClient::pushUpdates
    - label: php-push-updates-callers
      symbol: Polyglot\Todo\SyncClient::pushUpdates
    - label: php-task-controller
      symbol: Polyglot\Todo\Http\TaskController::store
    - label: php-task-controller-subclasses
      baseClass: Polyglot\Todo\Http\BaseController
    - label: php-route-attribute
      decorator: Polyglot\Todo\Http\Route
      args:
        methods: POST
---

# PHP anchor - Task Flow

![[task-flow]]
