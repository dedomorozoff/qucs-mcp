package validate

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/netlist"
)

func TestCompareHealthyRCLowPass(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{
			{Ref: "V1", Type: "S4Q_V", Pins: []model.Pin{{Name: "1", Net: "in"}, {Name: "2", Net: "0"}}},
			{Ref: "R1", Type: "R", Pins: []model.Pin{{Name: "1", Net: "in"}, {Name: "2", Net: "out"}}},
			{Ref: "C1", Type: "C", Pins: []model.Pin{{Name: "1", Net: "out"}, {Name: "2", Net: "0"}}},
		},
	}
	nl := `
V1 in  0 DC 0 AC 1
R1 in out  1K tc1=0.0 tc2=0.0
C1 out 0  100N
`
	actual, err := netlist.Parse(nl)
	if err != nil {
		t.Fatal(err)
	}
	rep := Compare(spec, actual)
	if !rep.Valid || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Pins) != 6 {
		t.Fatalf("pins checked = %d, want 6 (%+v)", len(rep.Pins), rep.Pins)
	}
}

func TestCompareDetectsFloatAndShort(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{
			{Ref: "R1", Type: "R", Pins: []model.Pin{{Name: "1", Net: "in"}, {Name: "2", Net: "out"}}},
			{Ref: "R2", Type: "R", Pins: []model.Pin{{Name: "1", Net: "out"}, {Name: "2", Net: "0"}}},
		},
	}
	// R1 pin1 floats (_net0), and in implicitly merged with nothing;
	// R2 pin1 got net "in" instead of "out" (label mix-up).
	nl := `
R1 _net0 out  1K tc1=0.0
R2 in 0  1K tc1=0.0
`
	actual, _ := netlist.Parse(nl)
	rep := Compare(spec, actual)
	if rep.Valid {
		t.Fatalf("must be invalid: %+v", rep)
	}
	var hasFloat, hasMismatch bool
	for _, e := range rep.Errors {
		if strings.Contains(e, `_net0`) {
			hasFloat = true
		}
		if strings.Contains(e, `expected net "out"`) {
			hasMismatch = true
		}
	}
	if !hasFloat || !hasMismatch {
		t.Fatalf("errors = %v", rep.Errors)
	}
}

func TestCompareDetectsShort(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{
			{Ref: "R1", Type: "R", Pins: []model.Pin{{Name: "1", Net: "a"}, {Name: "2", Net: "b"}}},
			{Ref: "R2", Type: "R", Pins: []model.Pin{{Name: "1", Net: "a"}, {Name: "2", Net: "0"}}},
		},
	}
	// a and b merged by a bad label on one wire
	nl := `
R1 a a  1K tc1=0.0
R2 a 0  1K tc1=0.0
`
	actual, _ := netlist.Parse(nl)
	rep := Compare(spec, actual)
	if rep.Valid {
		t.Fatalf("shorted net must be invalid: %+v", rep)
	}
	if len(rep.Errors) < 1 || !strings.Contains(rep.Errors[len(rep.Errors)-1], "merged into one node") {
		t.Fatalf("no short diagnostic: %v", rep.Errors)
	}
}

func TestCompareMissingDevice(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{
			{Ref: "R1", Type: "R", Pins: []model.Pin{{Name: "1", Net: "a"}, {Name: "2", Net: "b"}}},
			{Ref: "M1", Type: "_MOSFET", Pins: []model.Pin{{Name: "D", Net: "d"}, {Name: "G", Net: "g"}, {Name: "S", Net: "0"}}},
		},
	}
	actual, _ := netlist.Parse("R1 a b 1k\n")
	rep := Compare(spec, actual)
	if rep.Valid {
		t.Fatal("missing MOSFET must invalidate")
	}
	if len(rep.Errors) < 1 || !strings.Contains(rep.Errors[0], "M1") {
		t.Fatalf("errors = %v", rep.Errors)
	}
	// only R1's pins were actually checked (M1 bailed early)
	if len(rep.Pins) != 2 {
		t.Fatalf("pins = %+v", rep.Pins)
	}
}
