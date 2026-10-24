package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds runtime configuration resolved from environment variables.
type Config struct {
	// QucsRoot is the Qucs-S installation directory
	// (contains bin\qucs-s.exe).
	QucsRoot string
	// WorkDir is where schematics, netlists and datasets are written.
	WorkDir string
	// Timeout is the subprocess timeout for Qucs-S invocations.
	Timeout time.Duration
	// ServerName/ServerVersion identify the MCP server.
	ServerName    string
	ServerVersion string
}

// Default constants match the documented install layout; the one for this
// machine is used only as a last-resort fallback.
const (
	defaultFallbackRoot = `C:\Users\lau\Desktop\Soft\Qucs-S-25.1.1-win64`
	defaultWorkDir      = "."
	defaultTimeoutSec   = 120
)

// Load builds a Config from the environment, preferring, in order:
// QUCS_S_ROOT, a local ./qucs installation (relative to the current
// directory), then the documented default path.
func Load() Config {
	c := Config{
		ServerName:    "qucs-mcp",
		ServerVersion: "0.1.0",
		WorkDir:       os.Getenv("QUCS_MCP_WORKDIR"),
	}
	if c.WorkDir == "" {
		c.WorkDir = defaultWorkDir
	}
	c.QucsRoot = os.Getenv("QUCS_S_ROOT")
	if c.QucsRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			local := filepath.Join(wd, "qucs")
			if fileExists(filepath.Join(local, "bin", "qucs-s.exe")) {
				c.QucsRoot = local
			}
		}
	}
	if c.QucsRoot == "" {
		c.QucsRoot = defaultFallbackRoot
	}
	secs := defaultTimeoutSec
	if s := os.Getenv("QUCS_MCP_TIMEOUT"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			secs = v
		}
	}
	c.Timeout = time.Duration(secs) * time.Second
	return c
}

// QucsExe returns the full path of the qucs-s.exe binary.
func (c Config) QucsExe() string {
	return filepath.Join(c.QucsRoot, "bin", "qucs-s.exe")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
