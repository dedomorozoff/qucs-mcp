// Concurrency at the tool layer. Two overlapping headless Qucs-S runs
// pick up each other's intermediate ngspice deck, so a dataset can come
// back with the other circuit's node names (measured on Qucs-S 26.1.1).
// The server must not let that happen when two MCP clients ask for work
// at the same time.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// simCases are two voltage dividers whose nets deliberately share no
// name, so a foreign variable in a result is unmistakable.
var simCases = []struct {
	name    string
	netlist string
	own     string
	foreign string
}{
	{"divider_alpha", "V1 va 0 DC 5\nR1 va vb 1k\nR2 vb 0 2k\n.op\n.end\n", "v(vb)", "v(vd)"},
	{"divider_gamma", "V1 vc 0 DC 5\nR1 vc vd 2k\nR2 vd 0 1k\n.op\n.end\n", "v(vd)", "v(vb)"},
}

// toolText flattens a tool result into its JSON payload.
func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestConcurrentSimulationsKeepTheirOwnResults(t *testing.T) {
	sessions := newWorkingServerSet(t, len(simCases))
	const rounds = 3

	var (
		mu       sync.Mutex
		problems []string
	)
	report := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	for round := range rounds {
		var wg sync.WaitGroup
		for i, c := range simCases {
			sess := sessions[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
					Name: "create_schematic",
					Arguments: map[string]any{
						"netlist":  c.netlist,
						"name":     fmt.Sprintf("%s_r%d", c.name, round),
						"simulate": true,
					},
				})
				if err != nil {
					report("%s round %d: call: %v", c.name, round, err)
					return
				}
				if res.IsError {
					report("%s round %d: tool error: %s", c.name, round, toolText(res))
					return
				}
				var out rcCreateOut
				if err := json.Unmarshal([]byte(toolText(res)), &out); err != nil {
					report("%s round %d: decode: %v", c.name, round, err)
					return
				}
				if !out.Valid {
					report("%s round %d: netlist does not match the model: %+v", c.name, round, out.Report)
				}
				if out.Simulation == nil || !out.Simulation.OK {
					report("%s round %d: simulation failed: %+v", c.name, round, out.Simulation)
					return
				}
				var names []string
				for _, v := range out.Simulation.Variables {
					names = append(names, v.Name)
				}
				joined := strings.Join(names, " ")
				if !strings.Contains(joined, c.own) {
					report("%s round %d: its own variable %q is missing from %v", c.name, round, c.own, names)
				}
				if strings.Contains(joined, c.foreign) {
					report("%s round %d: dataset carries the other circuit's variable %q: %v",
						c.name, round, c.foreign, names)
				}
			}()
		}
		wg.Wait()
	}

	for _, p := range problems {
		t.Error(p)
	}
}
