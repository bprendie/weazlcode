package tui

import (
	"strings"
	"testing"
)

func TestBraidContractsStayCompactAndActionable(t *testing.T) {
	contracts := map[string][]string{
		"orchestrator": orchestratorBraidContract(),
		"plan repair":  planRepairBraidContract(),
		"worker":       workerBraidContract(),
		"diff repair":  workerDiffRepairBraidContract(),
	}
	for name, lines := range contracts {
		if len(lines) != 6 {
			t.Fatalf("%s contract lines = %d, want 6: %#v", name, len(lines), lines)
		}
		joined := strings.Join(lines, "\n")
		for _, want := range []string{"Boundary:", "Reasoning:", "Artifact:", "Iteration:", "Diagnostics:"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s contract missing %q:\n%s", name, want, joined)
			}
		}
		if len(joined) > 900 {
			t.Fatalf("%s contract too large: %d chars", name, len(joined))
		}
	}
}

func TestBraidContractsEncodeSplitBrainLoopRules(t *testing.T) {
	planner := strings.Join(orchestratorBraidContract(), "\n")
	for _, want := range []string{"worker capacity", "semi-serial dependencies", "explicit contracts", "final wiring/smoke task"} {
		if !strings.Contains(planner, want) {
			t.Fatalf("planner contract missing %q:\n%s", want, planner)
		}
	}
	worker := strings.Join(workerBraidContract(), "\n")
	for _, want := range []string{"touch the file named by the latest failure first", "rewrite the named file", "return blocker"} {
		if !strings.Contains(worker, want) {
			t.Fatalf("worker contract missing %q:\n%s", want, worker)
		}
	}
}
