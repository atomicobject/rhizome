package app

import "fmt"

// Package app exists to exercise coderefs scanning in Go.
//
// Coderefs in Go are extracted from C-style comments (// and /* ... */) and
// then scanned for wikilinks and @mentions.
//
// Docs:
// - [[notes/task-flow]] (baseline wikilink)
// - [[notes/task-flow|Task Flow]] (alias)
// - [[notes/task-flow#Task Flow]] (anchor)
// - ![[notes/sync-strategy]] (embed syntax should still be treated as a link)
// - @notes/task-flow (mention)
// - @notes/sync-strategy (mention)
// - support@company.com (should NOT match a mention)
// - "[@notes/task-flow]" and "[[notes/task-flow]]" inside a string should be ignored.
func DemoCoderefs() {
	fmt.Println("ignore: [[notes/task-flow]] @notes/task-flow")
}

/*
Block comment coderefs:
- [[notes/product-brief]]
- @notes/release-plan
- foo@notes/task-flow (should NOT match because it's part of a word)
*/
func DemoBlockComments() {}
