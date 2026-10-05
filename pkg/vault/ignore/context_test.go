package ignore

import "testing"

func TestSystemContextClassificationUsesCanonicalPathNormalization(t *testing.T) {
	t.Parallel()

	if !IsSystemContextPath(`nested\service\CONTEXT.md`) {
		t.Fatal("expected Windows-style vault path to identify system context")
	}
	if IsSystemContextPath(`nested\service\context.md`) {
		t.Fatal("expected system context classification to remain case-sensitive")
	}
	if !IsDefaultInfrastructurePath(`nested\node_modules\package\CONTEXT.md`) {
		t.Fatal("expected Windows-style vault path to preserve infrastructure boundaries")
	}
}
