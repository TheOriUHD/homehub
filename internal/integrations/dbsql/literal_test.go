package dbsql

import "testing"

func TestSQLStringLiteral(t *testing.T) {
	for in, want := range map[string]string{
		"plain":              "'plain'",
		"it's":               "'it''s'",
		`a\b`:                `'a\\b'`,
		`\', 1); DROP x -- `: `'\\'', 1); DROP x -- '`,
	} {
		got, err := sqlStringLiteral(in)
		if err != nil || got != want {
			t.Errorf("sqlStringLiteral(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := sqlStringLiteral("a\x00b"); err == nil {
		t.Error("a NUL byte must be refused")
	}
}
