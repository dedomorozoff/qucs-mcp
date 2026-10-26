package dataset

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lau/qucs-mcp/internal/layout"
	"github.com/lau/qucs-mcp/internal/model"
)

// TestRCSimulateIntegration generates RC low-pass, simulates it
// headlessly and parses the resulting Qucs dataset. Requires qucs-s.exe.
func TestRCSimulateIntegration(t *testing.T) {
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
	cmd := exec.Command(exe, "-n", "--ngspice", "--run", "-i", "rclow.sch", "-o", "rclow.dat")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("simulate failed: %v\n%s", err, out)
	}
	datPath := filepath.Join(dir, "rclow.dat")
	raw, rerr := os.ReadFile(datPath)
	if rerr != nil {
		t.Fatalf("no dataset produced: %v\n%s", rerr, out)
	}
	d, err := Parse(string(raw))
	if err != nil {
		t.Fatalf("parse dataset: %v", err)
	}

	indep, ok := d.Var("frequency")
	if !ok || !indep.Indep {
		t.Fatalf("frequency indep var missing: %+v", d.Vars)
	}
	vout, ok := d.Var("ac.v(out)")
	if !ok {
		t.Fatalf("ac.v(out) missing; vars = %+v", names(d))
	}
	if _, ok := d.Var("ac.v(in)"); !ok {
		t.Fatalf("ac.v(in) missing; vars = %+v", names(d))
	}
	if len(vout.Values) != len(indep.Values) {
		t.Fatalf("vout %d points vs indep %d", len(vout.Values), len(indep.Values))
	}
	// dec 91 x 5 decades -> 91*5+1 = 456 sweep points through ngspice
	if len(indep.Values) != 456 {
		t.Fatalf("indep count = %d, want 456 (dec 91 over 5 decades)", len(indep.Values))
	}
	if last := indep.Values[len(indep.Values)-1]; real(last) != 1e6 {
		t.Fatalf("last freq = %v, want 1e6 (stop suffix must survive)", last)
	}
	for i, z := range vout.Values {
		if math.IsNaN(real(z)) || math.IsNaN(imag(z)) ||
			math.IsInf(real(z), 0) || math.IsInf(imag(z), 0) {
			t.Fatalf("vout[%d] not finite: %v", i, z)
		}
	}
	// transfer at the lowest frequency (10 Hz): |H| = 1/sqrt(1+(wRC)^2)
	// ~= 0.998 for f0=10, R=1k, C=100n.
	v0 := vout.Values[0]
	if real(v0) < 0.9 || real(v0) > 1.01 {
		t.Fatalf("v(out) at f0 = %v, want ~0.998", v0)
	}
}

func names(d *Dataset) []string {
	out := make([]string, 0, len(d.Vars))
	for _, s := range d.Vars {
		out = append(out, s.Name)
	}
	return out
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
	if _, statErr := os.Stat(p); statErr != nil {
		return ""
	}
	return p
}
