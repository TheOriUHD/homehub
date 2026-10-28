package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// Every write that adds bytes is checked against the quota, including edits
// that grow a file and uploads racing each other. Database-backed.
func TestWorkspaceQuotaHoldsForCreatesEditsAndRaces(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	var userID uuid.UUID
	if err := st.Pool().QueryRow(ctx,
		`insert into users (username, display_name, password_hash, status)
		 values ($1, 'Quota probe', 'x', 'disabled') returning id`,
		"test-probe-"+uuid.NewString()[:8]).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, `delete from users where id = $1`, userID) })

	const quota = 50
	ten := strings.Repeat("x", 10)
	var wg sync.WaitGroup
	var mu sync.Mutex
	stored, refused := 0, 0
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.CreateUserFile(ctx, userID, fmt.Sprintf("f%d.txt", i), "", ten, quota)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				stored++
			case errors.Is(err, store.ErrQuotaExceeded):
				refused++
			default:
				t.Errorf("create: %v", err)
			}
		}()
	}
	wg.Wait()
	if stored != 5 || refused != 5 {
		t.Fatalf("parallel creates: %d stored, %d refused; want 5 and 5", stored, refused)
	}
	if used, _ := st.UserFilesTotalBytes(ctx, userID); used != quota {
		t.Fatalf("used %d bytes, quota %d", used, quota)
	}

	files, _ := st.ListUserFiles(ctx, userID)
	target := files[0]
	if _, err := st.UpdateUserFile(ctx, userID, target.ID, target.Name, ten+"y", quota); !errors.Is(err, store.ErrQuotaExceeded) {
		t.Fatalf("growing past quota: %v", err)
	}
	if _, err := st.UpdateUserFile(ctx, userID, target.ID, target.Name, "short", quota); err != nil {
		t.Fatalf("shrinking must always work: %v", err)
	}
	if _, err := st.CreateBinaryUserFile(ctx, uuid.New(), userID, "big.bin", "", "application/octet-stream", 6, quota); !errors.Is(err, store.ErrQuotaExceeded) {
		t.Fatalf("binary over quota: %v", err)
	}
	if _, err := st.CreateBinaryUserFile(ctx, uuid.New(), userID, "fits.bin", "", "application/octet-stream", 5, quota); err != nil {
		t.Fatalf("binary that fits: %v", err)
	}
}

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
		if _, err := st.CreateUserFile(ctx, userID, f[1], f[0], "x", 1<<30); err != nil {
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
