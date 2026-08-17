package tailscale

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"homehub/internal/integration"
	"homehub/internal/tailscalesim"
)

func newTestConn(t *testing.T) *Conn {
	t.Helper()
	srv := httptest.NewServer(tailscalesim.New())
	t.Cleanup(srv.Close)
	c, err := newClient(
		map[string]any{"tailnet": tailscalesim.Tailnet, "baseUrl": srv.URL},
		map[string]string{"apiKey": tailscalesim.APIKey},
	)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return &Conn{
		inst:   integration.Instance{ID: uuid.New(), Type: TypeID, Name: "sim"},
		client: c,
		deps:   integration.Deps{Publish: func(string, any) {}},
	}
}

func TestTestVerdicts(t *testing.T) {
	srv := httptest.NewServer(tailscalesim.New())
	defer srv.Close()

	res := Driver{}.Test(context.Background(),
		map[string]any{"tailnet": "-", "baseUrl": srv.URL},
		map[string]string{"apiKey": tailscalesim.APIKey})
	if !res.OK || !strings.Contains(res.Message, "5 device(s)") {
		t.Fatalf("expected 5 devices, got ok=%v %q", res.OK, res.Message)
	}

	bad := Driver{}.Test(context.Background(),
		map[string]any{"baseUrl": srv.URL},
		map[string]string{"apiKey": "tskey-api-wrong"})
	if bad.OK || !strings.Contains(bad.Message, "rejected the API token") {
		t.Fatalf("expected auth rejection, got ok=%v %q", bad.OK, bad.Message)
	}
}

func TestDevicesShape(t *testing.T) {
	conn := newTestConn(t)
	devices, err := conn.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 5 {
		t.Fatalf("expected 5 devices, got %d", len(devices))
	}
	var gateway, nas, pending *Device
	for i := range devices {
		switch devices[i].ID {
		case tailscalesim.GatewayID:
			gateway = &devices[i]
		case tailscalesim.NasID:
			nas = &devices[i]
		case tailscalesim.PendingID:
			pending = &devices[i]
		}
	}
	if gateway == nil || nas == nil || pending == nil {
		t.Fatal("expected fixture devices missing")
	}
	if !gateway.Online() || len(gateway.AdvertisedRoutes) != 3 || len(gateway.EnabledRoutes) != 1 {
		t.Fatalf("gateway not mapped: %+v", gateway)
	}
	if nas.Online() || !nas.UpdateAvailable || !nas.KeyExpiryDisabled || !nas.Expires.IsZero() {
		t.Fatalf("nas not mapped: %+v", nas)
	}
	if pending.Authorized {
		t.Fatalf("pending device should be unauthorized")
	}
}

func TestDeviceActions(t *testing.T) {
	conn := newTestConn(t)
	ctx := context.Background()

	if err := conn.SetAuthorized(ctx, tailscalesim.PendingID, true); err != nil {
		t.Fatalf("SetAuthorized: %v", err)
	}
	if err := conn.SetRoutes(ctx, tailscalesim.GatewayID, []string{"192.168.20.0/24", "0.0.0.0/0", "::/0"}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}
	if err := conn.SetKeyExpiryDisabled(ctx, tailscalesim.GatewayID, true); err != nil {
		t.Fatalf("SetKeyExpiryDisabled: %v", err)
	}
	if err := conn.ExpireKey(ctx, tailscalesim.LaptopID); err != nil {
		t.Fatalf("ExpireKey: %v", err)
	}
	if err := conn.DeleteDevice(ctx, tailscalesim.PhoneID); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}

	devices, err := conn.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices after actions: %v", err)
	}
	if len(devices) != 4 {
		t.Fatalf("expected 4 devices after delete, got %d", len(devices))
	}
	for _, d := range devices {
		switch d.ID {
		case tailscalesim.PendingID:
			if !d.Authorized {
				t.Fatal("pending device should now be authorized")
			}
		case tailscalesim.GatewayID:
			if len(d.EnabledRoutes) != 3 || !d.KeyExpiryDisabled {
				t.Fatalf("gateway routes/key not applied: %+v", d)
			}
		case tailscalesim.LaptopID:
			// Expire sets the key's expiry to now; the node itself drops
			// only when control disconnects it, so Online may lag.
			if d.Expires.IsZero() || time.Since(d.Expires.Time) > time.Minute {
				t.Fatalf("laptop key should be expired now, got %v", d.Expires)
			}
		}
	}

	if err := conn.DeleteDevice(ctx, "999999"); err == nil {
		t.Fatal("deleting an unknown device must error")
	}
}

func TestKeysLifecycle(t *testing.T) {
	conn := newTestConn(t)
	ctx := context.Background()

	created, err := conn.CreateKey(ctx, CreateKeySpec{
		Description: "homehub test", Reusable: true, Preauthorized: true, ExpirySeconds: 3600,
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if !strings.HasPrefix(created.Key, "tskey-auth-") {
		t.Fatalf("create must return the secret once, got %q", created.Key)
	}

	keys, err := conn.Keys(ctx)
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	for _, k := range keys {
		if k.Key != "" {
			t.Fatalf("list must never carry secrets, got %q", k.Key)
		}
		if k.ID == created.ID {
			if !k.Capabilities.Devices.Create.Reusable || !k.Capabilities.Devices.Create.Preauthorized {
				t.Fatalf("capabilities not round-tripped: %+v", k.Capabilities)
			}
		}
	}

	if err := conn.RevokeKey(ctx, created.ID); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	keys, _ = conn.Keys(ctx)
	for _, k := range keys {
		if k.ID == created.ID && (k.Revoked.IsZero() || !k.Invalid) {
			t.Fatalf("key should be revoked+invalid: %+v", k)
		}
	}
}

func TestDNSRoundTrip(t *testing.T) {
	conn := newTestConn(t)
	ctx := context.Background()

	dns, err := conn.DNS(ctx)
	if err != nil {
		t.Fatalf("DNS: %v", err)
	}
	if len(dns.Nameservers) != 2 || !dns.MagicDNS || len(dns.SearchPaths) != 1 {
		t.Fatalf("unexpected DNS fixture: %+v", dns)
	}

	if err := conn.SetDNS(ctx, DNSSettings{
		Nameservers: []string{"192.168.20.1"}, MagicDNS: true, SearchPaths: []string{"lan", "home"},
	}); err != nil {
		t.Fatalf("SetDNS: %v", err)
	}
	dns, _ = conn.DNS(ctx)
	if len(dns.Nameservers) != 1 || dns.Nameservers[0] != "192.168.20.1" || len(dns.SearchPaths) != 2 {
		t.Fatalf("DNS not applied: %+v", dns)
	}

	// MagicDNS without nameservers must be rejected upstream.
	err = conn.SetDNS(ctx, DNSSettings{Nameservers: []string{}, MagicDNS: true})
	if err == nil || !strings.Contains(err.Error(), "nameserver") {
		t.Fatalf("expected MagicDNS-without-nameservers rejection, got %v", err)
	}
}

func TestCollectSamples(t *testing.T) {
	conn := newTestConn(t)
	samples, err := conn.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float64{}
	for _, s := range samples {
		got[s.Metric] = s.Value
	}
	if got["devices_total"] != 5 || got["devices_online"] != 3 {
		t.Fatalf("unexpected samples: %+v", got)
	}
}
