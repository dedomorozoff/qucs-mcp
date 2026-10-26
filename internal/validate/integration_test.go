package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/layout"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/netlist"
)

// TestRCIntegration runs the full pipeline against the installed
// Qucs-S: SPICE text → model → layout → .sch → netlist → compare.
// Skipped when qucs-s.exe is not available.
func TestRCIntegration(t *testing.T) {
	exe := qucsExe()
	if exe == "" {
		t.Skip("qucs-s.exe not installed")
	}

	spec := &model.CircuitSpec{
		Name: "rclow",
		Components: []model.Component{
			{Ref: "V1", Type: "S4Q_V", Value: "DC 0 AC 1", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "0"},
			}},
			{Ref: "R1", Type: "R", Value: "1k", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "out"},
			}},
			{Ref: "C1", Type: "C", Value: "100n", Pins: []model.Pin{
				{Name: "1", Net: "out"}, {Name: "2", Net: "0"},
			}},
		},
		Analyses: []model.Analysis{
			{Type: "ac", Params: map[string]string{"sweep": "dec", "points": "91", "start": "10", "stop": "1M"}},
		},
	}

	doc, err := layout.Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	schPath := filepath.Join(dir, "rclow.sch")
	if err := doc.WriteTo(schPath); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(exe, "-n", "--ngspice", "-i", "rclow.sch", "-o", "rclow.cir")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("qucs-s -n failed: %v\n%s", err, out)
	}
	cirPath := filepath.Join(dir, "rclow.cir")
	if _, statErr := os.Stat(cirPath); statErr != nil {
		t.Fatalf("netlist not produced: %s", schPath)
	}

	// netlist row sanity checks
	cir, _ := os.ReadFile(cirPath)
	for _, want := range []string{"R1 in out", "C1 out 0"} {
		found := false
		for _, line := range strings.Split(string(cir), "\n") {
			trimmed := strings.Fields(strings.TrimSpace(line))
			if len(trimmed) >= 4 && trimmed[0] == strings.Fields(want)[0] &&
				trimmed[1] == strings.Fields(want)[1] && trimmed[2] == strings.Fields(want)[2] {
				found = true
			}
		}
		if !found {
			t.Errorf("netlist missing row %q:\n%s", want, cir)
		}
	}

	actual, err := netlist.Parse(string(cir))
	if err != nil {
		t.Fatal(err)
	}
	rep := Compare(spec, actual)
	if !rep.Valid {
		t.Fatalf("validation failed: %+v\nnetlist:\n%s", rep, cir)
	}
	if len(rep.Pins) != 6 {
		t.Fatalf("pins checked = %d, want 6", len(rep.Pins))
	}
}

func qucsExe() string {
	if env := os.Getenv("QUCS_S_ROOT"); env != "" {
		return filepath.Join(env, "bin", "qucs-s.exe")
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	p := filepath.Join(dir, "qucs", "bin", "qucs-s.exe")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
