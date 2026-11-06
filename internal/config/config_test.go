package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The release workflow stamps version and commit with -ldflags -X, so the
// version the server reports has to be the one the linker wrote.
func TestBuildInfoDescribesTheRunningBinary(t *testing.T) {
	info := BuildInfo()
	for _, want := range []string{version, runtime.GOOS, runtime.GOARCH} {
		if !strings.Contains(info, want) {
			t.Errorf("BuildInfo() = %q, missing %q", info, want)
		}
	}
	if c := Load(); c.ServerVersion != version {
		t.Errorf("ServerVersion = %q, want the stamped version %q", c.ServerVersion, version)
	}
}

// fakeInstall creates <dir>/bin/qucs-s[.exe] so discovery can find it.
func fakeInstall(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", binaryName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("QUCS_S_ROOT", fakeRoot(t))
	t.Setenv("QUCS_MCP_WORKDIR", "")
	c := Load()
	if c.QucsRoot == "" {
		t.Fatal("QucsRoot empty")
	}
	if base := filepath.Base(c.QucsExe()); base != binaryName() {
		t.Fatalf("QucsExe() = %q, want .../%s", c.QucsExe(), binaryName())
	}
	if len(c.WorkDir) == 0 {
		t.Fatal("WorkDir empty")
	}
	if c.Timeout != 2*time.Minute {
		t.Fatalf("Timeout = %v, want 2m", c.Timeout)
	}
}

func fakeRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fakeInstall(t, dir)
	return dir
}

func TestLoadEnvOverrides(t *testing.T) {
	root := fakeRoot(t)
	t.Setenv("QUCS_S_ROOT", root)
	t.Setenv("QUCS_MCP_WORKDIR", "/tmp/w")
	t.Setenv("QUCS_MCP_TIMEOUT", "90")
	c := Load()
	if c.QucsRoot != root {
		t.Fatalf("QucsRoot = %q, want %q", c.QucsRoot, root)
	}
	if c.WorkDir != "/tmp/w" {
		t.Fatalf("WorkDir = %q, want /tmp/w", c.WorkDir)
	}
	if c.Timeout != 90*time.Second {
		t.Fatalf("Timeout = %v, want 90s", c.Timeout)
	}
}

func TestExplicitRootMustContainBinary(t *testing.T) {
	dir := t.TempDir() // no bin/qucs-s inside
	t.Setenv("QUCS_S_ROOT", dir)
	c := Load()
	if c.QucsRoot != "" {
		t.Fatalf("QucsRoot = %q, want empty for an unusable QUCS_S_ROOT", c.QucsRoot)
	}
	if len(c.Searched) == 0 {
		t.Fatal("Searched must report where it looked")
	}
}

func TestLocalQucsDirPreferred(t *testing.T) {
	dir := t.TempDir()
	fakeInstall(t, filepath.Join(dir, "qucs"))
	t.Setenv("QUCS_S_ROOT", "")
	t.Chdir(dir)
	c := Load()
	if c.QucsRoot != filepath.Join(dir, "qucs") {
		t.Fatalf("QucsRoot = %q, want local dir", c.QucsRoot)
	}
}

func TestQucsExeJoin(t *testing.T) {
	c := Config{QucsRoot: `C:\q`}
	if got, want := c.QucsExe(), filepath.Join(`C:\q`, "bin", binaryName()); got != want {
		t.Fatalf("QucsExe() = %q, want %q", got, want)
	}
	if (&Config{}).QucsExe() != "" {
		t.Fatal("QucsExe() must be empty without a root")
	}
}

func TestDefaultWorkDirUsesDocuments(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := defaultWorkDir(); got != filepath.Join(home, "Documents", "qucs-mcp") {
		t.Fatalf("defaultWorkDir() = %q", got)
	}
}
