package db

import (
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
)

// Backups record the schema they were taken at and restores refuse anything
// newer than SupportedVersion, so it must be the newest embedded migration.
func TestSupportedVersionIsTheNewestMigration(t *testing.T) {
	got, err := SupportedVersion()
	if err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(migrationsFS, "migrations/*.sql")
	var want int64
	for _, name := range names {
		v, err := goose.NumericComponent(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want = max(want, v)
	}
	if got != want || got == 0 {
		t.Fatalf("SupportedVersion() = %d, want %d", got, want)
	}
}
