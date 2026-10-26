// Tool tests over the in-memory MCP transport: create→validate→simulate
// of the RC low-pass (skips without qucs-s), catalog tools and the
// overwrite guard (design §14/§16).
package server

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dedomorozoff/qucs-mcp/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// qucsExe reports whether the bundled Qucs-S install is available.
func qucsExe() string {
	if env := os.Getenv("QUCS_S_ROOT"); env != "" {
		p := filepath.Join(env, "bin", "qucs-s.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
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
	if _, statErr := os.Stat(p); statErr != nil {
		return ""
	}
	return p
}

const rcNetlist = `* RC low-pass
V1 in 0 DC 0 AC 1
R1 in out 1k
C1 out 0 100n
.ac dec 91 10 1MEG
.end
`

// call invokes a tool and decodes the JSON payload.
func call[In any, Out any](t *testing.T, sess *mcp.ClientSession, name string, in In) (Out, bool) {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: name, Arguments: in,
	})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var out Out
	if res.IsError {
		var text strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text.WriteString(tc.Text)
			}
		}
		t.Logf("tool %s error: %s", name, text.String())
		return out, false
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	if err := json.Unmarshal([]byte(text.String()), &out); err != nil {
		t.Fatalf("decode %s: %v\npayload: %s", name, err, text.String())
	}
	return out, true
}

func newWorkingServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	exe := qucsExe()
	if exe == "" {
		t.Skip("qucs-s.exe not installed")
	}
	cfg := config.Config{
		QucsRoot:      filepath.Dir(filepath.Dir(exe)),
		WorkDir:       t.TempDir(),
		Timeout:       90 * time.Second,
		ServerName:    "qucs-mcp-test",
		ServerVersion: "0.0.0",
	}
	srv := NewServer(cfg)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := srv.MCP().Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	sess, err := c.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close(); cancel() })
	return sess
}

type rcCreateOut struct {
	Path        string `json:"path"`
	NetlistPath string `json:"netlist_path"`
	Valid       bool   `json:"valid"`
	Report      *struct {
		Valid bool `json:"valid"`
	} `json:"report"`
	Warnings   []string `json:"warnings"`
	Simulation *struct {
		OK        bool `json:"ok"`
		Variables []struct {
			Name   string `json:"name"`
			Points int    `json:"points"`
		} `json:"variables"`
	} `json:"simulation"`
}

type rcPathOut struct {
	Path string `json:"path"`
}

func TestCreateValidateSimulateRCLowPass(t *testing.T) {
	sess := newWorkingServer(t)
	out, ok := call[map[string]any, rcCreateOut](t, sess, "create_schematic", map[string]any{
		"netlist":  rcNetlist,
		"name":     "rc_low",
		"simulate": true,
	})
	if !ok {
		t.Fatal("create_schematic returned an error")
	}
	if !out.Valid || out.Report == nil || !out.Report.Valid {
		t.Fatalf("validation failed: %+v", out.Report)
	}
	if out.Simulation == nil || !out.Simulation.OK {
		t.Fatalf("simulation missing/failed: %+v", out.Simulation)
	}
	found := map[string]int{}
	for _, v := range out.Simulation.Variables {
		found[v.Name] = v.Points
	}
	if found["frequency"] != 456 {
		t.Fatalf("frequency points = %d, want 456", found["frequency"])
	}
	if _, ok := found["ac.v(out)"]; !ok {
		t.Fatalf("ac.v(out) missing: %+v", found)
	}
	if _, err := os.Stat(out.Path); err != nil {
		t.Fatalf("schematic not written: %v", err)
	}
	if _, err := os.Stat(out.NetlistPath); err != nil {
		t.Fatalf("netlist not written: %v", err)
	}
}

func TestCreateOverwriteGuardIsError(t *testing.T) {
	sess := newWorkingServer(t)
	first, ok := call[map[string]any, rcPathOut](t, sess, "create_schematic",
		map[string]any{"netlist": rcNetlist, "name": "g1"})
	if !ok {
		t.Fatal("first create failed")
	}
	// Same name must now be a structured error.
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "create_schematic",
		Arguments: map[string]any{"netlist": rcNetlist, "name": "g1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("second create must be an IsError result")
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("first file must survive: %v", err)
	}
	// overwrite=true succeeds.
	if _, ok := call[map[string]any, rcPathOut](t, sess, "create_schematic",
		map[string]any{"netlist": rcNetlist, "name": "g1", "overwrite": true}); !ok {
		t.Fatal("overwrite create failed")
	}
}

func TestUpdateSchematicPreservesPositions(t *testing.T) {
	sess := newWorkingServer(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "upd.sch")
	if _, ok := call[map[string]any, rcPathOut](t, sess, "create_schematic",
		map[string]any{"netlist": rcNetlist, "name": "upd", "output_dir": dir}); !ok {
		t.Fatal("create failed")
	}
	positionsBefore := componentPositions(t, base)

	// Change R1's value and regenerate into the same file.
	updated := strings.Replace(rcNetlist, "R1 in out 1k", "R1 in out 3k3", 1)
	out, ok := call[map[string]any, rcCreateOut](t, sess, "update_schematic", map[string]any{"path": base, "netlist": updated})
	if !ok {
		t.Fatal("update failed")
	}
	if !out.Valid {
		t.Fatalf("update validation failed: %+v", out)
	}
	if out.Path != base {
		t.Fatalf("update wrote to %s, want %s", out.Path, base)
	}
	positionsAfter := componentPositions(t, base)
	if len(positionsBefore) == 0 || len(positionsBefore) != len(positionsAfter) {
		t.Fatalf("component count changed: %d vs %d", len(positionsBefore), len(positionsAfter))
	}
	for ref, pos := range positionsBefore {
		if positionsAfter[ref] != pos {
			t.Fatalf("position of %s changed: %+v -> %+v", ref, pos, positionsAfter[ref])
		}
	}
}

// componentPositions extracts ref→(x,y) from the .sch file.
func componentPositions(t *testing.T, path string) map[string][2]int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<") || !strings.Contains(line, " 1 ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		x, errX := strconv.Atoi(fields[3])
		y, errY := strconv.Atoi(fields[4])
		if errX != nil || errY != nil {
			continue
		}
		ref := fields[1]
		if ref == "*" {
			continue
		}
		out[ref] = [2]int{x, y}
	}
	return out
}

type validateCallOut struct {
	Valid  bool `json:"valid"`
	Report *struct {
		Valid bool `json:"valid"`
	} `json:"report"`
}

func TestValidateSchematicByPath(t *testing.T) {
	sess := newWorkingServer(t)
	dir := t.TempDir()
	if _, ok := call[map[string]any, rcPathOut](t, sess, "create_schematic",
		map[string]any{"netlist": rcNetlist, "name": "val", "output_dir": dir}); !ok {
		t.Fatal("create failed")
	}
	out, ok := call[map[string]any, validateCallOut](t, sess, "validate_schematic",
		map[string]any{"path": filepath.Join(dir, "val.sch")})
	if !ok {
		t.Fatal("validate call failed")
	}
	if !out.Valid || out.Report == nil || !out.Report.Valid {
		t.Fatalf("validate must pass for a healthy schematic: %+v", out)
	}

	// A mismatching expected netlist must flag a difference.
	bad := strings.Replace(rcNetlist, "R1 in out 1k", "R2 in mid 1k\nR1 mid out 1k", 1)
	out, ok = call[map[string]any, validateCallOut](t, sess, "validate_schematic", map[string]any{
		"path":             filepath.Join(dir, "val.sch"),
		"expected_netlist": bad,
	})
	if !ok {
		t.Fatal("validate call failed")
	}
	if out.Valid {
		t.Fatalf("validate must fail when the expected netlist differs")
	}
}

type simCallOut struct {
	OK        bool `json:"ok"`
	Variables []struct {
		Name   string       `json:"name"`
		Points int          `json:"points"`
		Data   [][2]float64 `json:"data"`
	} `json:"variables"`
}

func TestSimulateSchematic(t *testing.T) {
	sess := newWorkingServer(t)
	dir := t.TempDir()
	if _, ok := call[map[string]any, rcPathOut](t, sess, "create_schematic",
		map[string]any{"netlist": rcNetlist, "name": "sim", "output_dir": dir}); !ok {
		t.Fatal("create failed")
	}
	out, ok := call[map[string]any, simCallOut](t, sess, "simulate_schematic", map[string]any{
		"path":         filepath.Join(dir, "sim.sch"),
		"timeout_s":    60,
		"include_data": true,
	})
	if !ok {
		t.Fatal("simulate call failed")
	}
	if !out.OK {
		t.Fatal("simulation must succeed")
	}
	var freq, vout *struct {
		Name   string       `json:"name"`
		Points int          `json:"points"`
		Data   [][2]float64 `json:"data"`
	}
	for i := range out.Variables {
		v := &out.Variables[i]
		switch v.Name {
		case "frequency":
			freq = v
		case "ac.v(out)":
			vout = v
		}
	}
	if freq == nil || freq.Points != 456 || len(freq.Data) != 456 {
		t.Fatalf("frequency variable wrong: %+v", freq)
	}
	if vout == nil || len(vout.Data) != 456 {
		t.Fatalf("ac.v(out) wrong: %+v", vout)
	}
	if vout.Data[0][0] < 0.9 || vout.Data[0][0] > 1.01 {
		t.Fatalf("v(out) at 10 Hz = %v, want ~0.998", vout.Data[0][0])
	}
	if math.Abs(vout.Data[len(vout.Data)-1][0]) > 0.65 {
		t.Fatalf("v(out) at 1 MHz = %v, want low-pass roll-off", vout.Data[len(vout.Data)-1])
	}
}

type readCallOut struct {
	Version    string `json:"version"`
	Components []struct {
		Ref   string `json:"ref"`
		Type  string `json:"type"`
		Value string `json:"value"`
		X     int    `json:"x"`
		Y     int    `json:"y"`
	} `json:"components"`
	Wires []struct {
		Label string `json:"label"`
	} `json:"wires"`
	Warnings []string `json:"warnings"`
}

func TestReadSchematic(t *testing.T) {
	sess := newWorkingServer(t)
	dir := t.TempDir()
	if _, ok := call[map[string]any, rcPathOut](t, sess, "create_schematic",
		map[string]any{"netlist": rcNetlist, "name": "rd", "output_dir": dir}); !ok {
		t.Fatal("create failed")
	}
	out, ok := call[map[string]any, readCallOut](t, sess, "read_schematic",
		map[string]any{"path": filepath.Join(dir, "rd.sch")})
	if !ok {
		t.Fatal("read call failed")
	}
	refs := map[string]struct{ typ, value string }{}
	for _, c := range out.Components {
		refs[c.Ref] = struct{ typ, value string }{c.Type, c.Value}
	}
	r1, okc := refs["R1"]
	if !okc || r1.typ != "R" || r1.value != "1k" {
		t.Fatalf("R1 wrong: %+v", refs)
	}
	c1, okc := refs["C1"]
	if !okc || c1.typ != "C" || c1.value != "100n" {
		t.Fatalf("C1 wrong: %+v", refs)
	}
	if _, okc := refs["V1"]; !okc {
		t.Fatalf("V1 missing: %+v", refs)
	}
	labels := map[string]bool{}
	for _, w := range out.Wires {
		if w.Label != "" {
			labels[w.Label] = true
		}
	}
	if !labels["in"] || !labels["out"] {
		t.Fatalf("net labels missing: %+v", labels)
	}
}

type listCallOut struct {
	Types []struct {
		Type        string   `json:"type"`
		SpiceLetter string   `json:"spice_letter"`
		Pins        []string `json:"pins"`
	} `json:"types"`
}

func TestListAndDescribeComponents(t *testing.T) {
	sess, _ := newTestServer(t)
	l, ok := call[map[string]any, listCallOut](t, sess, "list_components", map[string]any{})
	if !ok {
		t.Fatal("list_components failed")
	}
	byType := map[string]struct {
		letter string
		pins   []string
	}{}
	for _, e := range l.Types {
		byType[e.Type] = struct {
			letter string
			pins   []string
		}{e.SpiceLetter, e.Pins}
	}
	r, okc := byType["R"]
	if !okc || r.letter != "R" || len(r.pins) != 2 {
		t.Fatalf("R entry wrong: %+v", r)
	}
	q, okc := byType["_BJT"]
	if !okc || q.letter != "Q" || len(q.pins) != 3 {
		t.Fatalf("BJT entry wrong: %+v", q)
	}

	d, ok := call[map[string]any, struct {
		Type        string `json:"type"`
		SpiceLetter string `json:"spice_letter"`
		Pins        []struct {
			Name string `json:"name"`
		} `json:"pins"`
		Example string `json:"example"`
	}](t, sess, "describe_component", map[string]any{"type": "S4Q_V"})
	if !ok {
		t.Fatal("describe_component failed")
	}
	if d.Type != "S4Q_V" || d.SpiceLetter != "V" || len(d.Pins) != 2 || !strings.Contains(d.Example, "S4Q_V") {
		t.Fatalf("describe S4Q_V wrong: %+v", d)
	}
	// Unknown type is a structured error.
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "describe_component",
		Arguments: map[string]any{"type": "NOPE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("unknown type must be an IsError result")
	}
}

func TestOpenSchematicMissingFileIsError(t *testing.T) {
	sess, _ := newTestServer(t)
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "open_schematic",
		Arguments: map[string]any{"path": filepath.Join(t.TempDir(), "nope.sch")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing file must be an IsError result")
	}
}
