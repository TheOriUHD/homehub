// Package dbdump runs database dumps into backup targets: mysqldump or
// mongodump piped through gzip into a temp file, then uploaded through the
// backup target abstraction. Outcomes land in the shared backup run history.
package dbdump

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	backuppkg "homehub/internal/backup"
	"homehub/internal/integration"
	"homehub/internal/integrations/dbsql"
	"homehub/internal/integrations/mongodb"
	"homehub/internal/store"
	"homehub/internal/vault"
)

// JobPayload parameterizes the db.dump job kind.
type JobPayload struct {
	IntegrationID uuid.UUID `json:"integrationId"`
	Database      string    `json:"database"`
	TargetID      uuid.UUID `json:"targetId"`
	UserID        uuid.UUID `json:"userId,omitempty"`
}

// Service executes dumps. It reads connection secrets straight from the
// vault (the dump tools need real credentials on their command line env).
type Service struct {
	store  *store.Store
	vault  *vault.Vault
	backup *backuppkg.Service
	logger *slog.Logger
	// Notify posts the outcome to the requesting user; wired in app.
	Notify func(ctx context.Context, userID uuid.UUID, severity, title, body string)
}

func New(st *store.Store, v *vault.Vault, backup *backuppkg.Service, logger *slog.Logger) *Service {
	return &Service{store: st, vault: v, backup: backup, logger: logger.With("component", "dbdump"),
		Notify: func(context.Context, uuid.UUID, string, string, string) {}}
}

// Run executes one dump job.
func (s *Service) Run(ctx context.Context, payload json.RawMessage) error {
	var p JobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	err := s.dump(ctx, p)
	if p.UserID != uuid.Nil {
		if err != nil {
			s.Notify(ctx, p.UserID, "error", "Database dump failed", err.Error())
		} else {
			s.Notify(ctx, p.UserID, "success", "Database dump completed", p.Database)
		}
	}
	return err
}

func (s *Service) dump(ctx context.Context, p JobPayload) error {
	inst, err := s.store.GetIntegration(ctx, p.IntegrationID)
	if err != nil {
		return fmt.Errorf("integration not found")
	}
	targetRow, err := s.store.GetBackupTarget(ctx, p.TargetID)
	if err != nil {
		return fmt.Errorf("backup target not found")
	}
	runID, err := s.store.StartBackupRun(ctx, targetRow.ID, targetRow.Name, "db-dump")
	if err != nil {
		return err
	}

	archive, size, err := s.execute(ctx, inst, p, targetRow)
	if err != nil {
		_ = s.store.FinishBackupRun(ctx, runID, "failed", archive, 0, err.Error())
		return err
	}
	_ = s.store.FinishBackupRun(ctx, runID, "succeeded", archive, size, "")
	s.logger.Info("database dumped", "integration", inst.Name, "database", p.Database, "archive", archive)
	return nil
}

func (s *Service) execute(ctx context.Context, inst *store.Integration, p JobPayload, targetRow *store.BackupTarget) (string, int64, error) {
	config := integration.ConfigFromJSON(inst.Config)
	secret := func(name string) string {
		raw, err := s.vault.Open(ctx, "integration:"+inst.ID.String(), name)
		if err != nil {
			return ""
		}
		return string(raw)
	}

	var cmd *exec.Cmd
	host, _ := config["host"].(string)
	switch inst.Type {
	case dbsql.TypeID:
		hostname, port := splitHostPort(host, "3306")
		bin, err := exec.LookPath("mariadb-dump")
		if err != nil {
			if bin, err = exec.LookPath("mysqldump"); err != nil {
				return "", 0, fmt.Errorf("no mysqldump/mariadb-dump binary available in this installation")
			}
		}
		cmd = exec.CommandContext(ctx, bin,
			"--host="+hostname, "--port="+port,
			"--user="+secret("username"),
			"--single-transaction", "--routines", "--triggers",
			p.Database)
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+secret("password"))
	case mongodb.TypeID:
		hostname, port := splitHostPort(host, "27017")
		bin, err := exec.LookPath("mongodump")
		if err != nil {
			return "", 0, fmt.Errorf("no mongodump binary available in this installation")
		}
		args := []string{
			"--host=" + hostname, "--port=" + port,
			"--db=" + p.Database, "--archive",
		}
		if user := secret("username"); user != "" {
			authSource, _ := config["authSource"].(string)
			if authSource == "" {
				authSource = "admin"
			}
			args = append(args, "--username="+user, "--password="+secret("password"),
				"--authenticationDatabase="+authSource)
		}
		cmd = exec.CommandContext(ctx, bin, args...)
	default:
		return "", 0, fmt.Errorf("dumps are not supported for %s", inst.Type)
	}

	// Dump → gzip → temp file; upload after the tool exits cleanly so a
	// mid-dump failure never leaves a truncated archive on the target.
	tmp, err := os.CreateTemp("", "homehub-dbdump-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	gz := gzip.NewWriter(tmp)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", 0, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", 0, err
	}
	if _, err := io.Copy(gz, stdout); err != nil {
		_ = cmd.Wait()
		return "", 0, err
	}
	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return "", 0, fmt.Errorf("dump tool failed: %s", detail)
	}
	if err := gz.Close(); err != nil {
		return "", 0, err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return "", 0, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}

	ext := ".sql.gz"
	if inst.Type == mongodb.TypeID {
		ext = ".archive.gz"
	}
	archive := path.Join("homehub", "dumps", sanitize(inst.Name),
		fmt.Sprintf("%s-%s%s", sanitize(p.Database), time.Now().Format("20060102-150405"), ext))

	target, err := s.backup.OpenTarget(ctx, targetRow)
	if err != nil {
		return archive, 0, err
	}
	defer target.Close()
	if err := target.Write(ctx, archive, tmp, size); err != nil {
		return archive, 0, fmt.Errorf("uploading to %s: %w", targetRow.Name, err)
	}
	return archive, size, nil
}

func splitHostPort(host, defaultPort string) (string, string) {
	host = strings.TrimSpace(host)
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		return host[:idx], host[idx+1:]
	}
	return host, defaultPort
}

func sanitize(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		case r == ' ':
			return '-'
		}
		return '_'
	}, name)
	return strings.Trim(out, "._-")
}
