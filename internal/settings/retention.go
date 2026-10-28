package settings

import (
	"context"
	"fmt"
	"time"
)

// History retention: how long the audit log and PatchPilot run history (with
// its captured package-manager output) are kept before the nightly
// maintenance job prunes them. Without a bound both grew for as long as the
// install lived.
const (
	keyAuditRetentionDays      = "audit_retention_days"
	keyPatchpilotRetentionDays = "patchpilot_retention_days"

	defaultAuditDays      = 365
	defaultPatchpilotDays = 90

	// A year of audit history is the default; a month is the floor so a
	// slip of the finger cannot wipe the trail of a recent incident.
	minAuditDays      = 30
	minPatchpilotDays = 7
	maxRetentionDays  = 3650
)

// HistoryRetentionDays returns the audit log and PatchPilot history
// retention in days.
func (s *Service) HistoryRetentionDays(ctx context.Context) (auditDays, patchpilotDays int) {
	auditDays, patchpilotDays = defaultAuditDays, defaultPatchpilotDays
	s.get(ctx, keyAuditRetentionDays, &auditDays)
	s.get(ctx, keyPatchpilotRetentionDays, &patchpilotDays)
	// A stored value from outside the API's bounds (hand-edited row) must
	// never turn into "prune everything".
	if auditDays < minAuditDays || auditDays > maxRetentionDays {
		auditDays = defaultAuditDays
	}
	if patchpilotDays < minPatchpilotDays || patchpilotDays > maxRetentionDays {
		patchpilotDays = defaultPatchpilotDays
	}
	return auditDays, patchpilotDays
}

// HistoryRetention is HistoryRetentionDays as durations.
func (s *Service) HistoryRetention(ctx context.Context) (audit, patchpilot time.Duration) {
	a, p := s.HistoryRetentionDays(ctx)
	return time.Duration(a) * 24 * time.Hour, time.Duration(p) * 24 * time.Hour
}

// SetHistoryRetentionDays validates and stores both windows.
func (s *Service) SetHistoryRetentionDays(ctx context.Context, auditDays, patchpilotDays int) error {
	if auditDays < minAuditDays || auditDays > maxRetentionDays {
		return fmt.Errorf("audit log retention must be between %d and %d days", minAuditDays, maxRetentionDays)
	}
	if patchpilotDays < minPatchpilotDays || patchpilotDays > maxRetentionDays {
		return fmt.Errorf("PatchPilot history retention must be between %d and %d days", minPatchpilotDays, maxRetentionDays)
	}
	if err := s.store.SetSetting(ctx, keyAuditRetentionDays, auditDays); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, keyPatchpilotRetentionDays, patchpilotDays); err != nil {
		return err
	}
	s.invalidate()
	return nil
}
