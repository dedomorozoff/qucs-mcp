// Package model defines the parsed circuit representation shared by the
// SPICE parser, the layout generator and the validator.
package model

import "sort"

// Pin ties a named device pin to a net.
type Pin struct {
	Name string // "1", "2", "A", "C", "B", "E", "D", "G", "S", "gnd"
	Net  string
}

// Component is one parsed device.
type Component struct {
	Ref   string // "R1"
	Type  string // catalog type: "R", "S4Q_V", "_BJT", ...
	Value string // "1k" (normalized) or raw SPICE source spec
	Pins  []Pin  // ordered like the Qucs netlist expects
	Props map[string]string
}

// NetOf returns the net of the named pin or "".
func (c Component) NetOf(pinName string) string {
	for _, p := range c.Pins {
		if p.Name == pinName {
			return p.Net
		}
	}
	return ""
}

// Analysis is one parsed simulation directive.
type Analysis struct {
	Type   string // "ac", "dc", "tran", "op"
	Params map[string]string
}

// CircuitSpec is the parsed, layout-agnostic circuit description.
type CircuitSpec struct {
	Name       string
	Components []Component
	Analyses   []Analysis
	Models     []string // raw .model lines (kept verbatim)
	Params     map[string]string
	Includes   []string
	Warnings   []string
}

// Find returns the component with the given ref.
func (c *CircuitSpec) Find(ref string) (Component, bool) {
	for _, e := range c.Components {
		if e.Ref == ref {
			return e, true
		}
	}
	return Component{}, false
}

// Nets returns all distinct net names used by device pins.
func (c *CircuitSpec) Nets() []string {
	set := map[string]struct{}{}
	for _, e := range c.Components {
		for _, p := range e.Pins {
			set[p.Net] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		if n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// HasAnalysis reports whether an analysis of the given type is present.
func (c *CircuitSpec) HasAnalysis(kind string) bool {
	for _, a := range c.Analyses {
		if a.Type == kind {
			return true
		}
	}
	return false
}
