package pvesim

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSimulator(t *testing.T) {
	s := New(Options{TaskDelay: 150 * time.Millisecond})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := ts.Client()

	do := func(method, path string, form url.Values) (int, json.RawMessage) {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequest(method, ts.URL+"/api2/json"+path, body)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Authorization", "PVEAPIToken="+s.Token())
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("%s %s: decode: %v", method, path, err)
		}
		return resp.StatusCode, env.Data
	}

	// Requests without the token are rejected.
	resp, err := client.Get(ts.URL + "/api2/json/version")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", resp.StatusCode)
	}

	// cluster/resources lists the seeded guests.
	code, data := do("GET", "/cluster/resources?type=vm", nil)
	if code != http.StatusOK {
		t.Fatalf("cluster/resources status = %d", code)
	}
	type res struct {
		Type     string `json:"type"`
		VMID     int    `json:"vmid"`
		Name     string `json:"name"`
		Node     string `json:"node"`
		Status   string `json:"status"`
		Template int    `json:"template"`
	}
	var guests []res
	if err := json.Unmarshal(data, &guests); err != nil {
		t.Fatalf("decode resources: %v", err)
	}
	if len(guests) != 9 {
		t.Fatalf("got %d guests, want 9", len(guests))
	}
	var target, lxc *res
	for i, g := range guests {
		if target == nil && g.Type == "qemu" && g.Status == "stopped" && g.Template == 0 {
			target = &guests[i]
		}
		if lxc == nil && g.Type == "lxc" {
			lxc = &guests[i]
		}
	}
	if target == nil || lxc == nil {
		t.Fatalf("seed data missing a stopped qemu guest or an lxc guest: %+v", guests)
	}

	// Starting a stopped VM returns a UPID whose task completes OK, after
	// which the guest reports running.
	code, data = do("POST", fmt.Sprintf("/nodes/%s/qemu/%d/status/start", target.Node, target.VMID), url.Values{})
	if code != http.StatusOK {
		t.Fatalf("start status = %d (%s)", code, data)
	}
	var upid string
	if err := json.Unmarshal(data, &upid); err != nil || !strings.HasPrefix(upid, "UPID:") {
		t.Fatalf("start returned %q, want UPID", data)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, data = do("GET", fmt.Sprintf("/nodes/%s/tasks/%s/status", target.Node, upid), nil)
		var st struct {
			Status     string `json:"status"`
			Exitstatus string `json:"exitstatus"`
		}
		if err := json.Unmarshal(data, &st); err != nil {
			t.Fatalf("decode task status: %v", err)
		}
		if st.Status == "stopped" {
			if st.Exitstatus != "OK" {
				t.Fatalf("task exitstatus = %q, want OK", st.Exitstatus)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not finish within 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, data = do("GET", fmt.Sprintf("/nodes/%s/qemu/%d/status/current", target.Node, target.VMID), nil)
	var cur struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &cur); err != nil {
		t.Fatalf("decode status/current: %v", err)
	}
	if cur.Status != "running" {
		t.Fatalf("guest status after start = %q, want running", cur.Status)
	}

	// Config changes round-trip.
	code, _ = do("PUT", fmt.Sprintf("/nodes/%s/lxc/%d/config", lxc.Node, lxc.VMID),
		url.Values{"memory": {"1024"}})
	if code != http.StatusOK {
		t.Fatalf("config PUT status = %d", code)
	}
	_, data = do("GET", fmt.Sprintf("/nodes/%s/lxc/%d/config", lxc.Node, lxc.VMID), nil)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if fmt.Sprint(cfg["memory"]) != "1024" {
		t.Fatalf("config memory = %v, want 1024", cfg["memory"])
	}
}
