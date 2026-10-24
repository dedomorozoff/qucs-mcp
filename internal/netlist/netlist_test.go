package netlist

import (
	"strings"
	"testing"
)

const sampleNetlist = `* Qucs 26.1.1  C:/tmp/x.sch
V1 in  0 DC 0 AC 1
R1 in out  1K tc1=0.0 tc2=0.0
C1 out 0  100N
D1 out 0
QcalQ c b e QMOD AREA=26.85 TEMP=0.0
McalM d g s s MMOD L=1UM W=1UM
.control
ac dec 91 10 1m
write spice4qucs.ac1.plot v(in) v(out)
destroy all
reset
exit
.endc
.END
`

func TestParseDevicesAndNodes(t *testing.T) {
	m, err := Parse(sampleNetlist)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ref  string
		pin  string
		want string
	}{
		{"v1", "1", "in"},
		{"v1", "2", "0"},
		{"r1", "1", "in"},
		{"r1", "2", "out"},
		{"c1", "1", "out"},
		{"c1", "2", "0"},
		{"qcalq", "C", "c"},
		{"qcalq", "B", "b"},
		{"qcalq", "E", "e"},
		{"mcalm", "D", "d"},
		{"mcalm", "G", "g"},
		{"mcalm", "S", "s"},
	}
	for _, c := range cases {
		got, ok := m[c.ref][c.pin]
		if !ok {
			t.Errorf("%s[%s] missing (have %v)", c.ref, c.pin, m[c.ref])
			continue
		}
		if got != c.want {
			t.Errorf("%s[%s] = %q, want %q", c.ref, c.pin, got, c.want)
		}
	}
	if _, ok := m["ac"]; ok {
		t.Error(".control content must not leak as a device")
	}
}

func TestParseDanglingDevice(t *testing.T) {
	m, err := Parse("R1 a b 1k\n")
	if err != nil {
		t.Fatal(err)
	}
	if m["r1"]["1"] != "a" || m["r1"]["2"] != "b" {
		t.Fatalf("r1 = %v", m["r1"])
	}
	if !strings.EqualFold("", "") {
		t.FailNow()
	}
}

func TestNetlistRef(t *testing.T) {
	// qucs-s prefixes the ref with the device letter unless it is present.
	cases := [][3]string{
		{"R1", "R", "R1"},
		{"calR", "R", "RcalR"},
		{"V1", "V", "V1"},
		{"load", "R", "Rload"},
	}
	for _, c := range cases {
		if got := NetlistRef(c[0], c[1]); got != c[2] {
			t.Errorf("NetlistRef(%q,%q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
