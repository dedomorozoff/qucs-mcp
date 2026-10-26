package server

import (
	"context"
	"fmt"
	"os"

	"github.com/lau/qucs-mcp/internal/config"
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
	QucsRoot  string `json:"qucs_root" jsonschema:"Qucs-S installation root"`
	QucsExe   string `json:"qucs_exe" jsonschema:"Path of the qucs-s.exe binary"`
	QucsFound bool   `json:"qucs_found" jsonschema:"true if qucs-s.exe exists"`
	WorkDir   string `json:"work_dir" jsonschema:"Directory for generated files"`
	TimeoutS  int    `json:"timeout_s" jsonschema:"Subprocess timeout in seconds"`
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
		}
		if _, err := os.Stat(s.cfg.QucsExe()); err == nil {
			out.QucsFound = true
		} else {
			return nil, out, fmt.Errorf("qucs-s.exe not found at %s (set QUCS_S_ROOT): %w", s.cfg.QucsExe(), err)
		}
		if err := os.MkdirAll(s.cfg.WorkDir, 0o755); err != nil {
			return nil, out, fmt.Errorf("work dir %s unusable: %w", s.cfg.WorkDir, err)
		}
		return nil, out, nil
	})
}

// Run serves the MCP protocol over stdio until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}
