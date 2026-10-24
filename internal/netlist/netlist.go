// Package netlist parses ngspice netlists produced by
// `qucs-s -n --ngspice` into a device→pin→net map used for validation.
package netlist

import (
	"bufio"
	"strings"
)

// Parse maps every device line to its nodes. Keys are lowercased netlist
// refs (qucs-s prefixes refs lacking the device letter, e.g. "calR"
// becomes "RcalR"); values pin name → net, pin names follow the Qucs
// netlist order calibrated in components.PinsOf: two-pin devices "1","2";
// BJT "C","B","E"; MOSFET "D","G","S" (bulk/substrate nodes are merged by
// qucs-s and ignored here).
func Parse(text string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inControl := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		if strings.HasPrefix(line, ".control") {
			inControl = true
			continue
		}
		if strings.HasPrefix(line, ".endc") {
			inControl = false
			continue
		}
		if inControl || strings.HasPrefix(line, ".") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := strings.ToLower(fields[0])
		names, known := nodeCount(ref[0])
		if !known || strings.ContainsAny(ref, "/") {
			continue
		}
		n := len(names)
		if len(fields) < n+1 {
			n = len(fields) - 1
		}
		pins := map[string]string{}
		for i := 0; i < n; i++ {
			pins[names[i]] = fields[i+1]
		}
		out[ref] = pins
	}
	return out, nil
}

// nodeCount maps the SPICE device letter to Qucs pin names and count.
func nodeCount(letter byte) ([]string, bool) {
	switch letter {
	case 'v', 'i', 'r', 'c', 'l', 'd':
		return []string{"1", "2"}, true
	case 'q':
		return []string{"C", "B", "E"}, true
	case 'm':
		return []string{"D", "G", "S"}, true
	}
	return nil, false
}

// NetlistRef reproduces qucs-s's ref normalization: refs not starting
// with the device letter get the letter prefixed.
func NetlistRef(ref, spiceLetter string) string {
	if spiceLetter == "" {
		return ref
	}
	if strings.HasPrefix(strings.ToLower(ref), strings.ToLower(spiceLetter)) &&
		len(ref) > len(spiceLetter) {
		return ref
	}
	return spiceLetter + ref
}
