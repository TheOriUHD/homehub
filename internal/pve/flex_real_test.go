package pve

import (
	"encoding/json"
	"testing"
)

// Real-cluster response shapes that diverged from the simulator (found on
// the user's live cluster 2026-08-17).
func TestFlexPropStringRealShapes(t *testing.T) {
	// prune-backups as an object (newer PVE).
	var job BackupJob
	raw := []byte(`{"id":"backup-1","prune-backups":{"keep-last":3,"keep-weekly":2},"schedule":"sun 02:00"}`)
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatalf("object prune: %v", err)
	}
	if job.Prune.String() != "keep-last=3,keep-weekly=2" {
		t.Fatalf("canonical prune = %q", job.Prune)
	}
	// prune-backups as a property string (older PVE / config style).
	if err := json.Unmarshal([]byte(`{"prune-backups":"keep-last=5"}`), &job); err != nil {
		t.Fatalf("string prune: %v", err)
	}
	if job.Prune.String() != "keep-last=5" {
		t.Fatalf("string prune = %q", job.Prune)
	}
}

func TestNodeTaskListRealShape(t *testing.T) {
	// The node task list carries the exit verdict in `status` ("OK" or error
	// text); running tasks lack endtime. No separate exitstatus field.
	var tasks []Task
	raw := []byte(`[
		{"upid":"UPID:n1:0001:0001:0001:vncproxy:114:root@pam:","type":"vncproxy","id":"114","user":"root@pam","status":"OK","starttime":1755400000,"endtime":1755400100},
		{"upid":"UPID:n1:0002:0002:0002:vzdump:101:root@pam:","type":"vzdump","id":"101","user":"root@pam","status":"job errors","starttime":1755400000,"endtime":1755400500},
		{"upid":"UPID:n1:0003:0003:0003:qmstart:102:root@pam:","type":"qmstart","id":"102","user":"root@pam","starttime":1755400600}
	]`)
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tasks[0].Status != "OK" || int64(tasks[0].EndTime) == 0 {
		t.Fatalf("finished-ok row: %+v", tasks[0])
	}
	if tasks[2].Status != "" || int64(tasks[2].EndTime) != 0 {
		t.Fatalf("running row: %+v", tasks[2])
	}
}
