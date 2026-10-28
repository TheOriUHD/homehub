package api

import (
	"strings"
	"testing"
)

func TestRedactSQLSecrets(t *testing.T) {
	for _, stmt := range []string{
		"CREATE USER 'bob'@'%' IDENTIFIED BY 's3cr3t'",
		"ALTER USER bob IDENTIFIED WITH mysql_native_password BY 's3cr3t'",
		"SET PASSWORD FOR bob = 's3cr3t'",
		"SET PASSWORD = PASSWORD('s3cr3t')",
		`GRANT ALL ON *.* TO bob IDENTIFIED BY "s3cr3t"`,
		"CREATE USER bob IDENTIFIED BY 'it''s s3cr3t'",
	} {
		got := redactSQLSecrets(stmt)
		if strings.Contains(got, "s3cr3t") {
			t.Errorf("secret survived: %q -> %q", stmt, got)
		}
	}
	plain := "SELECT * FROM users WHERE name = 'password'"
	if got := redactSQLSecrets(plain); got != plain {
		t.Errorf("ordinary SQL changed: %q", got)
	}
}
