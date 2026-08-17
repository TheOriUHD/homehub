package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/integrations/tailscale"
	"github.com/TheOriUHD/homehub/internal/jobs"
	"github.com/TheOriUHD/homehub/internal/notify"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

const (
	// renewNoticeWindow: an INFO notification counts down the last week.
	renewNoticeWindow = 7 * 24 * time.Hour
	// renewRotateWindow: inside the last day the token is rotated.
	renewRotateWindow = 24 * time.Hour
	// renewedKeyLifetime is what replacement tokens are minted with.
	renewedKeyLifetime = 90 * 24 * time.Hour
)

// registerTailscaleRenewal wires the hourly job that keeps Tailscale API
// tokens alive for instances that opted in: an INFO notification each day
// of the final week, and an automatic rotation one day before expiry —
// mint a clearly-named replacement, swap the vault secret, reconnect, and
// revoke the old token.
func registerTailscaleRenewal(
	engine *jobs.Engine,
	st *store.Store,
	v *vault.Vault,
	manager *integration.Manager,
	rbacEngine *rbac.Engine,
	notifySvc *notify.Service,
	logger *slog.Logger,
) {
	log := logger.With("component", "tailscale-renew")

	notifyViewers := func(ctx context.Context, inst store.Integration, severity, title, body string) {
		resource := inst.Type + "/" + inst.ID.String()
		notifySvc.NotifyResourceViewers(ctx, resource,
			func(ctx context.Context, userID uuid.UUID) bool {
				ok, err := rbacEngine.VisibleUnder(ctx, rbac.Principal{UserID: userID}, resource)
				return err == nil && ok
			}, severity, title, body, "tailscale.key")
	}

	// markOncePerDay records a dedupe stamp in the instance config so a
	// notification fires at most once per day per kind.
	markOncePerDay := func(ctx context.Context, row store.Integration, cfg map[string]any, kind string) bool {
		today := time.Now().Format("2006-01-02")
		if cfg[kind] == today {
			return false
		}
		cfg[kind] = today
		if err := st.UpdateIntegration(ctx, row.ID, row.Name, cfg, row.Enabled); err != nil {
			log.Error("persist renewal marker", "error", err)
		}
		return true
	}

	engine.Register("tailscale.renew", func(ctx context.Context, _ json.RawMessage) error {
		rows, err := st.ListIntegrations(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Type != tailscale.TypeID || !row.Enabled {
				continue
			}
			cfg := integration.ConfigFromJSON(row.Config)
			if enabled, _ := cfg["autoRenewKey"].(bool); !enabled {
				continue
			}
			conn, err := manager.Connection(row.ID)
			if err != nil {
				continue // unreachable instances are already reported elsewhere
			}
			tc, ok := conn.(*tailscale.Conn)
			if !ok {
				continue
			}

			own, err := tc.OwnKey(ctx)
			if err != nil {
				if markOncePerDay(ctx, row, cfg, "_renewErrDate") {
					notifyViewers(ctx, row, "warning", row.Name+": token expiry unknown",
						"Auto-renewal is on, but the token's expiry could not be read: "+err.Error())
				}
				continue
			}
			if own.Expires.IsZero() {
				continue // non-expiring token: nothing to renew
			}
			left := time.Until(own.Expires.Time)

			switch {
			case left <= renewRotateWindow:
				renewed, err := rotateToken(ctx, st, v, manager, tc, row, own.ID)
				if err != nil {
					log.Error("token rotation failed", "integration", row.Name, "error", err)
					if markOncePerDay(ctx, row, cfg, "_renewFailDate") {
						notifyViewers(ctx, row, "warning", row.Name+": automatic token renewal failed",
							err.Error()+" — create a new API token in the Tailscale admin console and update the system before it expires.")
					}
					continue
				}
				log.Info("token rotated", "integration", row.Name, "newKey", renewed)
				notifyViewers(ctx, row, "info", row.Name+": API token renewed",
					fmt.Sprintf("The token was %s from expiry, so HomeHub minted a replacement (%s), switched to it, and revoked the old one.",
						left.Round(time.Hour), renewed))
			case left <= renewNoticeWindow:
				if markOncePerDay(ctx, row, cfg, "_renewNoticeDate") {
					days := int(left.Hours() / 24)
					notifyViewers(ctx, row, "info", row.Name+": API token expires soon",
						fmt.Sprintf("The Tailscale API token expires in about %d day(s). HomeHub renews it automatically one day before expiry.", days))
				}
			}
		}
		return nil
	})
}

// rotateToken mints the replacement, swaps the vault secret, reconnects the
// instance, and revokes the old token using the NEW credential. Returns the
// replacement's description for the notification.
func rotateToken(
	ctx context.Context,
	st *store.Store,
	v *vault.Vault,
	manager *integration.Manager,
	tc *tailscale.Conn,
	row store.Integration,
	oldKeyID string,
) (string, error) {
	desc := "HomeHub auto-renewal " + time.Now().Format("2006-01-02 15:04")
	replacement, err := tc.CreateAPIKey(ctx, desc, int64(renewedKeyLifetime.Seconds()))
	if err != nil {
		return "", fmt.Errorf("minting the replacement token failed: %s", err)
	}
	if err := v.Seal(ctx, "integration:"+row.ID.String(), "apiKey", []byte(replacement.Key)); err != nil {
		return "", fmt.Errorf("storing the replacement token failed: %w", err)
	}
	manager.Apply(ctx, row.ID)

	// Revoke the old token with the NEW one — independent of how fast the
	// manager reconnects.
	cfg := integration.ConfigFromJSON(row.Config)
	if err := tailscale.RevokeKeyWithToken(ctx, cfg, replacement.Key, oldKeyID); err != nil {
		// The rotation itself succeeded; the leftover key still dies at its
		// original expiry. Report, don't fail.
		return desc + " (old token could not be revoked: " + err.Error() + ")", nil
	}
	return desc, nil
}
