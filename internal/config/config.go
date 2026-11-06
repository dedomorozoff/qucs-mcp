// Package config resolves the runtime configuration from the
// environment and the local Qucs-S installation.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// binaryName is the Qucs-S executable for the running platform.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "qucs-s.exe"
	}
	return "qucs-s"
}

// Config holds runtime configuration resolved from environment variables.
type Config struct {
	// QucsRoot is the Qucs-S installation directory (contains
	// bin/qucs-s[.exe]); empty when no installation was found.
	QucsRoot string
	// WorkDir is where schematics, netlists and datasets are written.
	WorkDir string
	// Timeout is the subprocess timeout for Qucs-S invocations.
	Timeout time.Duration
	// ServerName/ServerVersion identify the MCP server.
	ServerName    string
	ServerVersion string
	// Searched lists the locations probed for a Qucs-S installation,
	// for actionable doctor output.
	Searched []string
}

const (
	defaultTimeoutSec = 120
)

// Build identity, stamped by the release workflow:
//
//	-ldflags "-X github.com/dedomorozoff/qucs-mcp/internal/config.version=v0.2.0
//	 -X github.com/dedomorozoff/qucs-mcp/internal/config.commit=<sha>"
//
// They are vars, not consts, precisely so the linker can replace them; a
// plain go build leaves the development values below.
var (
	version = "0.1.0"
	commit  = "dev"
)

// BuildInfo describes the running binary: version, commit, toolchain and
// target platform.
func BuildInfo() string {
	return fmt.Sprintf("%s (commit %s, %s, %s/%s)",
		version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Load builds a Config from the environment: QUCS_S_ROOT, a ./qucs
// installation next to the repository, PATH, then common system
// install directories.
func Load() Config {
	c := Config{
		ServerName:    "qucs-mcp",
		ServerVersion: version,
		WorkDir:       os.Getenv("QUCS_MCP_WORKDIR"),
	}
	if c.WorkDir == "" {
		c.WorkDir = defaultWorkDir()
	}
	c.QucsRoot, c.Searched = findQucs()
	secs := defaultTimeoutSec
	if s := os.Getenv("QUCS_MCP_TIMEOUT"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			secs = v
		}
	}
	c.Timeout = time.Duration(secs) * time.Second
	return c
}

// defaultWorkDir keeps generated files out of the service directory that
// launched the MCP server: ~/Documents/qucs-mcp, falling back to the
// per-user config directory and finally the current directory.
func defaultWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	documents := filepath.Join(home, "Documents")
	if st, serr := os.Stat(documents); serr == nil && st.IsDir() {
		return filepath.Join(documents, "qucs-mcp")
	}
	base := home
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		base = xdg
	} else if appdata := os.Getenv("APPDATA"); appdata != "" {
		base = appdata
	}
	return filepath.Join(base, ".config", "qucs-mcp")
}

// findQucs returns the first installation root that holds the qucs-s
// binary, plus the list of locations that were probed.
func findQucs() (root string, searched []string) {
	if env := os.Getenv("QUCS_S_ROOT"); env != "" {
		searched = append(searched, "$QUCS_S_ROOT="+env)
		if isInstall(filepath.Join(env, "bin")) {
			return env, searched
		}
		return "", searched
	}

	// A bundled ./qucs next to the working tree (walk up a few levels so
	// running from a subdirectory still finds it).
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for range 6 {
			local := filepath.Join(dir, "qucs")
			searched = append(searched, filepath.Join(local, "bin", binaryName()))
			if isInstall(filepath.Join(local, "bin")) {
				return local, searched
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// On PATH (Linux/macOS packages and Windows installs added to PATH).
	if p, err := exec.LookPath(binaryName()); err == nil {
		abs, aerr := filepath.Abs(p)
		if aerr == nil {
			bin := filepath.Dir(abs)
			root := filepath.Dir(bin)
			searched = append(searched, abs+" (PATH)")
			if isInstall(bin) {
				return root, searched
			}
		}
	}

	// Common system locations.
	for _, pattern := range systemPatterns() {
		match, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, dir := range match {
			searched = append(searched, filepath.Join(dir, "bin", binaryName()))
			if isInstall(filepath.Join(dir, "bin")) {
				return dir, searched
			}
		}
	}
	return "", searched
}

// isInstall reports whether bin holds the qucs-s binary. A sibling
// ngspice is not checked here: qucs-s locates it relative to itself.
func isInstall(bin string) bool {
	st, err := os.Stat(filepath.Join(bin, binaryName()))
	return err == nil && !st.IsDir()
}

// systemPatterns lists installation directory globs per platform.
func systemPatterns() []string {
	if runtime.GOOS == "windows" {
		var out []string
		for _, env := range []string{"LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
			base := os.Getenv(env)
			if base == "" && env == "ProgramFiles" {
				base = `C:\Program Files`
			}
			if base == "" {
				continue
			}
			out = append(out, filepath.Join(base, "Qucs-S*"), filepath.Join(base, "qucs-s*"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			out = append(out, filepath.Join(home, "Desktop", "Soft", "Qucs-S*"))
		}
		return out
	}
	// Linux/macOS: a prefix install (binary in <prefix>/bin) or the
	// system tree (binary in /usr/bin, root /usr).
	return []string{"/usr/local", "/opt/Qucs-S*", "/opt/qucs-s*", "/usr"}
}

// QucsExe returns the full path of the qucs-s binary, or "" when no
// installation was found.
func (c Config) QucsExe() string {
	if c.QucsRoot == "" {
		return ""
	}
	return filepath.Join(c.QucsRoot, "bin", binaryName())
}
