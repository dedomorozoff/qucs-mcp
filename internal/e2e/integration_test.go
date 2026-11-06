package e2e

import (
	"math/cmplx"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/components"
	"github.com/dedomorozoff/qucs-mcp/internal/conn"
	"github.com/dedomorozoff/qucs-mcp/internal/dataset"
	"github.com/dedomorozoff/qucs-mcp/internal/layout"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/netlist"
	"github.com/dedomorozoff/qucs-mcp/internal/qtest"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
	"github.com/dedomorozoff/qucs-mcp/internal/spice"
	"github.com/dedomorozoff/qucs-mcp/internal/validate"
)

// cases are the netlist texts the pipeline is checked against. Every
// entry is small enough to read on its own and each isolates one of the
// behaviours listed above.
var cases = []struct{ name, text string }{
	{"dc_only", `V1 n1 0 DC 5
R1 n1 n2 1k
R2 n2 0 2k
.end`},
	{"diode_card", `V1 a 0 DC 1
D1 a b Dmod
R1 b 0 1k
.model Dmod d is=1e-14
.op
.end`},
	{"diode_missing_model", `V1 a 0 DC 1
D1 a b NOPE
R1 b 0 1k
.op
.end`},
	{"bjt_card", `V1 in 0 DC 0.7 AC 1
Rb in b 10k
Q1 c b e QM
Re e 0 100
Rc vcc c 1k
V2 vcc 0 DC 5
.model QM npn bf=150 is=1e-15 vaf=25
.ac dec 10 10 1MEG
.end`},
	{"mos_geom", `V1 g 0 DC 5
M1 d g s 0 Mmod L=1u W=10u
Rd vdd d 10k
V2 vdd 0 DC 5
.model Mmod nmos vto=0.7 kp=110u
.op
.end`},
	{"mos_pmos", `V1 g 0 DC 0
M1 d g s vdd Mp
V2 vdd 0 DC 5
.model Mp pfet vto=-0.7 kp=50u
.op
.end`},
	{"bjt_level3", `V1 in 0 DC 0.7
Q1 c b e QM AREA=2
Re e 0 100
Rc vcc c 1k
V2 vcc 0 DC 5
.model QM npn (bf=120 is=1e-16 theta=.015 c2=3e-10)
.op
.end`},
	{"gnd_net", `V1 in gnd DC 1
R1 in out 1k
R2 out GND 2k
.op
.end`},
	{"dollar_net", `V1 in 0 DC 1
R1 out1 $n$1 1k
R2 $n$1 0 2k
V2 in out1 DC 0
.op
.end`},
	{"case_fold", `V1 A 0 DC 1
R1 A B 1k
R2 a B 2k
.op
.end`},
	{"dc_sweep", `V1 n1 0 DC 0
R1 n1 n2 1k
R2 n2 0 2k
.dc V1 0 5 0.5
.end`},
	{"comment_semicolon", `V1 in 0 DC 1 ; drive
R1 in out 1k ; series
C1 out 0 100n
.op
.end`},
}

// text returns the netlist source of a case.
func text(t *testing.T, name string) string {
	t.Helper()
	for _, c := range cases {
		if c.name == name {
			return c.text
		}
	}
	t.Fatalf("no case %q", name)
	return ""
}

// circuit is one schematic built by the pipeline, plus the files it
// generates in a temporary directory.
type circuit struct {
	name string
	dir  string
	spec *model.CircuitSpec
	doc  *sch.Document
}

// build runs text → model → layout → .sch. It needs no Qucs-S, so it
// also proves the headless half of the pipeline on a bare runner.
func build(t *testing.T, name string) *circuit {
	t.Helper()
	spec, err := spice.Parse(text(t, name))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	spec.Name = name
	doc, err := layout.Build(spec)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	c := &circuit{name: name, dir: t.TempDir(), spec: spec, doc: doc}
	if err := doc.WriteTo(c.schPath()); err != nil {
		t.Fatalf("write .sch: %v", err)
	}
	return c
}

func (c *circuit) schPath() string { return filepath.Join(c.dir, c.name+".sch") }

// netlist generates the .cir with Qucs-S and checks that Qucs-S resolved
// exactly the connections the parsed model describes: the coherence the
// user sees as "the schematic is not whole" is a mismatch here.
func (c *circuit) netlist(t *testing.T) string {
	t.Helper()
	cir := qtest.Netlist(t, c.dir, c.name)
	parsed, err := netlist.Parse(cir)
	if err != nil {
		t.Fatalf("parse netlist: %v", err)
	}
	if rep := validate.Compare(c.spec, parsed); !rep.Valid {
		t.Errorf("netlist does not match the model: %v\n cir:\n%s", rep.Errors, cir)
	}
	return cir
}

// simulate runs the schematic and returns the parsed dataset.
func (c *circuit) simulate(t *testing.T) *dataset.Dataset {
	t.Helper()
	d, err := dataset.Parse(qtest.Simulate(t, c.dir, c.name))
	if err != nil {
		t.Fatalf("parse dataset: %v", err)
	}
	return d
}

// requireText fails when the netlist is missing any of the fragments.
// Whitespace runs are collapsed on both sides: ngspice aligns node
// columns, so "R1 in out  1K" and "R1 in out 1K" are the same row.
func requireText(t *testing.T, hay string, want ...string) {
	t.Helper()
	squash := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	lower := squash(hay)
	for _, w := range want {
		if !strings.Contains(lower, squash(w)) {
			t.Errorf("netlist is missing %q:\n%s", w, hay)
		}
	}
}

// statements renders the netlist without its header comment, which
// quotes the .sch path and would otherwise match net names by accident.
func statements(cir string) string {
	var b strings.Builder
	for _, line := range strings.Split(cir, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "*") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// hasStatement reports whether the netlist contains the whole statement
// on a line of its own; ngspice keywords are too short to search for as
// substrings.
func hasStatement(t *testing.T, cir, want string) bool {
	t.Helper()
	for _, line := range strings.Split(cir, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), want) {
			return true
		}
	}
	return false
}

// value returns the first sample of a dataset variable. ngspice reports
// names in lower case whatever case the netlist was written with.
func value(t *testing.T, d *dataset.Dataset, name string) complex128 {
	t.Helper()
	want := strings.ToLower(name)
	for _, s := range d.Vars {
		if len(s.Values) > 0 && strings.ToLower(s.Name) == want {
			return s.Values[0]
		}
	}
	t.Fatalf("dataset has no %q; variables: %v", name, d.VarNames())
	return 0
}

func wantFloat(t *testing.T, got complex128, want float64) {
	t.Helper()
	if abs := cmplx.Abs(got - complex(want, 0)); abs > 1e-4 {
		t.Errorf("value = %v, want %v", got, want)
	}
}

// remark returns the first schematic remark containing the fragment.
func (c *circuit) remark(t *testing.T, fragment string) string {
	t.Helper()
	for _, r := range c.doc.Remarks {
		if strings.Contains(strings.ToLower(r), strings.ToLower(fragment)) {
			return r
		}
	}
	t.Fatalf("no remark containing %q; remarks: %v", fragment, c.doc.Remarks)
	return ""
}

func warnContaining(spec *model.CircuitSpec, fragment string) string {
	for _, w := range spec.Warnings {
		if strings.Contains(strings.ToLower(w), strings.ToLower(fragment)) {
			return w
		}
	}
	return ""
}

// TestDefaultOperatingPoint: a netlist without any analysis directive
// used to produce a schematic that simulated to an empty dataset.
func TestDefaultOperatingPoint(t *testing.T) {
	c := build(t, "dc_only")
	if len(c.spec.Analyses) != 0 {
		t.Fatalf("analyses = %v, want none", c.spec.Analyses)
	}
	cir := c.netlist(t)
	if !hasStatement(t, cir, "op") {
		t.Errorf("netlist runs no operating point, the schematic would simulate empty:\n%s", cir)
	}

	d := c.simulate(t)
	if !d.HasResults() {
		t.Fatalf("simulation produced no results; variables: %v", d.VarNames())
	}
	wantFloat(t, value(t, d, "v(n2)"), 10.0/3.0) // 5 V across a 1k/2k divider
}

// TestModelCardKeepsDirectiveDot: Qucs-S writes the card verbatim into
// the netlist, so a card that lost its leading dot silently disabled
// every diode on the schematic.
func TestModelCardKeepsDirectiveDot(t *testing.T) {
	c := build(t, "diode_card")
	requireText(t, c.netlist(t), ".model Dmod d is=1e-14", "D1 a b Dmod")

	d := c.simulate(t)
	if !d.HasResults() {
		t.Fatal("diode simulation produced no results")
	}
	v := value(t, d, "v(b)")
	if real(v) < 0.3 || real(v) > 0.5 {
		t.Errorf("diode node v(b) = %v, want the forward drop near 0.37", v)
	}
}

// TestBJTCardBecomesDeviceModel: in spice mode Qucs-S builds transistor
// models per device from the component properties, not from the named
// card, so card parameters have to be carried into the device.
func TestBJTCardBecomesDeviceModel(t *testing.T) {
	c := build(t, "bjt_card")
	cir := c.netlist(t)
	requireText(t, cir, ".MODEL QMOD_Q1 npn", "Bf=150", "Is=1E-15", "Vaf=25")
	// Qucs-S writes one per-device model and keeps the source card.
	if n := strings.Count(strings.ToLower(statements(cir)), ".model"); n != 2 {
		t.Errorf("expected the device model plus the original card, got %d .model lines:\n%s", n, cir)
	}

	d := c.simulate(t)
	if !d.HasResults() {
		t.Fatal("amplifier simulation produced no results")
	}
	// A common-emitter stage with beta 150 inverts and gains: at 10 Hz
	// the collector must be out of phase with the base drive.
	if real(value(t, d, "ac.v(c)")) > 0 {
		t.Errorf("collector did not invert the input: %v", value(t, d, "ac.v(c)"))
	}
	if gain := cmplx.Abs(value(t, d, "ac.v(c)")) / cmplx.Abs(value(t, d, "ac.v(in)")); gain < 1 {
		t.Errorf("low-frequency gain = %v, want amplification", gain)
	}
}

// TestMOSGeometryParams: model parameters (kp) and instance parameters
// (l, w) both have to reach the device, in the units Qucs-S expects.
func TestMOSGeometryParams(t *testing.T) {
	c := build(t, "mos_geom")
	cir := c.netlist(t)
	requireText(t, cir, "Kp=0.00011", "L=1E-06", "W=1E-05", ".MODEL MMOD_M1 NMOS", "Vt0=0.7")
}

// TestPMOSFlavorFromCard: the channel type decides the pin order, and a
// p-channel card recognised as n-channel silently inverts the circuit.
func TestPMOSFlavorFromCard(t *testing.T) {
	c := build(t, "mos_pmos")
	cir := c.netlist(t)
	requireText(t, cir, ".MODEL MMOD_M1 PMOS", "Vt0=-0.7")
	if w := warnContaining(c.spec, "bulk"); w == "" {
		t.Errorf("expected a note about the merged bulk net; warnings: %v", c.spec.Warnings)
	}
}

// TestUnmappedParameterIsReported: a level-3 parameter with no Qucs-S
// property slot must be announced, not dropped quietly.
func TestUnmappedParameterIsReported(t *testing.T) {
	c := build(t, "bjt_level3")
	cir := c.netlist(t)
	requireText(t, cir, "AREA=2", "Bf=120", "Is=1E-16")
	c.remark(t, "THETA")
	c.remark(t, "C2")
}

// TestGNDNetIsNodeZero: Qucs-S resolves a net named gnd to node 0 but
// keeps printing v(gnd), which kills the simulation.
func TestGNDNetIsNodeZero(t *testing.T) {
	c := build(t, "gnd_net")
	cir := c.netlist(t)
	requireText(t, cir, "R2 out 0")
	if strings.Contains(strings.ToLower(statements(cir)), "gnd") {
		t.Errorf("netlist still refers to a gnd net:\n%s", cir)
	}
	wantFloat(t, value(t, c.simulate(t), "v(out)"), 2.0/3.0)
}

// TestIllegalNetNameIsRenamed: characters outside [A-Za-z0-9_] survive
// the netlist but vanish from the dataset, so such a net has to be
// renamed and reported.
func TestIllegalNetNameIsRenamed(t *testing.T) {
	c := build(t, "dollar_net")
	if w := warnContaining(c.spec, "renamed"); w == "" {
		t.Fatalf("the renamed net was not reported; warnings: %v", c.spec.Warnings)
	}
	cir := c.netlist(t)
	if strings.Contains(statements(cir), "$") {
		t.Errorf("netlist kept an illegal node name:\n%s", cir)
	}
	requireText(t, cir, "_n_1")
	d := c.simulate(t)
	if !d.HasResults() {
		t.Fatal("simulation of the renamed net produced no results")
	}
	wantFloat(t, value(t, d, "v(_n_1)"), 2.0/3.0)
}

// TestNodeNamesAreCaseInsensitive: SPICE treats A and a as one node,
// while Qucs labels keep the case they were written with.
func TestNodeNamesAreCaseInsensitive(t *testing.T) {
	c := build(t, "case_fold")
	cir := c.netlist(t)
	// Both resistors sit between the same two nodes: the source spelled
	// the nets two ways must produce one pair of labels.
	parsed, err := netlist.Parse(cir)
	if err != nil {
		t.Fatalf("parse netlist: %v", err)
	}
	r1, r2 := parsed["r1"], parsed["r2"]
	if len(r1) != 2 || len(r2) != 2 {
		t.Fatalf("resistor nodes missing from the netlist: %v %v", r1, r2)
	}
	if r1["1"] != r2["1"] || r1["2"] != r2["2"] {
		t.Errorf("case-folded nets stayed separate: R1 %v, R2 %v", r1, r2)
	}
	if r1["1"] == r1["2"] {
		t.Errorf("the two folded nets collapsed into one: %v", r1)
	}
	wantFloat(t, value(t, c.simulate(t), "v(B)"), 1.0)
}

// TestDCSweepIsAnnounced: spice mode has no sweep variables, so a .dc
// source becomes a single operating point and must say so.
func TestDCSweepIsAnnounced(t *testing.T) {
	c := build(t, "dc_sweep")
	c.netlist(t)
	c.remark(t, "not supported")
}

// TestInlineCommentIgnored: a trailing ";" comment belongs to the line
// it sits on and must not leak into a value or a node name.
func TestInlineCommentIgnored(t *testing.T) {
	c := build(t, "comment_semicolon")
	cir := c.netlist(t)
	if strings.Contains(cir, ";") {
		t.Errorf("comment text reached the netlist:\n%s", cir)
	}
	requireText(t, cir, "R1 in out 1K", "C1 out 0 100N")
}

// TestUndefinedModelFailsLoudly: ngspice aborts on a missing card, exits
// zero and leaves an empty dataset behind.
func TestUndefinedModelFailsLoudly(t *testing.T) {
	c := build(t, "diode_missing_model")
	if w := warnContaining(c.spec, "no model card"); w == "" {
		t.Errorf("the undefined model was not reported; warnings: %v", c.spec.Warnings)
	}
	c.netlist(t)
	if d := c.simulate(t); d.HasResults() {
		t.Errorf("a simulation without a model card reported success: %v", d.VarNames())
	}
}

// TestSchematicRoundTrip: the .sch a build produced has to read back
// into the same net partition, without Qucs-S involved. Geometry carries
// no connectivity here, so a mismatch means a lost label.
func TestSchematicRoundTrip(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := build(t, tc.name)
			rd, err := sch.Read(c.schPath())
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			back, warns := conn.Rebuild(rd, tc.name)
			if rep := validate.Compare(back, partitions(c.spec)); !rep.Valid {
				t.Errorf("round trip changed the connectivity: %v (warnings %v)", rep.Errors, warns)
			}
		})
	}
}

// partitions renders a model as the netlist-shaped map validate.Compare
// expects, so the rebuilt schematic can be checked against it directly.
func partitions(spec *model.CircuitSpec) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, c := range spec.Components {
		letter := components.SpiceLetter(c.Type)
		if letter == "" {
			continue // directive block: no device line in the netlist
		}
		nodes := map[string]string{}
		for _, p := range c.Pins {
			nodes[p.Name] = p.Net
		}
		out[strings.ToLower(netlist.NetlistRef(c.Ref, letter))] = nodes
	}
	return out
}
