package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// SafetyLevel defines how a tool should be executed
type SafetyLevel int

const (
	// SafetyLevelSafe - Auto-execute without user confirmation (read-only operations)
	SafetyLevelSafe SafetyLevel = iota
	// SafetyLevelPrompt - Require user confirmation before execution
	SafetyLevelPrompt
	// SafetyLevelDangerous - Require explicit user confirmation with warning
	SafetyLevelDangerous
)

// Parameter defines a tool parameter
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // string, number, boolean, object, array
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Enum        []any  `json:"enum,omitempty"`
}

// Tool defines the interface for all tools
type Tool interface {
	// Name returns the tool name (used in function calls)
	Name() string
	// Description returns what the tool does
	Description() string
	// Parameters returns the tool's parameters
	Parameters() []Parameter
	// SafetyLevel returns the safety level for execution
	SafetyLevel() SafetyLevel
	// Execute runs the tool with given parameters
	Execute(ctx context.Context, params map[string]any) (string, error)
}

// Registry manages available tools
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns all registered tools
func (r *Registry) List() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name() < tools[j].Name()
	})
	return tools
}

// ToOpenAIFormat converts tools to OpenAI function calling format
func (r *Registry) ToOpenAIFormat() []map[string]any {
	functions := make([]map[string]any, 0, len(r.tools))
	for _, tool := range r.List() {
		properties := make(map[string]any)
		required := make([]string, 0)

		for _, param := range tool.Parameters() {
			paramDef := map[string]any{
				"type":        param.Type,
				"description": param.Description,
			}
			if len(param.Enum) > 0 {
				paramDef["enum"] = param.Enum
			}
			properties[param.Name] = paramDef
			if param.Required {
				required = append(required, param.Name)
			}
		}

		function := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters": map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			},
		}
		functions = append(functions, function)
	}
	return functions
}

// ToOllamaFormat converts tools to Ollama tool format
func (r *Registry) ToOllamaFormat() []map[string]any {
	tools := make([]map[string]any, 0, len(r.tools))
	for _, tool := range r.List() {
		properties := make(map[string]any)
		required := make([]string, 0)

		for _, param := range tool.Parameters() {
			paramDef := map[string]any{
				"type":        param.Type,
				"description": param.Description,
			}
			if len(param.Enum) > 0 {
				paramDef["enum"] = param.Enum
			}
			properties[param.Name] = paramDef
			if param.Required {
				required = append(required, param.Name)
			}
		}

		toolDef := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters": map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			},
		}
		tools = append(tools, toolDef)
	}
	return tools
}

// ToolCall represents a tool invocation from the LLM
type ToolCall struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Args    map[string]any `json:"arguments"`
	RawArgs string         `json:"raw_arguments,omitempty"`
}

// ParseArguments parses the arguments from a JSON string
func (tc *ToolCall) ParseArguments() error {
	if tc.RawArgs == "" {
		return nil
	}
	return json.Unmarshal([]byte(tc.RawArgs), &tc.Args)
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	Error      string `json:"error,omitempty"`
}

// Execute runs a tool call through the registry
func (r *Registry) Execute(ctx context.Context, call ToolCall) ToolResult {
	tool, ok := r.Get(call.Name)
	if !ok {
		return ToolResult{
			ToolCallID: call.ID,
			Error:      fmt.Sprintf("tool %q not found", call.Name),
		}
	}

	// Parse arguments if needed
	if err := call.ParseArguments(); err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Error:      fmt.Sprintf("failed to parse arguments: %v", err),
		}
	}

	// Execute the tool
	result, err := tool.Execute(ctx, call.Args)
	if err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Error:      err.Error(),
		}
	}

	return ToolResult{
		ToolCallID: call.ID,
		Content:    result,
	}
}
