// Package conn rebuilds circuit connectivity from a parsed schematic
// (design §13): component pins, wire endpoints and net labels are
// joined with union-find; a net's name is its wire label, ground
// symbols bind to node "0", and unlabeled non-ground nets get stable
// generated names. The result feeds validate_schematic.
package conn

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dedomorozoff/qucs-mcp/internal/components"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

type point struct{ x, y int }

// Rebuild reconstructs the CircuitSpec from a schematic's components
// and wires. Components outside the catalog are skipped and reported
// as warnings.
func Rebuild(rd *sch.ReadDocument, name string) (*model.CircuitSpec, []string) {
	spec := &model.CircuitSpec{Name: name}
	var warnings []string

	rk := func(p point) int64 { return int64(p.x)<<32 + int64(p.y) }

	// ---- union-find over grid points
	parent := map[point]point{}
	var root func(p point) point
	root = func(p point) point {
		set, ok := parent[p]
		if !ok {
			parent[p] = p
			return p
		}
		if set == p {
			return p
		}
		set = root(set)
		parent[p] = set
		return set
	}
	union := func(a, b point) {
		ra, rb := root(a), root(b)
		if ra != rb {
			if rk(ra) > rk(rb) {
				ra, rb = rb, ra
			}
			parent[rb] = ra
		}
	}

	// ---- ground symbol positions
	gndPos := map[point]bool{}
	for _, c := range rd.Components {
		if c.Type == "GND" {
			gndPos[point{c.X, c.Y}] = true
		}
	}

	// ---- wires: connect their two endpoints; collect labels
	labels := map[int64]string{} // root key -> name chosen for the group
	pickLabel := func(p point, label string) {
		k := rk(root(p))
		if old, ok := labels[k]; !ok || label < old {
			labels[k] = label
		}
	}
	for _, w := range rd.Wires {
		union(point{w.X1, w.Y1}, point{w.X2, w.Y2})
	}
	for _, w := range rd.Wires {
		if w.Label != "" {
			pickLabel(point{w.X1, w.Y1}, w.Label)
		}
	}

	// ---- components: catalog types with rebuilt pin points
	pinPoint := map[string]map[string]point{}
	var order []model.Component
	dupRefs := map[string]int{}
	for _, c := range rd.Components {
		if c.Ref != "*" { // GND placeholders are all "*" by convention
			dupRefs[c.Ref]++
		}
		if !components.Supported(c.Type) || c.Type == "GND" {
			if c.Type != "GND" && !directive(c.Type) {
				warnings = append(warnings, fmt.Sprintf(
					"component %s has unsupported type %s (skipped)", c.Ref, c.Type))
			}
			continue
		}
		names, pts := pinsAt(c)
		m := map[string]point{}
		for i, pn := range names {
			m[pn] = pts[i]
		}
		pinPoint[c.Ref] = m
		order = append(order, model.Component{
			Ref:   c.Ref,
			Type:  c.Type,
			Value: firstQuoted(c.Body),
		})
	}
	for ref, n := range dupRefs {
		if n > 1 {
			warnings = append(warnings, fmt.Sprintf("ref %s appears %d times in the schematic", ref, n))
		}
	}

	// ---- join pins to the wires/grounds/other pins at their points
	for _, c := range order {
		for _, p := range pinPoint[c.Ref] {
			if gndPos[p] {
				union(p, p) // ensure root exists
				continue
			}
			root(p)
		}
	}

	// ---- net name resolution: ground roots → "0", label → label
	gndRoots := map[int64]bool{}
	for p := range gndPos {
		gndRoots[rk(root(p))] = true
	}
	for _, w := range rd.Wires {
		for _, p := range []point{{w.X1, w.Y1}, {w.X2, w.Y2}} {
			if gndPos[p] {
				gndRoots[rk(root(p))] = true
			}
		}
	}

	netNames := map[int64]string{}
	auto := 0
	for k := range parent {
		key := rk(root(k))
		if _, done := netNames[key]; done {
			continue
		}
		switch {
		case gndRoots[key]:
			netNames[key] = "0"
		case labels[key] != "":
			netNames[key] = labels[key]
		default:
			auto++
			netNames[key] = fmt.Sprintf("n%02d", auto)
		}
	}

	netName := func(p point) string {
		return netNames[rk(root(p))]
	}

	// ---- pin nets
	for i := range order {
		c := &order[i]
		var pins []model.Pin
		for _, pn := range pinNames(c.Type) {
			p, ok := pinPoint[c.Ref][pn]
			switch {
			case !ok:
				pins = append(pins, model.Pin{Name: pn, Net: ""})
			case gndPos[p]:
				pins = append(pins, model.Pin{Name: pn, Net: "0"})
			default:
				pins = append(pins, model.Pin{Name: pn, Net: netName(p)})
			}
		}
		c.Pins = pins
		spec.Components = append(spec.Components, *c)
	}
	sortComponentsByName(spec)
	return spec, warnings
}

// sortComponentsByName keeps output deterministic by ref then type.
func sortComponentsByName(spec *model.CircuitSpec) {
	sort.SliceStable(spec.Components, func(i, j int) bool {
		if spec.Components[i].Ref != spec.Components[j].Ref {
			return spec.Components[i].Ref < spec.Components[j].Ref
		}
		return spec.Components[i].Type < spec.Components[j].Type
	})
}

func directive(t string) bool {
	switch t {
	case ".AC", ".TR", ".DC", "SpicePar", "SpiceInclude", "SpiceModel":
		return true
	}
	return false
}

// pinsAt computes catalog pin offsets transformed by the component's
// stored Rot/Mirror. Layout only emits two orientation forms, both
// reconstructed here:
//   - passives/DIODE_SPICE with Rot=3: (dx,dy) rotated 3× ((x,y)->(y,-x))
//   - S4Q_V/S4Q_I with Mirror=1: pin1 (0,-30), pin2 (0,30)
func pinsAt(c sch.ComponentLine) (names []string, pts []point) {
	base := components.PinsOf(c.Type, false)
	if len(base) == 0 {
		return nil, nil
	}
	if (c.Type == "S4Q_V" || c.Type == "S4Q_I") && c.Mirror != 0 {
		base = components.PinsOf(c.Type, true)
	}
	for _, p := range base {
		dx, dy := p.DX, p.DY
		if !(c.Type == "S4Q_V" || c.Type == "S4Q_I") {
			for k := 0; k < mod(c.Rot, 4); k++ {
				dx, dy = dy, -dx
			}
			if c.Mirror != 0 {
				dx = -dx
			}
		}
		names = append(names, p.Name)
		pts = append(pts, point{c.X + dx, c.Y + dy})
	}
	return names, pts
}

func mod(a, b int) int {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

func pinNames(catalogType string) []string {
	var out []string
	for _, p := range components.PinsOf(catalogType, false) {
		out = append(out, p.Name)
	}
	return out
}

// firstQuoted extracts the first "…" string from a body.
func firstQuoted(body string) string {
	i := strings.IndexByte(body, '"')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(body[i+1:], '"')
	if j < 0 {
		return ""
	}
	return body[i+1 : i+1+j]
}
