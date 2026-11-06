package runner

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shellCmd returns an executable and arguments that run the one-liner on
// the current platform, so the subprocess plumbing is exercised on both
// the Windows and the POSIX CI runners.
func shellCmd(script string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", script}
	}
	return "sh", []string{"-c", script}
}

func TestRunMissingExe(t *testing.T) {
	r := Runner{QucsExe: filepath.Join(t.TempDir(), "missing.exe"), Timeout: time.Second}
	_, err := r.Run(context.Background(), []string{"--version"}, t.TempDir())
	if err == nil {
		t.Fatal("want error for missing exe")
	}
}

func TestRunCapturesOutput(t *testing.T) {
	exe, args := shellCmd("echo hello")
	r := Runner{QucsExe: exe, Timeout: 10 * time.Second}
	res, err := r.Run(context.Background(), args, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestRunNonZeroExitIsNotAnError(t *testing.T) {
	exe, args := shellCmd("exit 3")
	r := Runner{QucsExe: exe, Timeout: 10 * time.Second}
	res, err := r.Run(context.Background(), args, t.TempDir())
	if err != nil {
		t.Fatalf("non-zero exit should be a Result, got error: %v", err)
	}
	if res.Exit != 3 {
		t.Fatalf("exit = %d, want 3", res.Exit)
	}
}

func TestRunTimeout(t *testing.T) {
	// Run the long command directly, not through a shell: killing the
	// shell leaves its child holding the output pipe open, and Run would
	// only return once that child finished.
	exe, args := "ping", []string{"-n", "10", "127.0.0.1"}
	if runtime.GOOS != "windows" {
		exe, args = "sleep", []string{"10"}
	}
	r := Runner{QucsExe: exe, Timeout: 1 * time.Second}
	start := time.Now()
	_, err := r.Run(context.Background(), args, t.TempDir())
	if err == nil {
		t.Fatal("want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the timed-out run took %v", elapsed)
	}
}

func TestRunContextCancellation(t *testing.T) {
	// Cancel via ctx even when the process would finish quickly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exe, args := shellCmd("echo hi")
	r := Runner{QucsExe: exe, Timeout: 30 * time.Second}
	_, err := r.Run(ctx, args, t.TempDir())
	if err == nil {
		t.Fatal("want cancellation error")
	}
}

// TestRunQueueIsCancellable: a caller waiting behind another run gives up
// when its own context is done, instead of blocking the server.
func TestRunQueueIsCancellable(t *testing.T) {
	qucsS.Lock()
	defer qucsS.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	exe, args := shellCmd("echo queued")
	r := Runner{QucsExe: exe, Timeout: 30 * time.Second}
	start := time.Now()
	_, err := r.Run(ctx, args, t.TempDir())
	if err == nil {
		t.Fatal("want an error while the run lock is held")
	}
	if !strings.Contains(err.Error(), "another qucs-s run is still in progress") {
		t.Errorf("err = %v, want the queueing message", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %v for a cancelled queue slot", elapsed)
	}
}

// TestRunsSerialize: qucs-s runs never overlap — concurrent headless
// invocations have been measured writing one circuit's node names into
// the other's dataset.
func TestRunsSerialize(t *testing.T) {
	dir := t.TempDir()
	exe, args := shellCmd("echo queued")

	qucsS.Lock()
	done := make(chan string, 1)
	go func() {
		r := Runner{QucsExe: exe, Timeout: 30 * time.Second}
		res, err := r.Run(context.Background(), args, dir)
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- res.Stdout
	}()

	select {
	case out := <-done:
		qucsS.Unlock()
		t.Fatalf("a run started while another one held qucs-s: %q", out)
	case <-time.After(200 * time.Millisecond):
	}

	qucsS.Unlock()
	select {
	case out := <-done:
		if !strings.Contains(out, "queued") {
			t.Fatalf("stdout = %q", out)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the queued run never started once the lock was released")
	}
}

func TestNetlistAndSimulateArgs(t *testing.T) {
	r := Runner{QucsExe: "qucs-s.exe", Timeout: time.Second}
	got := r.NetlistArgs("in.sch", "out.cir")
	want := []string{"-n", "--ngspice", "-i", "in.sch", "-o", "out.cir"}
	if len(got) != len(want) {
		t.Fatalf("netlist args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("netlist args = %v, want %v", got, want)
		}
	}
	got = r.SimulateArgs("in.sch", "out.dat")
	want = []string{"-n", "--ngspice", "--run", "-i", "in.sch", "-o", "out.dat"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("simulate args = %v, want %v", got, want)
		}
	}
}
