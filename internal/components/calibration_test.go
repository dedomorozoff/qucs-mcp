package components

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lau/qucs-mcp/internal/sch"
)

// Calibration against the installed Qucs-S (integration test).
//
// Builds one schematic containing every catalog device with probe wires
// whose endpoints sit exactly on the calibrated pin points, generates a
// netlist with `qucs-s -n --ngspice`, and asserts the pin→net mapping.
// Empirically (see ledger), a wire connects to a pin only when the wire
// endpoint coincides with the pin point; passing through the pin is not
// sufficient.
//
// Skipped when qucs-s.exe is not installed.

type calStub struct {
	X1, Y1 int // far end (label lives near here)
	X2, Y2 int // pin end — must coincide with the pin point
}

func calProbe(dev sch.ComponentLine, p Pin) calStub {
	cx, cy := dev.X, dev.Y
	dx, dy := p.DX*2, p.DY*2
	return calStub{
		X1: cx + dx, Y1: cy + dy,
		X2: cx + p.DX, Y2: cy + p.DY,
	}
}

func TestCalibrationAgainstQucsS(t *testing.T) {
	exe := qucsExePath()
	if exe == "" {
		t.Skip("qucs-s.exe not installed")
	}
	dir := t.TempDir()

	type devCase struct {
		ref    string
		line   sch.ComponentLine
		pins   []Pin
		expect []string
	}
	cases := []devCase{
		{"calV", S4QV("calV", 100, 100, "DC 1", false), PinsOf("S4Q_V", false), []string{"calv_1", "calv_2"}},
		{"calI", S4QI("calI", 100, 300, "DC 1", false), PinsOf("S4Q_I", false), []string{"cali_1", "cali_2"}},
		{"calVv", S4QV("calVv", 100, 500, "DC 2", true), PinsOf("S4Q_V", true), []string{"calvv_1", "calvv_2"}},
		{"calR", R("calR", 300, 100, "1k", false), PinsOf("R", false), []string{"calr_1", "calr_2"}},
		{"calC", C("calC", 300, 300, "100n", false), PinsOf("C", false), []string{"calc_1", "calc_2"}},
		{"calL", L("calL", 300, 500, "1m", false), PinsOf("L", false), []string{"call_1", "call_2"}},
		{"calD", Diode("calD", 500, 100, "", false), PinsOf("DIODE_SPICE", false), []string{"cald_1", "cald_2"}},
		{"calQ", BJT("calQ", 500, 300, false), PinsOf("_BJT", false), []string{"calq_C", "calq_B", "calq_E"}},
		{"calM", MOSFET("calM", 500, 500, false), PinsOf("_MOSFET", false), []string{"calm_D", "calm_G", "calm_S"}},
	}

	doc := sch.NewDocument("calibrate")
	for i := range cases {
		c := &cases[i]
		doc.AddComponent(c.line)
		for _, p := range c.pins {
			s := calProbe(c.line, p)
			doc.AddWire(sch.WireLine{
				X1: s.X1, Y1: s.Y1, X2: s.X2, Y2: s.Y2,
				Label: strings.ToLower(c.line.Ref) + "_" + p.Name,
				LX:    s.X1, LY: s.Y1 - 20, Rot: 0,
			})
		}
	}
	// ground anchor so generation sees a sane schematic
	doc.AddComponent(GND(900, 100))
	doc.AddWire(sch.WireLine{X1: 870, Y1: 100, X2: 900, Y2: 100, Label: "", LX: 880, LY: 90, Rot: 0})

	schPath := filepath.Join(dir, "calibrate.sch")
	if err := doc.WriteTo(schPath); err != nil {
		t.Fatal(err)
	}

	cir := filepath.Join(dir, "calibrate.cir")
	cmd := exec.Command(exe, "-n", "--ngspice", "-i", "calibrate.sch", "-o", "calibrate.cir")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("qucs-s -n failed: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(cir); statErr != nil {
		t.Fatalf("netlist file not created at %s", cir)
	}

	f, err := os.Open(cir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "*") || strings.HasPrefix(line, ".") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 1 {
			lines[strings.ToLower(fields[0])] = fields[1:]
		}
	}
	for _, c := range cases {
		netRef := strings.ToLower(c.ref)
		if letter := SpiceLetter(c.line.Type); letter != "" && !strings.HasPrefix(netRef, strings.ToLower(letter)) {
			netRef = strings.ToLower(letter) + netRef
		}
		nodes, ok := lines[netRef]
		if !ok {
			t.Errorf("%s: device missing from netlist (looked for %s)", c.ref, netRef)
			continue
		}
		t.Logf("%s -> %v (net ref %q)", c.ref, nodes, netRef)
		if len(nodes) < len(c.expect) {
			t.Errorf("%s: netlist nodes %v shorter than expected %v", c.ref, nodes, c.expect)
			continue
		}
		for i, want := range c.expect {
			if got := nodes[i]; got != want {
				t.Errorf("%s: node[%d] = %q, want %q (all: %v)", c.ref, i, got, want, nodes)
			}
		}
	}
}

func qucsExePath() string {
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
