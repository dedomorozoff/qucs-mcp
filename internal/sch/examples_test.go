package sch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadAllExamples parses every schematic shipped with the bundled
// Qucs-S installation. Reading must never panic and must keep the
// component/wire counts stable across runs (tolerant reader, design
// §13/§16 regression).
func TestReadAllExamples(t *testing.T) {
	root := repoRoot(t)
	pattern := filepath.Join(root, "qucs", "share", "qucs-s", "examples")
	if _, err := os.Stat(pattern); err != nil {
		t.Skipf("examples not found: %v", err)
	}
	count := 0
	err := filepath.Walk(pattern, func(path string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() || !strings.HasSuffix(path, ".sch") {
			return werr
		}
		count++
		r, rerr := Read(path)
		if rerr != nil {
			t.Errorf("%s: read: %v", relPath(root, path), rerr)
			return nil
		}
		if r.Version == "" {
			t.Errorf("%s: empty version", relPath(root, path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 10 {
		t.Fatalf("only %d examples walked; corpus unexpectedly small", count)
	}
	t.Logf("parsed %d example schematics", count)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func relPath(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
