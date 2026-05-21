package tui

func orchestratorBraidContract() []string {
	return []string{
		"WeazlCode BRAID planning contract:",
		"Boundary: classify the request, worker capacity, repo shape, allowed outputs, and whether the artifact is cohesive or modular before choosing tasks.",
		"Reasoning: choose parallel tasks only for independent deliverables with explicit contracts; use semi-serial dependencies when a later module must consume generated interfaces.",
		"Artifact: every code task must include interface_contract with exact exports, imports, signatures, entrypoints, commands, paths, and downstream expectations.",
		"Iteration: include one final wiring/smoke task for generated multi-module code; it must depend on module drafts and may edit the entrypoint plus dependency module files.",
		"Diagnostics: acceptance checks should verify the requested artifact, not create separate validation-only work.",
	}
}

func planRepairBraidContract() []string {
	return []string{
		"WeazlCode BRAID plan repair contract:",
		"Boundary: fix only the parser, schema, quality, or dependency defect reported; preserve useful task intent.",
		"Reasoning: if a worker would have to guess an interface, add depends_on or explicit interface details instead of relying on repair loops.",
		"Artifact: keep task ids stable when possible, preserve or add interface_contract, and make allowed_paths sufficient for the task to complete surgically.",
		"Iteration: prefer fewer well-scoped tasks over microtasks; add final wiring only when independent drafts need integration.",
		"Diagnostics: repaired plans must pass quality checks without requiring another model call.",
	}
}

func workerBraidContract() []string {
	return []string{
		"WeazlCode BRAID worker contract:",
		"Boundary: edit only allowed_paths and satisfy only this task packet; do not solve future tasks unless explicitly asked.",
		"Reasoning: first read interface_contract, dependency_contracts, required exports, imports, entrypoints, and acceptance checks; then implement the smallest complete artifact.",
		"Artifact: generated files must be runnable, cohesive, and free of placeholders, markdown fences, wildcard imports, and unreachable smoke paths.",
		"Iteration: on Repair focus or Artifact validation repair focus, touch the file named by the latest failure first; rewrite the named file when syntax or structure is broken.",
		"Diagnostics: if missing context prevents a correct patch, return blocker with the exact missing interface or file instead of guessing.",
	}
}

func workerDiffRepairBraidContract() []string {
	return []string{
		"WeazlCode BRAID diff repair contract:",
		"Boundary: keep the same task id and allowed paths.",
		"Reasoning: repair the patch format or path mistake without changing the requested implementation.",
		"Artifact: if a unified diff is fragile, return files[] with complete content for the affected allowed file.",
		"Iteration: do not return the same ineffective patch; make a material change to the implicated file.",
		"Diagnostics: return blocker when the original patch cannot be repaired safely.",
	}
}
