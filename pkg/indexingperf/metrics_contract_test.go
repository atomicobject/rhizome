package indexingperf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductionObservationsUseSharedMetricIdentities(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	operations := map[string]bool{"AddCount": true, "AddBytes": true, "SetGauge": true, "ObserveLatency": true, "ObserveSample": true, "ObserveInterval": true, "MarkCountAvailable": true}
	for _, dir := range []string{"pkg", "cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if filePackage := filepath.Dir(path); filePackage != filepath.Join(root, "pkg/indexingperf") && !strings.Contains(string(source), "github.com/atomicobject/rhizome/pkg/indexingperf") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, source, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				var name string
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					pkg, ok := fn.X.(*ast.Ident)
					if !ok || pkg.Name != "indexingperf" {
						return true
					}
					name = fn.Sel.Name
				case *ast.Ident:
					if file.Name.Name != "indexingperf" {
						return true
					}
					name = fn.Name
				}
				if !operations[name] {
					return true
				}
				switch call.Args[1].(type) {
				case *ast.BasicLit, *ast.BinaryExpr:
					t.Errorf("%s: %s must use a shared MetricName or closed family selector", fset.Position(call.Pos()), name)
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
}

func TestClosedMetricSelectorsRejectUnknownReasonsAndBoundOpenLabels(t *testing.T) {
	require.Equal(t, MetricName("queue.flush.outcome.success"), QueueFlushOutcome.Select("success"))
	require.Equal(t, MetricName("provider.request.voyage.status.429"), ProviderHTTPStatus.Select("provider.request.voyage", "429"))
	require.Panics(t, func() { QueueFlushOutcome.Select("succes") })
	require.Panics(t, func() { ProviderHTTPStatus.Select("provider.request.voyage", "999") })
	require.Equal(t, MetricName("semantic.suppressed_anchors.other"), SuppressedAnchor.Select("arbitrary-source-text"))
	require.Equal(t, MetricName("search.retriever.auto_expand.refs.results"), SearchRetrieverResults.Select("auto_expand.refs"))
}
