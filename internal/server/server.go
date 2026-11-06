package server

import (
	"context"
	"fmt"
	"os"

	"github.com/dedomorozoff/qucs-mcp/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP server created from the loaded configuration.
type Server struct {
	mcp   *mcp.Server
	cfg   config.Config
	tools []*mcp.Tool
}

// NewServer creates an MCP server over the given config and registers the
// base `doctor` tool.
func NewServer(cfg config.Config) *Server {
	s := &Server{cfg: cfg}
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    cfg.ServerName,
		Version: cfg.ServerVersion,
	}, nil)
	s.registerDoctor()
	if err := s.registerTools(); err != nil {
		// registerTools currently cannot fail; fail fast if it ever can.
		panic(err)
	}
	return s
}

// Register registers every circuit tool (kept for API stability; the
// constructor already wires the tools).
func (s *Server) Register(ctx context.Context) error {
	return nil
}

// Tools returns the tool metadata currently registered on the server.
func (s *Server) Tools() []*mcp.Tool { return s.tools }

// MCP returns the underlying SDK server for Run.
func (s *Server) MCP() *mcp.Server { return s.mcp }

type doctorOut struct {
	QucsRoot  string   `json:"qucs_root" jsonschema:"Qucs-S installation root"`
	QucsExe   string   `json:"qucs_exe" jsonschema:"Path of the qucs-s binary"`
	QucsFound bool     `json:"qucs_found" jsonschema:"true if the qucs-s binary exists"`
	WorkDir   string   `json:"work_dir" jsonschema:"Directory for generated files"`
	TimeoutS  int      `json:"timeout_s" jsonschema:"Subprocess timeout in seconds"`
	Searched  []string `json:"searched,omitempty" jsonschema:"Locations probed for a Qucs-S installation"`
}

func (s *Server) registerDoctor() {
	s.tools = append(s.tools, &mcp.Tool{
		Name:        "doctor",
		Description: "Report health checks: Qucs-S installation, paths and configuration. Call this first to verify the setup.",
	})
	mcp.AddTool[struct{}, doctorOut](s.mcp, &mcp.Tool{
		Name:        "doctor",
		Description: "Report health checks: Qucs-S installation, paths and configuration. Call this first to verify the setup.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, doctorOut, error) {
		out := doctorOut{
			QucsRoot: s.cfg.QucsRoot,
			QucsExe:  s.cfg.QucsExe(),
			WorkDir:  s.cfg.WorkDir,
			TimeoutS: int(s.cfg.Timeout / 1e9),
			Searched: s.cfg.Searched,
		}
		if out.QucsExe == "" {
			return nil, out, fmt.Errorf(
				"no Qucs-S installation found; looked in: %s. Set QUCS_S_ROOT to the directory that contains bin/qucs-s.exe",
				joinList(s.cfg.Searched))
		}
		if _, err := os.Stat(out.QucsExe); err != nil {
			return nil, out, fmt.Errorf("qucs-s binary not found at %s: %w", out.QucsExe, err)
		}
		out.QucsFound = true
		if err := os.MkdirAll(s.cfg.WorkDir, 0o755); err != nil {
			return nil, out, fmt.Errorf("work dir %s unusable: %w", s.cfg.WorkDir, err)
		}
		return nil, out, nil
	})
}

func joinList(l []string) string {
	if len(l) == 0 {
		return "<nothing>"
	}
	out := l[0]
	for _, s := range l[1:] {
		out += ", " + s
	}
	return out
}

// Run serves the MCP protocol over stdio until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}
