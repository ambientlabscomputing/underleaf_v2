package migrations

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

type ColumnMetaData struct {
	Name string
	Type string
}

type TableMetaData struct {
	Name       string
	Columns    []ColumnMetaData
	PrimaryKey string
}

func (t *TableMetaData) ToSQLCreate() string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(t.Name)
	sb.WriteString(" (\n")

	for i, col := range t.Columns {
		sb.WriteString("\t")
		sb.WriteString(col.Name)
		sb.WriteString(" ")
		sb.WriteString(col.Type)
		if col.Name == t.PrimaryKey {
			sb.WriteString(" PRIMARY KEY")
		}
		if i < len(t.Columns)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(");")
	return sb.String()
}

// BuildTableMetaData uses reflection to derive TableMetaData from a struct value
// or pointer, similar to how Alembic inspects Python models. Column names are
// taken from the `json` struct tag when present, falling back to snake_case of
// the field name. SQL types are inferred from Go kinds. The primary key is the
// first field tagged with `db:"pk"`, or the field named "ID" by convention.
func BuildTableMetaData(_type interface{}) TableMetaData {
	t := reflect.TypeOf(_type)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	tableName := toSnakeCase(t.Name()) + "s"

	var columns []ColumnMetaData
	primaryKey := ""

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Resolve column name: prefer json tag, fall back to snake_case field name.
		colName := toSnakeCase(field.Name)
		if tag, ok := field.Tag.Lookup("json"); ok {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				colName = parts[0]
			}
		}

		colType := goKindToSQL(field.Type)

		columns = append(columns, ColumnMetaData{Name: colName, Type: colType})

		// Primary key detection: explicit db tag takes priority, then "id" convention.
		if dbTag, ok := field.Tag.Lookup("db"); ok && dbTag == "pk" {
			primaryKey = colName
		} else if primaryKey == "" && strings.ToLower(field.Name) == "id" {
			primaryKey = colName
		}
	}

	return TableMetaData{
		Name:       tableName,
		Columns:    columns,
		PrimaryKey: primaryKey,
	}
}

// goKindToSQL maps a Go reflect.Type to a SQL column type string.
func goKindToSQL(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "TEXT"
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "INTEGER"
	case reflect.Float32, reflect.Float64:
		return "REAL"
	case reflect.Struct:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return "TIMESTAMP"
		}
		return "TEXT"
	default:
		return "TEXT"
	}
}

// toSnakeCase converts a CamelCase identifier to snake_case, treating runs of
// uppercase letters (e.g. "ID") as a single word: "NodeID" → "node_id".
func toSnakeCase(s string) string {
	runes := []rune(s)
	var out []rune
	for i, r := range runes {
		upper := unicode.IsUpper(r)
		if upper && i > 0 {
			prevLower := unicode.IsLower(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				out = append(out, '_')
			}
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}

// DiffTableMetaData is a developer tool — not called at runtime. It queries
// PRAGMA table_info to compare the live schema against the desired
// TableMetaData and returns a []string of SQL statements (ADD COLUMN / DROP
// COLUMN) representing the delta. If the table does not exist it returns the
// full CREATE TABLE statement. The caller pastes the result into a new
// migration file.
func DiffTableMetaData(db *sql.DB, desired TableMetaData) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", desired.Name))
	if err != nil {
		return nil, fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Table does not exist yet — return a CREATE TABLE statement.
	if len(existing) == 0 {
		return []string{desired.ToSQLCreate()}, nil
	}

	// Build desired column set for reverse diff.
	desiredSet := make(map[string]bool, len(desired.Columns))
	for _, col := range desired.Columns {
		desiredSet[col.Name] = true
	}

	var stmts []string

	// Columns present in desired but missing from live schema → ADD COLUMN.
	for _, col := range desired.Columns {
		if !existing[col.Name] {
			stmts = append(stmts,
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", desired.Name, col.Name, col.Type))
		}
	}

	// Columns present in live schema but absent from desired (skip PK) → DROP COLUMN.
	for name := range existing {
		if !desiredSet[name] && name != desired.PrimaryKey {
			stmts = append(stmts,
				fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", desired.Name, name))
		}
	}

	return stmts, nil
}
