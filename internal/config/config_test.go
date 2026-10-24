package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	c := Load()
	if c.QucsRoot == "" {
		t.Fatal("QucsRoot empty")
	}
	if got := c.QucsExe(); filepath.Base(got) != "qucs-s.exe" {
		t.Fatalf("QucsExe() = %q, want ...\\qucs-s.exe", got)
	}
	if len(c.WorkDir) == 0 {
		t.Fatal("WorkDir empty")
	}
	if c.Timeout != 2*time.Minute {
		t.Fatalf("Timeout = %v, want 2m", c.Timeout)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("QUCS_S_ROOT", "/tmp/q")
	t.Setenv("QUCS_MCP_WORKDIR", "/tmp/w")
	t.Setenv("QUCS_MCP_TIMEOUT", "90")
	c := Load()
	if c.QucsRoot != "/tmp/q" {
		t.Fatalf("QucsRoot = %q, want /tmp/q", c.QucsRoot)
	}
	if c.WorkDir != "/tmp/w" {
		t.Fatalf("WorkDir = %q, want /tmp/w", c.WorkDir)
	}
	if c.Timeout != 90*time.Second {
		t.Fatalf("Timeout = %v, want 90s", c.Timeout)
	}
}

func TestLocalQucsDirPreferred(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "qucs", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "qucs", "bin", "qucs-s.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUCS_S_ROOT", "")
	t.Chdir(dir)
	c := Load()
	if c.QucsRoot != filepath.Join(dir, "qucs") {
		t.Fatalf("QucsRoot = %q, want local dir", c.QucsRoot)
	}
}

func TestQucsExeJoin(t *testing.T) {
	c := Config{QucsRoot: `C:\q`}
	if got, want := c.QucsExe(), `C:\q\bin\qucs-s.exe`; got != want {
		t.Fatalf("QucsExe() = %q, want %q", got, want)
	}
}
