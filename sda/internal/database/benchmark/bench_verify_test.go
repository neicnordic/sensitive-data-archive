package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stripTrailingWS strips trailing spaces/tabs per line. Editors and gofmt
// frequently strip trailing whitespace, and Karl's SQL has trailing spaces
// after some keywords; comparing post-strip avoids false drift on noise.
func stripTrailingWS(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

func TestBenchVerifyCopiedSQL(t *testing.T) {
	methodDir := "../postgres"
	entries, err := os.ReadDir(methodDir)
	if err != nil {
		t.Skipf("cannot read %s: %v (expected on baseline-main worktree)", methodDir, err)
	}

	var combined strings.Builder
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "method_") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(methodDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}

	src := stripTrailingWS(combined.String())
	for name, q := range queries {
		if !strings.Contains(src, stripTrailingWS(q)) {
			t.Errorf("query %q not found in any method_*.go — copied SQL has drifted", name)
		}
	}
}
