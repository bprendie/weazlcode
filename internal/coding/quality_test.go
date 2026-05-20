package coding

import (
	"strings"
	"testing"
)

func TestValidatePlanQualityAcceptsConcreteTask(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Update README",
		Goal:         "Update README.md to document the Phase 4 setup smoke result.",
		AllowedPaths: []string{"README.md"},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "README mentions the Phase 4 setup smoke result"},
		},
	}}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityFlagsVagueAndUnboundedTask(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:     "task-1",
		Title:  "Loose task",
		Goal:   "do work",
		Status: TaskStatusPending,
	}}}

	issues := ValidatePlanQuality(plan)
	for _, want := range []string{"goal is too vague", "allowed_paths is empty", "acceptance_checks is empty"} {
		if !qualityIssuesContain(issues, want) {
			t.Fatalf("issues missing %q: %#v", want, issues)
		}
	}
}

func TestValidatePlanQualityFlagsBroadPathScope(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Broad task",
		Goal:         "Update the repository documentation to describe the setup flow.",
		AllowedPaths: []string{"."},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "Documentation describes the setup flow"},
		},
	}}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, "allowed_paths is too broad") {
		t.Fatalf("issues = %#v, want broad path issue", issues)
	}
}

func TestValidatePlanQualityFlagsUnknownDependencies(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Dependent task",
		Goal:         "Update README.md to document the setup flow.",
		AllowedPaths: []string{"README.md"},
		DependsOn:    []string{"missing-task"},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "README documents the setup flow"},
		},
	}}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, `depends_on references unknown task id "missing-task"`) {
		t.Fatalf("issues = %#v, want unknown dependency issue", issues)
	}
}

func TestValidatePlanQualityFlagsSerialSharedFileMutation(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{
			ID:           "task-1",
			Title:        "Create base file",
			Goal:         "Create the first draft of the page markup.",
			AllowedPaths: []string{"app/page.html"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "base markup exists"},
			},
		},
		{
			ID:           "task-2",
			Title:        "Add first section",
			Goal:         "Add the first section to the existing page markup.",
			AllowedPaths: []string{"app/page.html"},
			DependsOn:    []string{"task-1"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "first section exists"},
			},
		},
		{
			ID:           "task-3",
			Title:        "Add second section",
			Goal:         "Add the second section to the existing page markup.",
			AllowedPaths: []string{"app/page.html"},
			DependsOn:    []string{"task-2"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "second section exists"},
			},
		},
		{
			ID:           "task-4",
			Title:        "Add third section",
			Goal:         "Add the third section to the existing page markup.",
			AllowedPaths: []string{"app/page.html"},
			DependsOn:    []string{"task-3"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "third section exists"},
			},
		},
	}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, `too many serial tasks mutate "app/page.html"`) {
		t.Fatalf("issues = %#v, want serial shared-file issue", issues)
	}
}

func TestValidatePlanQualityAllowsParallelSeparateFiles(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{
			ID:           "task-1",
			Title:        "Create module one",
			Goal:         "Create the first independent page section module without doctype, html, head, or body document wrapper tags.",
			AllowedPaths: []string{"sections/one.html"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "first module exists"},
			},
		},
		{
			ID:           "task-2",
			Title:        "Create module two",
			Goal:         "Create the second independent page section module without doctype, html, head, or body document wrapper tags.",
			AllowedPaths: []string{"sections/two.html"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "second module exists"},
			},
		},
		{
			ID:           "task-3",
			Title:        "Create module three",
			Goal:         "Create the third independent page section module without doctype, html, head, or body document wrapper tags.",
			AllowedPaths: []string{"sections/three.html"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "third module exists"},
			},
		},
		{
			ID:           "task-4",
			Title:        "Assemble modules",
			Goal:         "Assemble the independent page section modules into the final page.",
			AllowedPaths: []string{"app/page.html"},
			DependsOn:    []string{"task-1", "task-2", "task-3"},
			AcceptanceChecks: []AcceptanceCheck{
				{Description: "final page includes all modules"},
			},
		},
	}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityAllowsLayeredCodeModules(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "cards", Title: "Cards", Goal: "Create card and deck module.", AllowedPaths: []string{"cards.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "cards module exists"}}},
		{ID: "game", Title: "Game state", Goal: "Create game state module using the card/deck interface contract.", AllowedPaths: []string{"game.py"}, DependsOn: []string{"cards"}, AcceptanceChecks: []AcceptanceCheck{{Description: "game module exists"}}},
		{ID: "renderer", Title: "Renderer", Goal: "Create pygame renderer module using the game state interface contract.", AllowedPaths: []string{"renderer.py"}, DependsOn: []string{"game"}, AcceptanceChecks: []AcceptanceCheck{{Description: "renderer module exists"}}},
		{ID: "main", Title: "Entrypoint", Goal: "Wire modules and smoke mode.", AllowedPaths: []string{"main.py", "cards.py", "game.py", "renderer.py"}, DependsOn: []string{"renderer"}, AcceptanceChecks: []AcceptanceCheck{{Description: "main smoke works"}}},
	}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityFlagsSingleFileInteractivePythonGame(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "main", Title: "Create flappy bird game", Goal: "Use pygame to make a flappy bird type game with colorful rendering.", AllowedPaths: []string{"main.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "python main.py --smoke exits"}}},
		{ID: "readme", Title: "Document game", Goal: "Document how to run the pygame game.", AllowedPaths: []string{"README.md"}, DependsOn: []string{"main"}, AcceptanceChecks: []AcceptanceCheck{{Description: "README documents run command"}}},
	}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, "generated interactive Python app/game plan is single-file") {
		t.Fatalf("issues = %#v, want single-file interactive Python issue", issues)
	}
}

func TestValidatePlanQualityAllowsExplicitSingleFileInteractivePythonGame(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "main", Title: "Create one file pygame game", Goal: "Use pygame to make a one file flappy bird type game.", AllowedPaths: []string{"main.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "python main.py --smoke exits"}}},
	}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityAllowsParallelCodeDraftsWithFinalWiring(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "cards", Title: "Cards", Goal: "Create card and deck module.", AllowedPaths: []string{"cards.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "cards module exists"}}},
		{ID: "game", Title: "Game state", Goal: "Create game state module using the card/deck interface contract.", AllowedPaths: []string{"game.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "game module exists"}}},
		{ID: "renderer", Title: "Renderer", Goal: "Create pygame renderer module using the game state interface contract.", AllowedPaths: []string{"renderer.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "renderer module exists"}}},
		{ID: "main", Title: "Entrypoint", Goal: "Wire modules and smoke mode.", AllowedPaths: []string{"main.py", "cards.py", "game.py", "renderer.py"}, DependsOn: []string{"cards", "game", "renderer"}, AcceptanceChecks: []AcceptanceCheck{{Description: "main smoke works"}}},
	}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityFlagsIntegrationTaskWithoutModuleSurfaces(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "cards", Title: "Cards", Goal: "Create card and deck module.", AllowedPaths: []string{"cards.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "cards module exists"}}},
		{ID: "game", Title: "Game state", Goal: "Create game state module using the card/deck interface contract.", AllowedPaths: []string{"game.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "game module exists"}}},
		{ID: "renderer", Title: "Renderer", Goal: "Create pygame renderer module using the game state interface contract.", AllowedPaths: []string{"renderer.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "renderer module exists"}}},
		{ID: "main", Title: "Entrypoint", Goal: "Wire modules and smoke mode.", AllowedPaths: []string{"main.py"}, DependsOn: []string{"cards", "game", "renderer"}, AcceptanceChecks: []AcceptanceCheck{{Description: "main smoke works"}}},
	}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, "cannot edit their integration surfaces") {
		t.Fatalf("issues = %#v, want integration surface issue", issues)
	}
}

func TestValidatePlanQualityFlagsDependentCodeTaskWithoutModuleSurfaces(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "bird", Title: "Bird", Goal: "Create bird module.", AllowedPaths: []string{"bird.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "bird module exists"}}},
		{ID: "pipe", Title: "Pipe", Goal: "Create pipe module.", AllowedPaths: []string{"pipe.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "pipe module exists"}}},
		{ID: "state", Title: "Game state", Goal: "Create game state module that composes Bird and Pipe for collisions and scoring.", AllowedPaths: []string{"game_state.py"}, DependsOn: []string{"bird", "pipe"}, AcceptanceChecks: []AcceptanceCheck{{Description: "game state module exists"}}},
	}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, "dependent code task composes multiple module drafts") {
		t.Fatalf("issues = %#v, want dependent code task scope issue", issues)
	}
}

func TestValidatePlanQualityAllowsDependentCodeTaskWithModuleSurfaces(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "bird", Title: "Bird", Goal: "Create bird module.", AllowedPaths: []string{"bird.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "bird module exists"}}},
		{ID: "pipe", Title: "Pipe", Goal: "Create pipe module.", AllowedPaths: []string{"pipe.py"}, AcceptanceChecks: []AcceptanceCheck{{Description: "pipe module exists"}}},
		{ID: "state", Title: "Game state", Goal: "Create game state module that composes Bird and Pipe for collisions and scoring.", AllowedPaths: []string{"game_state.py", "bird.py", "pipe.py"}, DependsOn: []string{"bird", "pipe"}, AcceptanceChecks: []AcceptanceCheck{{Description: "game state module exists"}}},
	}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityFlagsHTMLFragmentWithoutDocumentWrapperGuard(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Create hero card",
		Goal:         "Create a reusable hero card fragment for the landing page.",
		AllowedPaths: []string{"sections/hero.html"},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "hero fragment exists"},
		},
	}}}

	issues := ValidatePlanQuality(plan)
	if !qualityIssuesContain(issues, "HTML fragment/module task must explicitly forbid doctype/html/head/body") {
		t.Fatalf("issues = %#v, want HTML fragment guard issue", issues)
	}
}

func TestValidatePlanQualityAllowsHTMLFragmentWithDocumentWrapperGuard(t *testing.T) {
	plan := Plan{Tasks: []Task{{
		ID:           "task-1",
		Title:        "Create hero card",
		Goal:         "Create a reusable hero card fragment without doctype, html, head, or body document wrapper tags.",
		AllowedPaths: []string{"sections/hero.html"},
		AcceptanceChecks: []AcceptanceCheck{
			{Description: "hero fragment exists"},
		},
	}}}

	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidatePlanQualityFlagsOverfragmentedSinglePageStaticPlan(t *testing.T) {
	tasks := []Task{
		{ID: "hero", Title: "Hero", Goal: "Create hero fragment without doctype, html, head, or body.", AllowedPaths: []string{"sections/hero.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "hero exists"}}},
		{ID: "intro", Title: "Intro", Goal: "Create intro fragment without doctype, html, head, or body.", AllowedPaths: []string{"sections/intro.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "intro exists"}}},
		{ID: "chat", Title: "Chat", Goal: "Create chat card fragment without doctype, html, head, or body.", AllowedPaths: []string{"components/chat.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "chat exists"}}},
		{ID: "tunes", Title: "Tunes", Goal: "Create tunes card fragment without doctype, html, head, or body.", AllowedPaths: []string{"components/tunes.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "tunes exists"}}},
		{ID: "write", Title: "Write", Goal: "Create write card fragment without doctype, html, head, or body.", AllowedPaths: []string{"components/write.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "write exists"}}},
		{ID: "code", Title: "Code", Goal: "Create code card fragment without doctype, html, head, or body.", AllowedPaths: []string{"components/code.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "code exists"}}},
		{ID: "css", Title: "CSS", Goal: "Create stylesheet.", AllowedPaths: []string{"styles.css"}, AcceptanceChecks: []AcceptanceCheck{{Description: "css exists"}}},
		{ID: "readme", Title: "README", Goal: "Create README.", AllowedPaths: []string{"README.md"}, AcceptanceChecks: []AcceptanceCheck{{Description: "README exists"}}},
		{ID: "assemble", Title: "Assemble", Goal: "Assemble final index page.", AllowedPaths: []string{"index.html"}, DependsOn: []string{"hero", "intro", "chat", "tunes", "write", "code"}, AcceptanceChecks: []AcceptanceCheck{{Description: "index exists"}}},
	}
	issues := ValidatePlanQuality(Plan{Tasks: tasks})
	if !qualityIssuesContain(issues, "single-page static website plan has too many fragment/module tasks") {
		t.Fatalf("issues = %#v, want overfragmented static plan issue", issues)
	}
}

func TestValidatePlanQualityAllowsCompactSinglePageStaticPlan(t *testing.T) {
	plan := Plan{Tasks: []Task{
		{ID: "html", Title: "Build page", Goal: "Create complete browser-openable index.html from supplied copy and assets.", AllowedPaths: []string{"index.html"}, AcceptanceChecks: []AcceptanceCheck{{Description: "index includes supplied copy and assets"}}},
		{ID: "css", Title: "Style page", Goal: "Create complete responsive styles.css using plain browser CSS.", AllowedPaths: []string{"styles.css"}, AcceptanceChecks: []AcceptanceCheck{{Description: "styles are responsive"}}},
		{ID: "readme", Title: "Document page", Goal: "Create README with concise usage notes.", AllowedPaths: []string{"README.md"}, AcceptanceChecks: []AcceptanceCheck{{Description: "README explains how to open the page"}}},
	}}
	if issues := ValidatePlanQuality(plan); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func qualityIssuesContain(issues []PlanQualityIssue, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, want) {
			return true
		}
	}
	return false
}
