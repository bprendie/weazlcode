package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/bprendie/weazlcode/internal/storage"
)

type RememberTool struct {
	store  *storage.Store
	limits Limits
}

type RecallTool struct {
	store  *storage.Store
	limits Limits
}

type ListMemoriesTool struct {
	store  *storage.Store
	limits Limits
}

type ForgetTool struct {
	store *storage.Store
}

func NewRememberTool(store *storage.Store, limits Limits) *RememberTool {
	return &RememberTool{store: store, limits: limits}
}
func NewRecallTool(store *storage.Store, limits Limits) *RecallTool {
	return &RecallTool{store: store, limits: limits}
}
func NewListMemoriesTool(store *storage.Store, limits Limits) *ListMemoriesTool {
	return &ListMemoriesTool{store: store, limits: limits}
}
func NewForgetTool(store *storage.Store) *ForgetTool { return &ForgetTool{store: store} }

func (t *RememberTool) Name() string     { return "remember" }
func (t *RecallTool) Name() string       { return "recall" }
func (t *ListMemoriesTool) Name() string { return "list_memories" }
func (t *ForgetTool) Name() string       { return "forget" }

func (t *RememberTool) Description() string {
	return "Store an explicit local encrypted memory by key"
}
func (t *RecallTool) Description() string {
	return "Search explicit local encrypted memories"
}
func (t *ListMemoriesTool) Description() string {
	return "List recent explicit local encrypted memories"
}
func (t *ForgetTool) Description() string {
	return "Delete an explicit local encrypted memory by key"
}

func (t *RememberTool) SafetyLevel() SafetyLevel     { return SafetyLevelSafe }
func (t *RecallTool) SafetyLevel() SafetyLevel       { return SafetyLevelSafe }
func (t *ListMemoriesTool) SafetyLevel() SafetyLevel { return SafetyLevelSafe }
func (t *ForgetTool) SafetyLevel() SafetyLevel       { return SafetyLevelSafe }

func (t *RememberTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "key", Type: "string", Description: "Stable memory key", Required: true},
		{Name: "value", Type: "string", Description: "Memory value", Required: true},
		{Name: "tags", Type: "string", Description: "Optional comma-separated tags", Required: false},
	}
}

func (t *RecallTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "query", Type: "string", Description: "Optional search text", Required: false},
		{Name: "limit", Type: "number", Description: "Maximum memories to return, defaults to 10", Required: false},
	}
}

func (t *ListMemoriesTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "limit", Type: "number", Description: "Maximum memories to return, defaults to 20", Required: false},
	}
}

func (t *ForgetTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "key", Type: "string", Description: "Memory key to delete", Required: true},
	}
}

func (t *RememberTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	key, _ := params["key"].(string)
	value, _ := params["value"].(string)
	tags, _ := params["tags"].(string)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return "", fmt.Errorf("key parameter is required")
	}
	if value == "" {
		return "", fmt.Errorf("value parameter is required")
	}
	if err := t.store.Remember(key, value, strings.TrimSpace(tags)); err != nil {
		return "", err
	}
	return "Memory saved: " + key, nil
}

func (t *RecallTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	limit := intParam(params, "limit", 10, 1, 100)
	memories, err := t.store.RecallMemories(query, limit)
	if err != nil {
		return "", err
	}
	return t.limits.Truncate(formatMemories(memories)), nil
}

func (t *ListMemoriesTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	limit := intParam(params, "limit", 20, 1, 100)
	memories, err := t.store.Memories(limit)
	if err != nil {
		return "", err
	}
	return t.limits.Truncate(formatMemories(memories)), nil
}

func (t *ForgetTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	key, _ := params["key"].(string)
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("key parameter is required")
	}
	if err := t.store.ForgetMemory(key); err != nil {
		return "", err
	}
	return "Memory deleted: " + key, nil
}

func formatMemories(memories []storage.Memory) string {
	if len(memories) == 0 {
		return "No memories found."
	}
	var b strings.Builder
	for _, memory := range memories {
		fmt.Fprintf(&b, "%s", memory.Key)
		if memory.Tags != "" {
			fmt.Fprintf(&b, " [%s]", memory.Tags)
		}
		fmt.Fprintf(&b, "\n%s\n\n", memory.Value)
	}
	return strings.TrimSpace(b.String())
}
