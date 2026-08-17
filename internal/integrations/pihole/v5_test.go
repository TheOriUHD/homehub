package pihole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheOriUHD/homehub/internal/integrations/dns"
)

// v5Fixture emulates the legacy api.php surface closely enough to pin the
// client's parsing and auth behavior. Real-server coverage exists for v6;
// v5 hardware is rare enough that this fixture is the regression net.
func v5Fixture(t *testing.T) *httptest.Server {
	t.Helper()
	lists := map[string][]map[string]any{
		"black": {{"id": 1, "domain": "ads.example.com", "enabled": 1, "comment": "seeded"}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/api.php", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("auth") != "v5token" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		respond := func(v any) {
			_ = json.NewEncoder(w).Encode(v)
		}
		switch {
		case q.Has("summaryRaw"):
			respond(map[string]any{
				"dns_queries_today": 1234, "ads_blocked_today": 321,
				"ads_percentage_today": 26.01, "unique_clients": 7,
				"domains_being_blocked": 90000, "status": "enabled",
			})
		case q.Has("status"), q.Has("enable"):
			respond(map[string]string{"status": "enabled"})
		case q.Has("disable"):
			respond(map[string]string{"status": "disabled"})
		case q.Has("getAllQueries"):
			respond(map[string]any{"data": [][]string{
				{"1700000000", "A", "example.com", "192.168.1.2", "2"},
				{"1700000001", "A", "ads.example.com", "192.168.1.2", "1"},
				{"1700000002", "AAAA", "cached.example.com", "192.168.1.3", "3"},
			}})
		case q.Has("getQuerySources"):
			respond(map[string]any{"top_sources": map[string]int64{
				"desktop|192.168.1.2": 100, "192.168.1.3": 50,
			}})
		case q.Has("list") && q.Has("add"):
			name := q.Get("list")
			lists[name] = append(lists[name], map[string]any{
				"id": len(lists[name]) + 10, "domain": q.Get("add"), "enabled": 1,
			})
			respond(map[string]string{"success": "true"})
		case q.Has("list") && q.Has("sub"):
			name, domain := q.Get("list"), q.Get("sub")
			kept := lists[name][:0]
			for _, entry := range lists[name] {
				if entry["domain"] != domain {
					kept = append(kept, entry)
				}
			}
			lists[name] = kept
			respond(map[string]string{"success": "true"})
		case q.Has("list"):
			respond(map[string]any{"data": lists[q.Get("list")]})
		case q.Has("versions"):
			respond(map[string]any{"core_current": "v5.18.4"})
		default:
			respond(map[string]any{})
		}
	})
	return httptest.NewServer(mux)
}

func TestV5DetectionAndSurface(t *testing.T) {
	server := v5Fixture(t)
	defer server.Close()
	ctx := context.Background()

	provider, version, err := detect(ctx, server.URL, server.Client(), "v5token")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if _, ok := provider.(*v5Client); !ok {
		t.Fatalf("expected v5 client, got %T", provider)
	}
	if version != "v5.18.4" {
		t.Fatalf("version = %q", version)
	}

	stats, err := provider.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.QueriesToday != 1234 || stats.BlockedToday != 321 || !stats.Enabled {
		t.Fatalf("stats = %+v", stats)
	}

	queries, err := provider.Queries(ctx, dns.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 3 {
		t.Fatalf("queries = %d", len(queries))
	}
	// Newest first, statuses normalized.
	if queries[0].Status != dns.StatusCached || queries[1].Status != dns.StatusBlocked || queries[2].Status != dns.StatusAllowed {
		t.Fatalf("normalized statuses wrong: %+v", queries)
	}
	if queries[1].Reason != "gravity" {
		t.Fatalf("blocked reason = %q", queries[1].Reason)
	}

	blocked, err := provider.Queries(ctx, dns.QueryFilter{Status: dns.StatusBlocked})
	if err != nil || len(blocked) != 1 || blocked[0].Domain != "ads.example.com" {
		t.Fatalf("status filter: %v %+v", err, blocked)
	}

	if err := provider.AddListEntry(ctx, dns.ListEntry{Type: dns.ListAllow, Domain: "good.example.org"}); err != nil {
		t.Fatal(err)
	}
	entries, err := provider.Lists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var foundAllow, foundDeny bool
	for _, entry := range entries {
		if entry.Type == dns.ListAllow && entry.Domain == "good.example.org" {
			foundAllow = true
		}
		if entry.Type == dns.ListDeny && entry.Domain == "ads.example.com" {
			foundDeny = true
		}
	}
	if !foundAllow || !foundDeny {
		t.Fatalf("lists missing entries: %+v", entries)
	}
	if err := provider.RemoveListEntry(ctx, dns.ListEntry{Type: dns.ListAllow, Domain: "good.example.org"}); err != nil {
		t.Fatal(err)
	}

	if err := provider.SetBlocking(ctx, false, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	clients, err := provider.Clients(ctx)
	if err != nil || len(clients) != 2 {
		t.Fatalf("clients: %v %+v", err, clients)
	}
	if err := provider.UpsertGroup(ctx, dns.Group{Name: "x"}); err != dns.ErrUnsupported {
		t.Fatalf("group upsert should be unsupported on v5, got %v", err)
	}
}

func TestV5BadToken(t *testing.T) {
	server := v5Fixture(t)
	defer server.Close()
	v5 := &v5Client{base: server.URL, token: "wrong", http: server.Client()}
	if _, err := v5.Stats(context.Background()); err == nil {
		t.Fatal("expected auth error with wrong token")
	}
}
