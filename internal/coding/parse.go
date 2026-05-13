package coding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func ParsePlanJSON(raw []byte) (Plan, error) {
	plan, err := DecodePlanJSON(raw)
	if err != nil {
		return Plan{}, err
	}
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func DecodePlanJSON(raw []byte) (Plan, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var plan Plan
	if err := dec.Decode(&plan); err != nil {
		return Plan{}, err
	}
	if dec.More() {
		return Plan{}, fmt.Errorf("plan JSON contains trailing data")
	}
	return plan, nil
}

func ParseWorkerPatchJSON(raw []byte) (WorkerPatch, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var patch WorkerPatch
	if err := dec.Decode(&patch); err != nil {
		return WorkerPatch{}, err
	}
	if dec.More() {
		return WorkerPatch{}, fmt.Errorf("worker patch JSON contains trailing data")
	}
	patch = NormalizeWorkerPatch(patch)
	if err := ValidateWorkerPatch(patch); err != nil {
		return WorkerPatch{}, err
	}
	return patch, nil
}

func NormalizeWorkerPatch(patch WorkerPatch) WorkerPatch {
	patch.TaskID = strings.TrimSpace(patch.TaskID)
	patch.Summary = strings.TrimSpace(patch.Summary)
	patch.Blocker = strings.TrimSpace(patch.Blocker)
	for i := range patch.Files {
		patch.Files[i].Path = strings.TrimSpace(patch.Files[i].Path)
	}
	switch strings.ToLower(patch.Blocker) {
	case "none", "no", "n/a", "na", "null", "nil":
		patch.Blocker = ""
	}
	return patch
}

func ParseReviewVerdictJSON(raw []byte) (ReviewVerdict, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var verdict ReviewVerdict
	if err := dec.Decode(&verdict); err != nil {
		return ReviewVerdict{}, err
	}
	if dec.More() {
		return ReviewVerdict{}, fmt.Errorf("review verdict JSON contains trailing data")
	}
	if err := ValidateReviewVerdict(verdict); err != nil {
		return ReviewVerdict{}, err
	}
	return verdict, nil
}

func PrepareImportedPlan(plan Plan, sessionID, projectRoot string, idFunc func() string) Plan {
	if idFunc == nil {
		idFunc = func() string { return "" }
	}
	if strings.TrimSpace(plan.ID) == "" {
		plan.ID = idFunc()
	}
	plan.SessionID = sessionID
	plan.ProjectRoot = projectRoot
	if strings.TrimSpace(plan.Status) == "" {
		plan.Status = PlanStatusDraft
	}
	for i := range plan.Tasks {
		if strings.TrimSpace(plan.Tasks[i].ID) == "" {
			plan.Tasks[i].ID = idFunc()
		}
		plan.Tasks[i].PlanID = plan.ID
		plan.Tasks[i].AllowedPaths = normalizePlanPaths(plan.Tasks[i].AllowedPaths, projectRoot)
		plan.Tasks[i].ForbiddenPaths = normalizePlanPaths(plan.Tasks[i].ForbiddenPaths, projectRoot)
		plan.Tasks[i].ContextFiles = normalizePlanPaths(plan.Tasks[i].ContextFiles, projectRoot)
		if strings.TrimSpace(plan.Tasks[i].Status) == "" {
			plan.Tasks[i].Status = TaskStatusPending
		}
	}
	return plan
}

func normalizePlanPaths(paths []string, projectRoot string) []string {
	if len(paths) == 0 {
		return paths
	}
	root := filepath.Clean(projectRoot)
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if filepath.IsAbs(clean) {
			if rel, err := filepath.Rel(root, clean); err == nil && relInsideRoot(rel) {
				clean = rel
			}
		}
		normalized = append(normalized, filepath.ToSlash(clean))
	}
	return normalized
}

func relInsideRoot(rel string) bool {
	return rel == "." || (rel != ".." && !strings.HasPrefix(filepath.ToSlash(rel), "../"))
}
