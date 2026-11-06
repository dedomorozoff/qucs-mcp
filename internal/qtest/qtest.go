// Package qtest locates an installed Qucs-S for the integration tests and
// wraps the headless netlist/simulation invocations they share.
//
// Tests must call Binary and skip when it returns "", so the suite stays
// green on machines and CI runners without Qucs-S.
package qtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dedomorozoff/qucs-mcp/internal/config"
)

// timeout bounds every qucs-s invocation in tests.
const timeout = 120 * time.Second

// Binary returns the qucs-s binary path, or "" when Qucs-S is not
// installed (QUCS_S_ROOT, a ./qucs directory in the tree, PATH, system
// locations — see internal/config).
func Binary() string {
	exe := config.Load().QucsExe()
	if exe == "" {
		return ""
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		return ""
	}
	return exe
}

// Netlist runs `qucs-s -n --ngspice` on name.sch in dir and returns the
// generated .cir text.
func Netlist(t *testing.T, dir, name string) string {
	t.Helper()
	return run(t, dir, name, ".cir", false)
}

// Simulate runs `qucs-s -n --ngspice --run` on name.sch in dir and
// returns the generated dataset text.
func Simulate(t *testing.T, dir, name string) string {
	t.Helper()
	return run(t, dir, name, ".dat", true)
}

func run(t *testing.T, dir, name, ext string, sim bool) string {
	t.Helper()
	exe := Binary()
	if exe == "" {
		t.Skip("Qucs-S not installed")
	}
	waitForQucs(t)
	args := []string{"-n", "--ngspice"}
	if sim {
		args = append(args, "--run")
	}
	args = append(args, "-i", name+".sch", "-o", name+ext)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("qucs-s %v: %v\noutput:\n%s", args, err, out)
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, name+ext))
	if rerr != nil {
		t.Fatalf("no %s produced: %v\noutput:\n%s", ext, rerr, out)
	}
	return string(raw)
}

// Exclusive holds the qucs-s lock for the whole test. A test that drives
// Qucs-S through the server's own runner instead of Netlist/Simulate needs
// it: the runner serializes inside its process, but go test still runs
// packages side by side.
func Exclusive(t *testing.T) {
	t.Helper()
	if err := holdQucs(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseQucs)
}

// waitForQucs serializes qucs-s across test *processes*. go test runs
// packages concurrently, and two overlapping headless invocations pick up
// each other's intermediate netlist: a schematic ends up with another
// circuit's node names in its own dataset (measured on Qucs-S 26.1.1).
// internal/runner already serializes within one process; this covers the
// rest of the suite. The lock is re-entrant per process, because one test
// generates a netlist and then simulates the same schematic.
func waitForQucs(t *testing.T) {
	t.Helper()
	if err := holdQucs(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseQucs)
}

const (
	// queueTimeout bounds how long a test waits for another process.
	queueTimeout = 10 * time.Minute
	// staleLock is how old an orphaned lock file must be to be taken
	// over: a killed test run must not wedge the whole suite.
	staleLock = 10 * time.Minute
)

var (
	qucsMu    sync.Mutex
	qucsHolds int
)

func lockPath() string { return filepath.Join(os.TempDir(), "qucs-mcp-qtest.lock") }

func holdQucs() error {
	qucsMu.Lock()
	defer qucsMu.Unlock()
	if qucsHolds > 0 {
		qucsHolds++
		return nil
	}
	path := lockPath()
	deadline := time.Now().Add(queueTimeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "pid %d\n", os.Getpid())
			_ = f.Close()
			qucsHolds = 1
			return nil
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > staleLock {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("another qucs-s test run has held %s for over %v", path, queueTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func releaseQucs() {
	qucsMu.Lock()
	defer qucsMu.Unlock()
	qucsHolds--
	if qucsHolds == 0 {
		_ = os.Remove(lockPath())
	}
}
