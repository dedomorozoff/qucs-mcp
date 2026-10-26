package server_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioSmoke builds the real binary and talks to it over stdio with
// an MCP client, verifying the doctor tool answers end to end. This test
// requires the Go toolchain to build the binary at test time.
func TestStdioSmoke(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not in PATH")
	}
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "qucs-mcp.exe")
	if runtime.GOOS != "windows" {
		exe = filepath.Join(tmp, "qucs-mcp")
	}
	cmd := exec.Command("go", "build", "-o", exe, "github.com/dedomorozoff/qucs-mcp/cmd/qucs-mcp")
	cmd.Dir = moduleRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	proc := exec.Command(exe, "-q")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "smoke", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &mcp.CommandTransport{Command: proc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "doctor"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		// doctor fails when qucs-s.exe is absent; the smoke test only
		// checks protocol connectivity.
		t.Logf("doctor reported error (Qucs-S likely missing): %v", res.Content)
	}

	// list_components must answer over stdio too.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_components",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("list_components failed over stdio: %v", res.Content)
	}
	n := 0
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if text := tc.Text; n == 0 && !strings.Contains(text, "S4Q_V") {
				t.Fatalf("list_components payload misses catalog types: %.200s", text)
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("list_components returned no text content")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}
