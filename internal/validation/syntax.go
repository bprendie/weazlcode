package validation

import (
	"encoding/json"
	"strconv"
	"strings"
)

// checkPythonSyntax performs basic Python syntax checks.
func checkPythonSyntax(path, content string) []string {
	warnings := make([]string, 0)

	lines := strings.Split(content, "\n")

	// Check for common Python syntax issues
	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check for unclosed strings (basic check)
		if strings.Count(line, `"`)%2 != 0 || strings.Count(line, `'`)%2 != 0 {
			warnings = append(warnings, formatWarning(path, lineNum, "possible unclosed string"))
		}

		// Check for mismatched parentheses (basic check)
		openParen := strings.Count(line, "(")
		closeParen := strings.Count(line, ")")
		if openParen != closeParen {
			warnings = append(warnings, formatWarning(path, lineNum, "mismatched parentheses"))
		}

		// Check for mismatched brackets
		openBracket := strings.Count(line, "[")
		closeBracket := strings.Count(line, "]")
		if openBracket != closeBracket {
			warnings = append(warnings, formatWarning(path, lineNum, "mismatched brackets"))
		}

		// Check for mismatched braces
		openBrace := strings.Count(line, "{")
		closeBrace := strings.Count(line, "}")
		if openBrace != closeBrace {
			warnings = append(warnings, formatWarning(path, lineNum, "mismatched braces"))
		}
	}

	return warnings
}

// checkHTMLSyntax performs basic HTML syntax checks.
func checkHTMLSyntax(path, content string) []string {
	warnings := make([]string, 0)

	// Check for basic HTML structure
	if !strings.Contains(strings.ToLower(content), "<html") {
		warnings = append(warnings, formatWarning(path, 0, "missing <html> tag"))
	}
	if !strings.Contains(strings.ToLower(content), "<body") {
		warnings = append(warnings, formatWarning(path, 0, "missing <body> tag"))
	}

	// Check for unclosed tags (basic check)
	lines := strings.Split(content, "\n")
	tagStack := make([]string, 0)

	for i, line := range lines {
		lineNum := i + 1

		// Find all tags in the line
		for j := 0; j < len(line); j++ {
			if line[j] == '<' {
				end := strings.IndexByte(line[j:], '>')
				if end == -1 {
					warnings = append(warnings, formatWarning(path, lineNum, "unclosed tag"))
					break
				}

				tag := line[j : j+end+1]
				tagName := extractTagName(tag)

				if tagName == "" {
					continue
				}

				// Skip self-closing tags and special tags
				if strings.HasSuffix(tag, "/>") || isSelfClosingTag(tagName) {
					continue
				}

				// Handle closing tags
				if strings.HasPrefix(tagName, "/") {
					openTag := tagName[1:]
					if len(tagStack) == 0 {
						warnings = append(warnings, formatWarning(path, lineNum, "closing tag without opening: "+tagName))
					} else if tagStack[len(tagStack)-1] != openTag {
						warnings = append(warnings, formatWarning(path, lineNum, "mismatched closing tag: expected "+tagStack[len(tagStack)-1]+", got "+openTag))
					} else {
						tagStack = tagStack[:len(tagStack)-1]
					}
				} else {
					// Opening tag
					tagStack = append(tagStack, tagName)
				}

				j += end
			}
		}
	}

	// Check for unclosed tags at end
	if len(tagStack) > 0 {
		warnings = append(warnings, formatWarning(path, 0, "unclosed tags: "+strings.Join(tagStack, ", ")))
	}

	return warnings
}

// checkJSONSyntax validates JSON syntax.
func checkJSONSyntax(path, content string) []string {
	warnings := make([]string, 0)

	var js interface{}
	if err := json.Unmarshal([]byte(content), &js); err != nil {
		warnings = append(warnings, formatWarning(path, 0, "invalid JSON: "+err.Error()))
	}

	return warnings
}

// extractTagName extracts the tag name from an HTML tag string.
func extractTagName(tag string) string {
	tag = strings.TrimPrefix(tag, "<")
	tag = strings.TrimSuffix(tag, ">")
	tag = strings.TrimSuffix(tag, "/")
	tag = strings.TrimSpace(tag)

	// Get just the tag name (before any attributes)
	parts := strings.Fields(tag)
	if len(parts) == 0 {
		return ""
	}

	return strings.ToLower(parts[0])
}

// isSelfClosingTag checks if a tag is self-closing.
func isSelfClosingTag(tagName string) bool {
	selfClosing := map[string]bool{
		"area":   true,
		"base":   true,
		"br":     true,
		"col":    true,
		"embed":  true,
		"hr":     true,
		"img":    true,
		"input":  true,
		"link":   true,
		"meta":   true,
		"param":  true,
		"source": true,
		"track":  true,
		"wbr":    true,
	}

	return selfClosing[strings.ToLower(tagName)]
}

// formatWarning formats a validation warning message.
func formatWarning(path string, line int, message string) string {
	if line > 0 {
		return path + ":" + strconv.Itoa(line) + ": " + message
	}
	return path + ": " + message
}
