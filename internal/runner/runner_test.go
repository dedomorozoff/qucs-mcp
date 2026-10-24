package runner

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunMissingExe(t *testing.T) {
	r := Runner{QucsExe: filepath.Join(t.TempDir(), "missing.exe"), Timeout: time.Second}
	_, err := r.Run(context.Background(), []string{"--version"}, t.TempDir())
	if err == nil {
		t.Fatal("want error for missing exe")
	}
}

func TestRunCapturesOutput(t *testing.T) {
	// cmd is always present on Windows; use it to verify plumbing.
	r := Runner{QucsExe: "cmd", Timeout: 10 * time.Second}
	res, err := r.Run(context.Background(), []string{"/c", "echo", "hello"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestRunNonZeroExitIsNotAnError(t *testing.T) {
	r := Runner{QucsExe: "cmd", Timeout: 10 * time.Second}
	res, err := r.Run(context.Background(), []string{"/c", "exit", "3"}, t.TempDir())
	if err != nil {
		t.Fatalf("non-zero exit should be a Result, got error: %v", err)
	}
	if res.Exit != 3 {
		t.Fatalf("exit = %d, want 3", res.Exit)
	}
}

func TestRunTimeout(t *testing.T) {
	r := Runner{QucsExe: "ping", Timeout: 1 * time.Second}
	_, err := r.Run(context.Background(), []string{"-n", "10", "127.0.0.1"}, t.TempDir())
	if err == nil {
		t.Fatal("want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestRunContextCancellation(t *testing.T) {
	// Cancel via ctx even when the process would finish quickly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Runner{QucsExe: "cmd", Timeout: 30 * time.Second}
	_, err := r.Run(ctx, []string{"/c", "ver"}, t.TempDir())
	if err == nil {
		t.Fatal("want cancellation error")
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
