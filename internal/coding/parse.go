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
	patch, err := decodeWorkerPatchJSON(raw)
	if err != nil {
		sanitized := sanitizeInvalidJSONEscapes(raw)
		if !bytes.Equal(sanitized, raw) {
			patch, err = decodeWorkerPatchJSON(sanitized)
		}
		if err != nil {
			if recovered, ok := recoverLooseWorkerPatchJSON(sanitized); ok {
				patch = recovered
				err = nil
			}
		}
	}
	if err != nil {
		return WorkerPatch{}, err
	}
	patch = NormalizeWorkerPatch(patch)
	if err := ValidateWorkerPatch(patch); err != nil {
		return WorkerPatch{}, err
	}
	return patch, nil
}

func decodeWorkerPatchJSON(raw []byte) (WorkerPatch, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var patch WorkerPatch
	if err := dec.Decode(&patch); err != nil {
		return WorkerPatch{}, err
	}
	if dec.More() {
		return WorkerPatch{}, fmt.Errorf("worker patch JSON contains trailing data")
	}
	return patch, nil
}

type jsonStringPair struct {
	key   string
	value string
}

func recoverLooseWorkerPatchJSON(raw []byte) (WorkerPatch, bool) {
	pairs := scanLooseJSONStringPairs(raw)
	if len(pairs) == 0 {
		return WorkerPatch{}, false
	}
	var patch WorkerPatch
	var pendingPath string
	for _, pair := range pairs {
		switch pair.key {
		case "task_id":
			if patch.TaskID == "" {
				patch.TaskID = pair.value
			}
		case "summary":
			if patch.Summary == "" {
				patch.Summary = pair.value
			}
		case "patch":
			if patch.Patch == "" {
				patch.Patch = pair.value
			}
		case "blocker":
			if patch.Blocker == "" {
				patch.Blocker = pair.value
			}
		case "path":
			pendingPath = pair.value
		case "content":
			if strings.TrimSpace(pendingPath) != "" {
				patch.Files = append(patch.Files, WorkerFileEdit{
					Path:    pendingPath,
					Content: pair.value,
				})
				pendingPath = ""
			}
		}
	}
	return patch, strings.TrimSpace(patch.TaskID) != "" && (len(patch.Files) > 0 || strings.TrimSpace(patch.Patch) != "" || strings.TrimSpace(patch.Blocker) != "")
}

func scanLooseJSONStringPairs(raw []byte) []jsonStringPair {
	var pairs []jsonStringPair
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		key, next, ok := readJSONString(raw, i)
		if !ok {
			continue
		}
		j := skipJSONSpace(raw, next)
		if j >= len(raw) || raw[j] != ':' {
			i = next
			continue
		}
		j = skipJSONSpace(raw, j+1)
		if j >= len(raw) || raw[j] != '"' {
			i = next
			continue
		}
		value, end, ok := readJSONString(raw, j)
		if !ok {
			i = next
			continue
		}
		pairs = append(pairs, jsonStringPair{key: key, value: value})
		i = end - 1
	}
	return pairs
}

func skipJSONSpace(raw []byte, i int) int {
	for i < len(raw) {
		switch raw[i] {
		case ' ', '\n', '\r', '\t':
			i++
		default:
			return i
		}
	}
	return i
}

func readJSONString(raw []byte, start int) (string, int, bool) {
	if start >= len(raw) || raw[start] != '"' {
		return "", start, false
	}
	escaped := false
	for i := start + 1; i < len(raw); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch raw[i] {
		case '\\':
			escaped = true
		case '"':
			var value string
			if err := json.Unmarshal(raw[start:i+1], &value); err != nil {
				return "", i + 1, false
			}
			return value, i + 1, true
		}
	}
	return "", len(raw), false
}

func sanitizeInvalidJSONEscapes(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escaped := false
	for _, b := range raw {
		if !inString {
			out = append(out, b)
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			if !validJSONEscapeByte(b) {
				out = append(out, '\\')
			}
			out = append(out, b)
			escaped = false
			continue
		}
		out = append(out, b)
		switch b {
		case '\\':
			escaped = true
		case '"':
			inString = false
		}
	}
	return out
}

func validJSONEscapeByte(b byte) bool {
	switch b {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	default:
		return false
	}
}

func NormalizeWorkerPatch(patch WorkerPatch) WorkerPatch {
	patch.TaskID = strings.TrimSpace(patch.TaskID)
	patch.Summary = strings.TrimSpace(patch.Summary)
	patch.Blocker = strings.TrimSpace(patch.Blocker)
	for i := range patch.Files {
		patch.Files[i].Path = strings.TrimSpace(patch.Files[i].Path)
		patch.Files[i].Content = stripWholeFileMarkdownFence(patch.Files[i].Content)
	}
	switch strings.ToLower(patch.Blocker) {
	case "none", "no", "n/a", "na", "null", "nil":
		patch.Blocker = ""
	}
	return patch
}

func stripWholeFileMarkdownFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return content
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return content
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return content
	}
	body := strings.Join(lines[1:len(lines)-1], "\n")
	if strings.HasSuffix(content, "\n") {
		body += "\n"
	}
	return body
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
		plan.Tasks[i].Skills = normalizePlanSkills(plan.Tasks[i].Skills)
		plan.Tasks[i].DependsOn = normalizePlanSkills(plan.Tasks[i].DependsOn)
		if strings.TrimSpace(plan.Tasks[i].Status) == "" {
			plan.Tasks[i].Status = TaskStatusPending
		}
	}
	return plan
}

func normalizePlanSkills(skills []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		skill = strings.TrimSpace(skill)
		if skill == "" || seen[skill] {
			continue
		}
		seen[skill] = true
		out = append(out, skill)
	}
	return out
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
