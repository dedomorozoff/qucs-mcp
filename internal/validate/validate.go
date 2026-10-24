// Package validate compares generated netlists against the parsed
// circuit model (design §11).
package validate

import (
	"fmt"
	"strings"

	"github.com/lau/qucs-mcp/internal/components"
	"github.com/lau/qucs-mcp/internal/model"
	"github.com/lau/qucs-mcp/internal/netlist"
)

// PinResult is one checked pin.
type PinResult struct {
	Component string `json:"component"`
	Pin       string `json:"pin"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Ok        bool   `json:"ok"`
}

// Report is the validation outcome.
type Report struct {
	Valid    bool        `json:"valid"`
	Pins     []PinResult `json:"pins"`
	Errors   []string    `json:"errors"`
	Warnings []string    `json:"warnings"`
}

// Compare checks the actual netlist (device→pin→net map from
// netlist.Parse) against the expected model.
func Compare(spec *model.CircuitSpec, actual map[string]map[string]string) Report {
	rep := Report{Valid: true}
	merged := map[string]string{} // actual net -> expected net sets detection of shorts
	shorts := map[string]string{}

	for i := range spec.Components {
		c := spec.Components[i]
		pinNames := pinNames(c.Type)
		if len(pinNames) == 0 {
			continue // directive blocks have no pins
		}
		lref := strings.ToLower(netlist.NetlistRef(c.Ref, components.SpiceLetter(c.Type)))
		nodes, ok := actual[lref]
		if !ok {
			rep.Errors = append(rep.Errors,
				fmt.Sprintf("%s (%s): device missing from netlist (not recognized by Qucs-S)", c.Ref, c.Type))
			rep.Valid = false
			continue
		}
		for _, pn := range pinNames {
			want := ""
			for _, mp := range c.Pins {
				if mp.Name == pn {
					want = mp.Net
					break
				}
			}
			got, ok := nodes[pn]
			if !ok {
				rep.Errors = append(rep.Errors,
					fmt.Sprintf("%s: pin %s missing from netlist row", c.Ref, pn))
				rep.Valid = false
				continue
			}
			okPin := netsEqual(want, got)
			if !okPin {
				rep.Errors = append(rep.Errors,
					fmt.Sprintf("%s pin %s: expected net %q but the netlist has %q", c.Ref, pn, want, got))
				rep.Valid = false
			}
			// short/merge detection: two distinct expected nets sharing one actual net
			if got != "" {
				if prev, seen := merged[got]; seen && prev != want {
					key := got + ":" + prev + "+" + want
					if _, dup := shorts[key]; !dup {
						shorts[key] = ""
						rep.Errors = append(rep.Errors,
							fmt.Sprintf("nets %q and %q merged into one node %q (short circuit)", prev, want, got))
					}
				} else if !seen {
					merged[got] = want
				}
			}
			rep.Pins = append(rep.Pins, PinResult{
				Component: c.Ref, Pin: pn, Expected: want, Actual: got, Ok: okPin,
			})
		}
	}
	return rep
}

// pinNames returns the catalog pin names in netlist order ("" = no pins).
func pinNames(catalogType string) []string {
	pins := components.PinsOf(catalogType, false)
	if len(pins) == 0 {
		return nil
	}
	out := make([]string, len(pins))
	for i, p := range pins {
		out[i] = p.Name
	}
	return out
}

// netsEqual compares expected and actual nets. SPICE ground is "0".
func netsEqual(expected, actual string) bool {
	if expected == actual {
		return true
	}
	if expected == "0" && actual == "0" {
		return true
	}
	return false
}
