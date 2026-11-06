package dataset

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/layout"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/qtest"
)

// TestRCSimulateIntegration generates RC low-pass, simulates it
// headlessly and parses the resulting Qucs dataset. Skipped without
// Qucs-S.
func TestRCSimulateIntegration(t *testing.T) {
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
	dat := qtest.Simulate(t, dir, "rclow")
	d, err := Parse(dat)
	if err != nil {
		t.Fatalf("parse dataset: %v", err)
	}

	indep, ok := d.Var("frequency")
	if !ok || !indep.Indep {
		t.Fatalf("frequency indep var missing: %+v", d.Vars)
	}
	vout, ok := d.Var("ac.v(out)")
	if !ok {
		t.Fatalf("ac.v(out) missing; vars = %v", d.VarNames())
	}
	if _, ok := d.Var("ac.v(in)"); !ok {
		t.Fatalf("ac.v(in) missing; vars = %v", d.VarNames())
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
