package server

import (
	"context"
	"testing"
	"time"

	"github.com/dedomorozoff/qucs-mcp/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestServer(t *testing.T) (*mcp.ClientSession, *Server) {
	t.Helper()
	cfg := config.Config{
		QucsRoot:      t.TempDir(),
		WorkDir:       t.TempDir(),
		Timeout:       time.Second,
		ServerName:    "qucs-mcp-test",
		ServerVersion: "0.0.0",
	}
	srv := NewServer(cfg)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := srv.MCP().Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	sess, err := c.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close(); cancel() })
	if err := sess.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	_ = sess
	return sess, srv
}

func TestDoctorToolLists(t *testing.T) {
	sess, _ := newTestServer(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"doctor":           true,
		"create_schematic": true, "update_schematic": true,
		"validate_schematic": true, "simulate_schematic": true,
		"read_schematic": true, "list_components": true,
		"describe_component": true, "open_schematic": true,
	}
	if len(res.Tools) != len(want) {
		t.Fatalf("tools = %d, want %d", len(res.Tools), len(want))
	}
	for _, tl := range res.Tools {
		if !want[tl.Name] {
			t.Fatalf("unexpected tool %q", tl.Name)
		}
	}
}

func TestDoctorMissingExeFails(t *testing.T) {
	sess, _ := newTestServer(t)
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "doctor"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("doctor must fail when qucs-s.exe is missing; result=%+v", res)
	}
}
