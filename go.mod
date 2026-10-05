module github.com/atomicobject/rhizome

go 1.24.0

toolchain go1.24.2

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/atotto/clipboard v0.1.4
	github.com/bmatcuk/doublestar/v4 v4.6.1
	github.com/evanw/esbuild v0.28.2
	github.com/fsnotify/fsevents v0.2.0
	github.com/fsnotify/fsnotify v1.8.0
	github.com/go-git/go-git/v5 v5.16.4
	github.com/ktr0731/go-fuzzyfinder v0.8.0
	github.com/mark3labs/mcp-go v0.34.0
	github.com/mattn/go-isatty v0.0.20
	github.com/mattn/go-sqlite3 v1.14.48
	github.com/sergi/go-diff v1.4.0
	github.com/skratchdot/open-golang v0.0.0-20200116055534-eef842397966
	github.com/spf13/cobra v1.8.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.11.1
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-c-sharp v0.23.2-0.20260217170834-88366631d598
	github.com/tree-sitter/tree-sitter-javascript v0.25.0
	github.com/tree-sitter/tree-sitter-python v0.25.0
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	github.com/vektah/gqlparser/v2 v2.5.32
	golang.org/x/net v0.39.0
	golang.org/x/sys v0.36.0
	golang.org/x/term v0.31.0
	golang.org/x/text v0.24.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/agnivade/levenshtein v1.2.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/gdamore/tcell/v2 v2.7.4 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.6.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/ktr0731/go-ansisgr v0.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/nsf/termbox-go v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/tree-sitter/tree-sitter-php v0.24.2 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
)

replace github.com/tree-sitter/tree-sitter-python/bindings/go => github.com/tree-sitter/tree-sitter-python v0.25.0

replace github.com/tree-sitter/tree-sitter-typescript/bindings/go => github.com/tree-sitter/tree-sitter-typescript v0.23.2

replace github.com/tree-sitter/tree-sitter-c-sharp/bindings/go => github.com/tree-sitter/tree-sitter-c-sharp v0.23.2-0.20260217170834-88366631d598
replace github.com/tree-sitter/tree-sitter-php/bindings/go => github.com/tree-sitter/tree-sitter-php v0.24.2
