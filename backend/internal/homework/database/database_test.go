package database

import (
	"os"
	"testing"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
)

// 集成测试：需本地 MySQL。TEST_MYSQL=1 go test ./internal/homework/database/...
func TestMigrateAllTables(t *testing.T) {
	if os.Getenv("TEST_MYSQL") == "" {
		t.Skip("set TEST_MYSQL=1 (and DB_*) to run against a real MySQL")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(cfg.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, m := range model.All() {
		if !db.Migrator().HasTable(m) {
			t.Errorf("missing table for %T", m)
		}
	}
}
