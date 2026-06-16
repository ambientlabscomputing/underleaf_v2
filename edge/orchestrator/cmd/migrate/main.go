// migrate is a developer tool for managing database migrations.
// It is NOT part of the shipped product — use it during development only.
//
// Usage:
//
//	go run ./cmd/migrate provision --autogenerate -m "describe your change"
//
// With --autogenerate the tool diffs every registered model against the live
// database and writes a new numbered migration file into
// repository/migrations/. Review and commit the generated file; the
// orchestrator will apply it on next start via Migrator.Migrate().
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/migrator"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/utils"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Developer tool for managing orchestrator database migrations",
}

var (
	autogenerate bool
	message      string
)

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Create a new migration file",
	Long: `Create a new versioned migration file in repository/migrations/.

Without --autogenerate: prints a blank migration template to stdout for you
to fill in manually.

With --autogenerate: diffs every registered model against the live database
and writes a numbered .go file with the resulting SQL statements.
Review and edit the file before committing — especially any DROP COLUMN lines.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if message == "" {
			return fmt.Errorf("--message / -m is required")
		}
		if autogenerate {
			return runAutogenerate(message)
		}
		return runBlank(message)
	},
}

func init() {
	provisionCmd.Flags().BoolVar(&autogenerate, "autogenerate", false, "Diff registered models against live DB and generate SQL")
	provisionCmd.Flags().StringVarP(&message, "message", "m", "", "Short description of this migration (required)")
	rootCmd.AddCommand(provisionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// migrationsDir resolves the repository/migrations directory relative to this
// source file so the tool works regardless of the working directory.
func migrationsDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		// Fall back to cwd-relative path.
		return filepath.Join("repository", "migrations")
	}
	// file = …/cmd/migrate/main.go → go up two levels to repo root.
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	return filepath.Join(root, "repository", "migrations")
}

// nextMigrationNumber counts existing numbered migration files and returns
// the next sequence number as a zero-padded 4-digit string.
func nextMigrationNumber(dir string) (string, error) {
	pattern := regexp.MustCompile(`^\d{4}_`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read migrations dir: %w", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && pattern.MatchString(e.Name()) {
			count++
		}
	}
	return fmt.Sprintf("%04d", count+1), nil
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// migrationFileTemplate is the template for a generated migration file.
var migrationFileTemplate = template.Must(template.New("migration").Parse(`package migrations

// {{ .VarName }} was generated on {{ .Date }} with:
//
//	go run ./cmd/migrate provision --autogenerate -m "{{ .Message }}"
//
// Review before committing. Remove any DROP COLUMN lines you do not intend.
var {{ .VarName }} = []string{
{{ range .Stmts }}	` + "`" + `{{ . }}` + "`" + `,
{{ end }}}

func init() {
	Migrations = append(Migrations, Migration{
		ID:  "{{ .ID }}",
		SQL: {{ .VarName }},
	})
}
`))

type templateData struct {
	VarName string
	Date    string
	Message string
	ID      string
	Stmts   []string
}

func writeMigrationFile(dir, id, varName, message string, stmts []string) (string, error) {
	filename := filepath.Join(dir, id+".go")
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	data := templateData{
		VarName: varName,
		Date:    time.Now().Format("2006-01-02"),
		Message: message,
		ID:      id,
		Stmts:   stmts,
	}
	if err := migrationFileTemplate.Execute(f, data); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return filename, nil
}

func runAutogenerate(msg string) error {
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db %s: %w", cfg.DBPath, err)
	}
	defer db.Close()

	var stmts []string
	for _, model := range migrator.Models {
		desired := migrator.BuildTableMetaData(model)
		diff, err := migrator.DiffTableMetaData(db, desired)
		if err != nil {
			return fmt.Errorf("diff %T: %w", model, err)
		}
		stmts = append(stmts, diff...)
	}

	if len(stmts) == 0 {
		fmt.Println("No changes detected — database is up to date.")
		return nil
	}

	dir := migrationsDir()
	num, err := nextMigrationNumber(dir)
	if err != nil {
		return err
	}
	slug := slugify(msg)
	id := num + "_" + slug
	varName := "migration" + num

	filename, err := writeMigrationFile(dir, id, varName, msg, stmts)
	if err != nil {
		return err
	}

	fmt.Printf("Generated: %s\n", filename)
	fmt.Println("Review the file, then commit it. The orchestrator will apply it on next start.")
	return nil
}

func runBlank(msg string) error {
	dir := migrationsDir()
	num, err := nextMigrationNumber(dir)
	if err != nil {
		return err
	}
	slug := slugify(msg)
	id := num + "_" + slug
	varName := "migration" + num

	filename, err := writeMigrationFile(dir, id, varName, msg, []string{"-- TODO: add SQL statements"})
	if err != nil {
		return err
	}

	fmt.Printf("Generated: %s\n", filename)
	fmt.Println("Fill in the SQL statements, then commit the file.")
	return nil
}
