package components

import (
	"sort"
	"strconv"
	"strings"
)

// SPICE model cards carry their parameters inside the _BJT/_MOSFET
// component itself: qucs-s does not reference a named .model for these
// two types, it generates one per device (QMOD_<ref>, MMOD_<ref>) out of
// the property body. A .model line that is only written into the netlist
// therefore never reaches the transistor, and the device silently runs on
// built-in defaults. The tables below map SPICE parameter names to the
// body slot that holds them.
//
// Slot indices were measured against the installed Qucs-S by writing a
// sentinel value into every slot of BJT()/MOSFET() bodies and reading the
// .MODEL line back out of `qucs-s -n --ngspice` output. Slots that
// produce no netlist output at all are left unmapped (they are qucs
// internals or unsupported).

// bjtModelSlots maps an uppercase SPICE BJT model parameter to its slot.
var bjtModelSlots = map[string]int{
	"IS": 1, "NF": 2, "NR": 3, "IKF": 4, "IKR": 5, "VAF": 6, "VAR": 7,
	"ISE": 8, "NE": 9, "ISC": 10, "NC": 11, "BF": 12, "BR": 13,
	"RBM": 14, "IRB": 15, "RC": 16, "RE": 17, "RB": 18,
	"CJE": 19, "VJE": 20, "MJE": 21, "CJC": 22, "VJC": 23, "MJC": 24,
	"XCJC": 25, "CJS": 26, "VJS": 27, "MJS": 28, "FC": 29,
	"TF": 30, "XTF": 31, "VTF": 32, "ITF": 33, "TR": 34,
	"KF": 36, "AF": 37, "PTF": 42, "XTB": 43, "XTI": 44, "EG": 45, "TNOM": 46,
}

// bjtInstanceSlots maps device-line (instance) parameters.
var bjtInstanceSlots = map[string]int{"AREA": 47}

// mosModelSlots maps an uppercase SPICE level-1/2/3 MOSFET parameter.
var mosModelSlots = map[string]int{
	"VTO": 1, "KP": 2, "GAMMA": 3, "PHI": 4, "LAMBDA": 5, "RD": 6, "RS": 7,
	"IS": 9, "LD": 13, "TOX": 14, "CGSO": 15, "CGDO": 16, "CGBO": 17,
	"CBD": 18, "CBS": 19, "PB": 20, "MJ": 21, "FC": 22, "CJSW": 23,
	"MJSW": 24, "NSUB": 26, "NSS": 27, "TPG": 28, "UO": 29, "RSH": 30,
	"CJ": 33, "JS": 34, "KF": 39, "AF": 40,
}

// mosInstanceSlots maps MOSFET device-line geometry parameters. The body
// slots that qucs-s writes as L=/W=/Ad=/As=/Pd=/Ps= on the device line.
var mosInstanceSlots = map[string]int{
	"W": 11, "L": 12, "AD": 35, "AS": 36, "PD": 37, "PS": 38,
}

// ModelSlotTable returns the model-card and instance parameter slot maps
// for a catalog type (nil for types without one).
func ModelSlotTable(schType string) (modelSlots, instanceSlots map[string]int) {
	switch schType {
	case "_BJT":
		return bjtModelSlots, bjtInstanceSlots
	case "_MOSFET":
		return mosModelSlots, mosInstanceSlots
	}
	return nil, nil
}

// ApplyParams rewrites body property slots from SPICE parameters. params
// keys are uppercase SPICE names; values are literal body text. model and
// instance slots are consulted in that order, instance values win. The
// returned list names the parameters with no slot in the table.
func ApplyParams(body string, modelSlots, instanceSlots map[string]int, params map[string]string) (string, []string) {
	if len(params) == 0 {
		return body, nil
	}
	props := parseProps(body)
	var ignored []string
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		idx, ok := modelSlots[k]
		if !ok {
			idx, ok = instanceSlots[k]
		}
		if !ok || idx < 0 || idx >= len(props) {
			ignored = append(ignored, k+"="+params[k])
			continue
		}
		props[idx].value = params[k]
	}
	return renderProps(props), ignored
}

// propPair is one body property: a quoted value and the show-value flag.
type propPair struct {
	value string
	show  string
}

// parseProps scans `"value" flag` pairs; values may contain spaces.
func parseProps(body string) []propPair {
	var props []propPair
	for i := 0; i < len(body); {
		for i < len(body) && body[i] != '"' {
			i++
		}
		if i >= len(body) {
			break
		}
		i++ // past the opening quote
		start := i
		for i < len(body) && body[i] != '"' {
			i++
		}
		if i >= len(body) {
			break
		}
		p := propPair{value: body[start:i]}
		i++ // past the closing quote
		tail := body[i:]
		flag := "0"
		if fields := strings.Fields(tail); len(fields) > 0 {
			flag = fields[0]
		}
		p.show = flag
		props = append(props, p)
	}
	return props
}

func renderProps(props []propPair) string {
	var b strings.Builder
	for i, p := range props {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.Quote(p.value))
		b.WriteByte(' ')
		b.WriteString(p.show)
	}
	return b.String()
}
