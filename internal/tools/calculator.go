package tools

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CalculatorTool performs basic mathematical calculations
type CalculatorTool struct{}

// NewCalculatorTool creates a new calculator tool
func NewCalculatorTool() *CalculatorTool {
	return &CalculatorTool{}
}

func (t *CalculatorTool) Name() string {
	return "calculate"
}

func (t *CalculatorTool) Description() string {
	return "Perform basic mathematical calculations. Supports operations: add, subtract, multiply, divide, power, sqrt, and percentage"
}

func (t *CalculatorTool) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "operation",
			Type:        "string",
			Description: "The mathematical operation to perform",
			Required:    true,
			Enum:        []any{"add", "subtract", "multiply", "divide", "power", "sqrt", "percentage"},
		},
		{
			Name:        "a",
			Type:        "number",
			Description: "The first number (or only number for sqrt)",
			Required:    true,
		},
		{
			Name:        "b",
			Type:        "number",
			Description: "The second number (not required for sqrt)",
			Required:    false,
		},
	}
}

func (t *CalculatorTool) SafetyLevel() SafetyLevel {
	return SafetyLevelSafe // Pure computation, no side effects
}

func (t *CalculatorTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	operation, ok := params["operation"].(string)
	if !ok || operation == "" {
		return "", fmt.Errorf("operation parameter is required")
	}

	a, err := getNumber(params, "a")
	if err != nil {
		return "", err
	}

	var result float64

	switch strings.ToLower(operation) {
	case "add":
		b, err := getNumber(params, "b")
		if err != nil {
			return "", err
		}
		result = a + b
		return fmt.Sprintf("%.6g + %.6g = %.6g", a, b, result), nil

	case "subtract":
		b, err := getNumber(params, "b")
		if err != nil {
			return "", err
		}
		result = a - b
		return fmt.Sprintf("%.6g - %.6g = %.6g", a, b, result), nil

	case "multiply":
		b, err := getNumber(params, "b")
		if err != nil {
			return "", err
		}
		result = a * b
		return fmt.Sprintf("%.6g × %.6g = %.6g", a, b, result), nil

	case "divide":
		b, err := getNumber(params, "b")
		if err != nil {
			return "", err
		}
		if b == 0 {
			return "", fmt.Errorf("division by zero")
		}
		result = a / b
		return fmt.Sprintf("%.6g ÷ %.6g = %.6g", a, b, result), nil

	case "power":
		b, err := getNumber(params, "b")
		if err != nil {
			return "", err
		}
		result = math.Pow(a, b)
		return fmt.Sprintf("%.6g ^ %.6g = %.6g", a, b, result), nil

	case "sqrt":
		if a < 0 {
			return "", fmt.Errorf("cannot calculate square root of negative number")
		}
		result = math.Sqrt(a)
		return fmt.Sprintf("√%.6g = %.6g", a, result), nil

	case "percentage":
		b, err := getNumber(params, "b")
		if err != nil {
			return "", err
		}
		result = (a / 100) * b
		return fmt.Sprintf("%.6g%% of %.6g = %.6g", a, b, result), nil

	default:
		return "", fmt.Errorf("unsupported operation: %s", operation)
	}
}

func getNumber(params map[string]any, key string) (float64, error) {
	val, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("%s parameter is required", key)
	}

	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number, got: %s", key, v)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("%s must be a number, got: %T", key, val)
	}
}
