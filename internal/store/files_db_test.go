package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Deleting a folder whose name is a LIKE wildcard must delete that folder
// only. Database-backed: run with HOMEHUB_DATABASE_URL set; the probe user
// and its rows are removed afterwards.
func TestDeletingAWildcardFolderLeavesOtherFolders(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	var userID uuid.UUID
	if err := st.Pool().QueryRow(ctx,
		`insert into users (username, display_name, password_hash, status)
		 values ($1, 'Like probe', 'x', 'disabled') returning id`,
		"test-probe-"+uuid.NewString()[:8]).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, `delete from users where id = $1`, userID) })

	for _, folder := range []string{"%", "_x", "docs", "%/inner"} {
		if _, err := st.CreateUserFolder(ctx, userID, folder); err != nil {
			t.Fatalf("folder %q: %v", folder, err)
		}
	}
	for _, f := range [][2]string{{"%", "a.md"}, {"%/inner", "b.md"}, {"docs", "keep.md"}, {"_x", "keep2.md"}} {
		if _, err := st.CreateUserFile(ctx, userID, f[1], f[0], "x"); err != nil {
			t.Fatalf("file %v: %v", f, err)
		}
	}
	folders, err := st.ListUserFolders(ctx, userID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	var pct uuid.UUID
	for _, f := range folders {
		if f.Path == "%" {
			pct = f.ID
		}
	}
	if found, _, err := st.DeleteUserFolder(ctx, userID, pct); err != nil || !found {
		t.Fatalf("delete %%: %v %v", found, err)
	}
	files, _ := st.ListUserFiles(ctx, userID)
	left := map[string]bool{}
	for _, f := range files {
		left[f.Name] = true
	}
	if left["a.md"] || left["b.md"] {
		t.Fatalf("files inside %% survived: %v", left)
	}
	if !left["keep.md"] || !left["keep2.md"] {
		t.Fatalf("deleting %% took other folders' files with it: %v", left)
	}
}
