package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Runner invokes the headless qucs-s.exe subprocess.
type Runner struct {
	// QucsExe is the full path to bin\qucs-s.exe.
	QucsExe string
	// Timeout bounds every subprocess run.
	Timeout time.Duration
}

// Result captures a completed subprocess invocation.
type Result struct {
	// CmdLine is the reconstructed command line (for logs and errors).
	CmdLine string
	// Exe and Args are the exact pieces passed to the OS.
	Exe  string
	Args []string
	// Dir is the working directory the command ran in.
	Dir string
	// Stdout/Stderr captured with in-memory buffers.
	Stdout string
	Stderr string
	// Exit is the process exit code (-1 if the process could not start
	// or was killed).
	Exit int
}

// Errorf builds an error annotated with the command line and captured
// output, so failures from qucs-s are always explainable.
func (r *Result) Errorf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w\ncommand: %s\nstdout:\n%s\nstderr:\n%s",
		errors.New(msg), r.CmdLine, r.Stdout, r.Stderr)
}

// Run executes QucsExe with args in dir. A non-zero exit code is not an
// error: callers interpret the captured output. Exceeding the timeout is
// a context.DeadlineExceeded error.
func (r Runner) Run(ctx context.Context, args []string, dir string) (*Result, error) {
	if r.Timeout <= 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, r.QucsExe, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	res := &Result{
		Exe:     r.QucsExe,
		Args:    args,
		Dir:     dir,
		CmdLine: quote(r.QucsExe) + " " + quoteArgs(args),
		Exit:    -1,
	}

	err := cmd.Run()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	if err == nil {
		if code, gerr := exitCode(cmd); gerr == nil {
			res.Exit = code
		}
		return res, nil
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("%w: qucs-s did not finish in %v", ctx.Err(), r.Timeout)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.Exit = ee.ExitCode()
		return res, nil
	}
	return res, fmt.Errorf("could not run %s: %w (check that Qucs-S is installed at %s)", r.QucsExe, err, filepath.Dir(r.QucsExe))
}

// NetlistArgs returns the command that forwards a schematic to ngspice and
// writes a SPICE netlist: qucs-s -n --ngspice -i <sch> -o <cir>.
func (r Runner) NetlistArgs(sch, cir string) []string {
	return []string{"-n", "--ngspice", "-i", sch, "-o", cir}
}

// SimulateArgs returns the command that runs the simulation and writes a
// Qucs dataset: qucs-s -n --ngspice --run -i <sch> -o <dat>.
func (r Runner) SimulateArgs(sch, dat string) []string {
	return []string{"-n", "--ngspice", "--run", "-i", sch, "-o", dat}
}

// Netlist runs the netlist generation for sch writing cir.
func (r Runner) Netlist(ctx context.Context, sch, cir string) (*Result, error) {
	dir := filepath.Dir(sch)
	return r.Run(ctx, r.NetlistArgs(filepath.Base(sch), filepath.Base(cir)), dir)
}

// Simulate runs the simulation for sch writing dat.
func (r Runner) Simulate(ctx context.Context, sch, dat string) (*Result, error) {
	dir := filepath.Dir(sch)
	return r.Run(ctx, r.SimulateArgs(filepath.Base(sch), filepath.Base(dat)), dir)
}

func exitCode(cmd *exec.Cmd) (int, error) {
	state := cmd.ProcessState
	if state == nil {
		return -1, os.ErrInvalid
	}
	return state.ExitCode(), nil
}

func quote(p string) string {
	if p == "" || !containsAny(p, ` "`+"\t") {
		return p
	}
	return `"` + p + `"`
}

func quoteArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += quote(a)
	}
	return out
}

func containsAny(s, chars string) bool {
	for _, c := range s {
		for _, x := range chars {
			if c == x {
				return true
			}
		}
	}
	return false
}
