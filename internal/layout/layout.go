// Package layout turns a parsed model.CircuitSpec into a schematic
// document. Connectivity is carried entirely by wire net labels, so the
// geometric arrangement never changes the electrical result (design §9).
//
// Calibrated facts baked into this package:
//   - a wire connects to a pin only when a wire ENDPOINT sits exactly on
//     the pin point (passing through is not enough);
//   - identical labels on physically separate wires merge into one net;
//   - unlabeled wires into a GND symbol create SPICE node "0";
//   - .AC mapping: Qucs "log" Points = round(points_per_decade * decades)+1,
//     Qucs "lin" Points = total point count;
//   - .TR mapping: step is recomputed by spice4qucs as
//     (stop-start)/(Points-1), so Points = round((stop-start)/step)+1.
package layout

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/lau/qucs-mcp/internal/components"
	"github.com/lau/qucs-mcp/internal/model"
	"github.com/lau/qucs-mcp/internal/sch"
)

const (
	gridStep  = 10
	pinStubs  = 30 // stub length beyond the pin point
	gndOffset = 30
)

// Row Y positions per device group; all multiples of 10.
const (
	ySources = 100
	yPassive = 220
	ySemi    = 360
	yAnalys  = 560
	yDirect  = 660
	yModels  = 740
	yInclude = 820
)

// Build renders spec into a schematic document.
func Build(spec *model.CircuitSpec) (*sch.Document, error) {
	doc := sch.NewDocument(spec.Name)

	cursorTwo := 0
	cursorSemi := 0

	for i := range spec.Components {
		c := &spec.Components[i]
		var line sch.ComponentLine
		switch c.Type {
		case "S4Q_V":
			cursorTwo++
			line = components.S4QV(c.Ref, xAt(cursorTwo), ySources, c.Value, false)
		case "S4Q_I":
			cursorTwo++
			line = components.S4QI(c.Ref, xAt(cursorTwo), ySources, c.Value, false)
		case "R", "C", "L", "DIODE_SPICE":
			cursorTwo++
			x, y := xAt(cursorTwo), yPassive
			switch c.Type {
			case "R":
				line = components.R(c.Ref, x, y, c.Value, false)
			case "C":
				line = components.C(c.Ref, x, y, c.Value, false)
			case "L":
				line = components.L(c.Ref, x, y, c.Value, false)
			case "DIODE_SPICE":
				line = components.Diode(c.Ref, x, y, c.Value, false)
			}
		case "_BJT", "_MOSFET":
			cursorSemi++
			x, y := xAt(cursorSemi), ySemi
			if c.Type == "_BJT" {
				line = components.BJT(c.Ref, x, y, c.Props["flavor"] == "pnp")
			} else {
				line = components.MOSFET(c.Ref, x, y, c.Props["flavor"] == "pmos")
			}
		case "SpicePar", "SpiceInclude", "SpiceModel", "GND":
			continue // emitted separately below
		default:
			return nil, fmt.Errorf("unsupported component type %q (ref %s)", c.Type, c.Ref)
		}
		doc.AddComponent(line)
		addPinStubs(doc, c, line.X, line.Y)
	}

	if err := addAnalyses(doc, spec); err != nil {
		return nil, err
	}
	if err := addDirectives(doc, spec); err != nil {
		return nil, err
	}
	return doc, nil
}

func xAt(n int) int { return 100 + 160*n }

// addPinStubs wires every pin of a device placed at (x, y): non-ground
// nets get a labeled stub, ground pins get an unlabeled wire into a GND
// symbol (which produces SPICE node "0").
func addPinStubs(doc *sch.Document, c *model.Component, x, y int) {
	for _, p := range components.PinsOf(c.Type, false) {
		net := c.NetOf(p.Name)
		px, py := x+p.DX, y+p.DY
		if net == "0" {
			var gx, gy int
			switch {
			case p.DY > 0: // bottom pin: ground below
				gx, gy = px, py+gndOffset
			case p.DY < 0: // top pin: ground above
				gx, gy = px, py-gndOffset
			default: // side pin: ground below
				gx, gy = px, py+gndOffset
			}
			doc.AddWire(sch.WireLine{X1: px, Y1: py, X2: gx, Y2: gy, LX: gx - 10, LY: gy - 10})
			doc.AddComponent(components.GND(gx, gy))
			continue
		}
		sx, sy := 0, 0
		if p.DX != 0 {
			sx = sign(p.DX)
		}
		if p.DY != 0 {
			sy = sign(p.DY)
		}
		tx, ty := px+sx*pinStubs, py+sy*pinStubs
		doc.AddWire(sch.WireLine{
			X1: px, Y1: py, X2: tx, Y2: ty,
			Label: net, LX: tx + 5, LY: ty - 10,
		})
	}
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// addAnalyses maps parsed analyses to Qucs simulation blocks. All
// analysis blocks share one vertical stack below the circuit.
func addAnalyses(doc *sch.Document, spec *model.CircuitSpec) error {
	stack := 0
	nextY := func() int {
		stack++
		return yAnalys + 80*(stack-1)
	}
	for _, a := range spec.Analyses {
		switch a.Type {
		case "op", "dc":
			doc.AddComponent(components.DCOrOp(fmt.Sprintf("DC%d", stack+1), 40, nextY()))
			if a.Type == "dc" && a.Params["src"] != "" {
				// spice-mode .DC runs as .OP; sweep parameters cannot be
				// represented in this MVP block.
				doc.SetRemark(fmt.Sprintf(".dc sweep of %s is not carried into the schematic (MVP: plain OP)", a.Params["src"]))
			}
		case "ac":
			sweep := strings.ToLower(a.Params["sweep"])
			pts, err := acPoints(sweep, a.Params["points"], a.Params["start"], a.Params["stop"])
			if err != nil {
				return err
			}
			qt := "lin"
			if sweep == "dec" || sweep == "oct" {
				qt = "log"
			}
			doc.AddComponent(components.AC(fmt.Sprintf("AC%d", stack+1), 40, nextY(),
				qt, a.Params["start"], a.Params["stop"], pts))
		case "tran":
			start := a.Params["tstart"]
			if start == "" {
				start = "0"
			}
			pts, err := tranPoints(start, a.Params["tstop"], a.Params["tstep"])
			if err != nil {
				return err
			}
			doc.AddComponent(components.TR(fmt.Sprintf("TR%d", stack+1), 40, nextY(),
				start, a.Params["tstop"], pts, "0"))
		}
	}
	return nil
}

// acPoints computes the Qucs .AC Points field. spice4qucs translates
// Qucs log sweeps to ngspice "dec" using points-per-decade =
// (Points-1)/decades, hence Points = N*decades+1 to reproduce SPICE
// ".ac dec N". "lin" passes the count through. SPICE "oct N" is
// approximated as N*log2(10) points per decade (~3.322*N).
func acPoints(sweep, points, start, stop string) (int, error) {
	n, err := strconv.Atoi(points)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf(".ac points %q must be a positive count", points)
	}
	switch sweep {
	case "lin":
		return n, nil
	case "dec":
		s0, ok0 := parseQucsNumber(start)
		s1, ok1 := parseQucsNumber(stop)
		if !ok0 || !ok1 {
			return 0, fmt.Errorf(".ac dec: cannot interpret start/stop %q / %q", start, stop)
		}
		if s0 <= 0 || s1 <= 0 || s1 <= s0 {
			return 0, fmt.Errorf(".ac dec needs 0 < start < stop, got %q..%q", start, stop)
		}
		decades := math.Log10(s1) - math.Log10(s0)
		return int(math.Round(float64(n)*decades)) + 1, nil
	case "oct":
		s0, ok0 := parseQucsNumber(start)
		s1, ok1 := parseQucsNumber(stop)
		if !ok0 || !ok1 {
			return 0, fmt.Errorf(".ac oct: cannot interpret start/stop %q / %q", start, stop)
		}
		if s0 <= 0 || s1 <= 0 || s1 <= s0 {
			return 0, fmt.Errorf(".ac oct needs 0 < start < stop, got %q..%q", start, stop)
		}
		decades := (math.Log10(s1) - math.Log10(s0)) / math.Log10(2)
		return int(math.Round(float64(n)*math.Log2(10)*decades)) + 1, nil
	}
	return 0, fmt.Errorf(".ac sweep %q not supported (lin/dec/oct)", sweep)
}

// tranPoints computes .TR Points so spice4qucs recovers the requested
// step: Points = round((stop-start)/step)+1.
func tranPoints(start, stop, step string) (int, error) {
	s0, ok0 := parseQucsNumber(start)
	s1, ok1 := parseQucsNumber(stop)
	d, okd := parseQucsNumber(step)
	if !ok0 || !ok1 || !okd {
		return 0, fmt.Errorf(".tran: cannot interpret start/stop/step %q %q %q", start, stop, step)
	}
	if d <= 0 || s1 <= s0 {
		return 0, fmt.Errorf(".tran needs stop > start and step > 0, got %v..%v step %q", s0, s1, step)
	}
	pts := int(math.Round((s1-s0)/d)) + 1
	if pts < 2 {
		pts = 2
	}
	return pts, nil
}

// parseQucsNumber parses Qucs-suffixed numbers: "10", "1M" (mega),
// "100u", "1e-6", "2.5E3". M/m are case sensitive per Qucs semantics;
// trailing letters beyond the multiplier are junk units and ignored.
func parseQucsNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	// exponent if present
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j && j > 0 {
			i = k
		}
	}
	numText := s[:i]
	num, err := strconv.ParseFloat(numText, 64)
	if err != nil {
		return 0, false
	}
	if i >= len(s) {
		return num, true
	}
	switch s[i] {
	case 'T', 't':
		num *= 1e12
	case 'G', 'g':
		num *= 1e9
	case 'M':
		num *= 1e6
	case 'k', 'K':
		num *= 1e3
	case 'm':
		num *= 1e-3
	case 'U', 'u':
		num *= 1e-6
	case 'N', 'n':
		num *= 1e-9
	case 'P', 'p':
		num *= 1e-12
	case 'F', 'f':
		num *= 1e-15
	}
	return num, true
}

// addDirectives renders .param/.include/.model blocks.
func addDirectives(doc *sch.Document, spec *model.CircuitSpec) error {
	n := 0
	for k, v := range spec.Params {
		n++
		doc.AddComponent(components.SpicePar(fmt.Sprintf("SpicePar%d", n), 300, yDirect+60*n,
			map[string]string{k: v}))
	}
	for i, model := range spec.Models {
		n++
		doc.AddComponent(components.SpiceModel(fmt.Sprintf("SpiceModel%d", n), 520, yModels+60*(i+1), model))
	}
	if len(spec.Includes) > 0 {
		doc.AddComponent(components.SpiceInclude("SpiceInclude1", 760, yInclude, spec.Includes...))
	}
	return nil
}

// HasAnalysisBlock reports whether the document contains a block of the
// given analysis type ("ac", "dc", "tran").
func HasAnalysisBlock(doc *sch.Document, kind string) (bool, error) {
	prefix := map[string]string{"ac": ".AC", "tran": ".TR", "dc": ".DC"}[kind]
	if prefix == "" {
		return false, fmt.Errorf("unknown analysis kind %q", kind)
	}
	for _, c := range doc.Components {
		if c.Type == prefix {
			return true, nil
		}
	}
	return false, nil
}
