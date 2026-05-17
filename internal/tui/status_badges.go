package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bprendie/weazlcode/internal/config"
)

func (m model) statusBadges(contextTokens, budget int) []string {
	return []string{
		"proj:" + projectBadgeName(m.project.Root),
		"git:" + branchBadge(m.project.GitRoot, m.project.Branch),
		"repo:" + dirtyBadge(m.project.GitRoot, m.project.Dirty),
		"role:" + m.statusRoleBadge(),
		fmt.Sprintf("ctx:%d%%", contextPercent(contextTokens, budget)),
	}
}

func (m model) statusRoleBadge() string {
	if m.hasModelWork() {
		return m.processingRole()
	}
	return activeRoleBadge(m.cfg.ActiveProvider, m.cfg.ModelRoles)
}

func projectBadgeName(root string) string {
	name := filepath.Base(root)
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		return root
	}
	return name
}

func branchBadge(gitRoot bool, branch string) string {
	if !gitRoot {
		return "none"
	}
	if strings.TrimSpace(branch) == "" {
		return "detached"
	}
	return branch
}

func dirtyBadge(gitRoot, dirty bool) string {
	if !gitRoot {
		return "no-git"
	}
	if dirty {
		return "dirty"
	}
	return "clean"
}

func activeRoleBadge(activeProvider string, roles config.ModelRoles) string {
	switch activeProvider {
	case roles.Worker:
		return "worker"
	case roles.Reviewer:
		return "reviewer"
	case roles.Summarizer:
		return "summarizer"
	case roles.Orchestrator:
		return "orchestrator"
	default:
		if strings.TrimSpace(activeProvider) == "" {
			return "unset"
		}
		return activeProvider
	}
}

func contextPercent(tokens, budget int) int {
	if budget <= 0 {
		return 0
	}
	pct := int(float64(tokens) / float64(budget) * 100)
	if pct < 0 {
		return 0
	}
	if pct > 999 {
		return 999
	}
	return pct
}
