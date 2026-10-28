package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// The audit log refuses edits and deletes at the database, except a delete
// inside a transaction marked as retention pruning. Database-backed; the
// probe row is removed through that same retention path.
func TestAuditLogIsAppendOnly(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	action := "test.probe." + uuid.NewString()[:8]
	if err := st.InsertAudit(ctx, &store.AuditEntry{ActorKind: "system", Action: action, Outcome: "success"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `update audit_log set outcome = 'failure' where action = $1`, action); err == nil {
		t.Fatal("an audit entry was edited")
	}
	if _, err := st.Pool().Exec(ctx, `delete from audit_log where action = $1`, action); err == nil {
		t.Fatal("an audit entry was deleted outside retention")
	}

	tx, err := st.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select set_config('homehub.audit_retention', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	tag, err := tx.Exec(ctx, `delete from audit_log where action = $1`, action)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("retention delete: %v (%d rows)", err, tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
