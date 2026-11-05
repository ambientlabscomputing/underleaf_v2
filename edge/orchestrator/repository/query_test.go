package repository

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestQueryIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "orch.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("db_path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", cfgPath)

	// WAL mode with a live writer, like the real orchestrator.
	rw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"CREATE TABLE t(a INTEGER)",
		"INSERT INTO t VALUES (1)",
	} {
		if _, err := rw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	r := &Repository{}

	out, err := r.Query("SELECT a FROM t")
	if err != nil || !strings.Contains(out, "1") {
		t.Fatalf("select: out=%q err=%v", out, err)
	}

	evil := filepath.Join(dir, "evil.db")
	for name, q := range map[string]string{
		"insert":                "INSERT INTO t VALUES (2)",
		"delete":                "DELETE FROM t",
		"drop":                  "DROP TABLE t",
		"pragma off then write": "PRAGMA query_only=0; DELETE FROM t",
		"attach creates file":   "ATTACH DATABASE '" + evil + "' AS x",
	} {
		if _, err := r.Query(q); err == nil {
			t.Errorf("%s: query unexpectedly succeeded", name)
		}
	}

	if _, err := os.Stat(evil); err == nil {
		t.Error("ATTACH created a file on disk")
	}
	var n int
	if err := rw.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil || n != 1 {
		t.Errorf("table modified: count=%d err=%v", n, err)
	}
}
