<?php
namespace Polyglot\Todo;

// Magic methods exercise the method_declaration walker. Catalog 2026-06-12 listed magic
// methods as a potential real-gap; v1 indexer emits them without name filtering.
class Container {
    public function __construct(private array $bindings = []) {}

    public function __invoke(string $key) {
        return $this->bindings[$key] ?? null;
    }

    public function __toString(): string {
        return 'Container';
    }
}
