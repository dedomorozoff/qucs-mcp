package validate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/layout"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/netlist"
	"github.com/dedomorozoff/qucs-mcp/internal/qtest"
)

// TestRCIntegration runs the pipeline against the installed Qucs-S:
// SPICE text → model → layout → .sch → netlist → compare. It builds the
// model by hand rather than through internal/spice, so a parser
// regression cannot hide a validation regression. Skipped when Qucs-S is
// not installed.
func TestRCIntegration(t *testing.T) {
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
	if err := doc.WriteTo(filepath.Join(dir, "rclow.sch")); err != nil {
		t.Fatal(err)
	}

	cir := qtest.Netlist(t, dir, "rclow")

	// netlist row sanity checks
	for _, want := range []string{"R1 in out", "C1 out 0"} {
		found := false
		for _, line := range strings.Split(cir, "\n") {
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

	actual, err := netlist.Parse(cir)
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
