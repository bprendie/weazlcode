package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteQueryTool struct {
	limits Limits
}

func NewSQLiteQueryTool(limits Limits) *SQLiteQueryTool {
	return &SQLiteQueryTool{limits: limits}
}

func (t *SQLiteQueryTool) Name() string { return "query_sqlite" }
func (t *SQLiteQueryTool) Description() string {
	return "Run a read-only SQLite query against a database under a configured workspace root"
}
func (t *SQLiteQueryTool) SafetyLevel() SafetyLevel { return SafetyLevelSafe }
func (t *SQLiteQueryTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "db_path", Type: "string", Description: "SQLite database path under a configured workspace root", Required: true},
		{Name: "query", Type: "string", Description: "Read-only SELECT, WITH, EXPLAIN, or PRAGMA table_info query", Required: true},
		{Name: "max_rows", Type: "number", Description: "Maximum rows to return, defaults to 50", Required: false},
	}
}

func (t *SQLiteQueryTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if err := t.limits.RequireRoots(); err != nil {
		return "", err
	}
	dbParam, _ := params["db_path"].(string)
	dbPath, err := t.limits.ResolveAllowed(dbParam)
	if err != nil {
		return "", err
	}
	query, _ := params["query"].(string)
	query = strings.TrimSpace(query)
	if err := validateReadOnlySQL(query); err != nil {
		return "", err
	}
	maxRows := intParam(params, "max_rows", 50, 1, 500)

	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro&_query_only=1")
	if err != nil {
		return "", err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(strings.Join(cols, "\t"))
	b.WriteByte('\n')
	count := 0
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		for i, value := range values {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(sqlValueString(value))
		}
		b.WriteByte('\n')
		count++
		if count >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count == 0 {
		return "Query returned no rows.", nil
	}
	return t.limits.Truncate(b.String()), nil
}

func validateReadOnlySQL(query string) error {
	if query == "" {
		return fmt.Errorf("query parameter is required")
	}
	trimmed := strings.TrimSpace(strings.TrimRight(query, ";"))
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("multiple SQL statements are not allowed")
	}
	lower := strings.ToLower(strings.TrimSpace(trimmed))
	if strings.HasPrefix(lower, "select ") || strings.HasPrefix(lower, "with ") || strings.HasPrefix(lower, "explain ") {
		return nil
	}
	if strings.HasPrefix(lower, "pragma table_info(") || strings.HasPrefix(lower, "pragma table_info ") {
		return nil
	}
	return fmt.Errorf("only read-only SELECT, WITH, EXPLAIN, and PRAGMA table_info queries are allowed")
}

func sqlValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
