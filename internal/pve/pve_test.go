package pve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		BaseURL: srv.URL,
		TokenID: "root@pam!homehub",
		Secret:  "s3cret",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestAuthHeader(t *testing.T) {
	var gotAuth string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"version":"8.2.4","release":"8.2","repoid":"abc123"}}`))
	}))
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	const want = "PVEAPIToken=root@pam!homehub=s3cret"
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if v.Version != "8.2.4" || v.Release != "8.2" || v.RepoID != "abc123" {
		t.Errorf("VersionInfo = %+v", v)
	}
}

func TestErrorMapping(t *testing.T) {
	t.Run("errors map", func(t *testing.T) {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"data":null,"errors":{"vmid":"invalid format - unable to parse"}}`))
		}))
		_, err := c.ClusterResources(context.Background())
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("err = %v (%T), want *pve.Error", err, err)
		}
		if apiErr.Status != http.StatusInternalServerError {
			t.Errorf("Status = %d, want 500", apiErr.Status)
		}
		if !strings.Contains(apiErr.Message, "500 internal server error") {
			t.Errorf("Message = %q, want it to contain %q", apiErr.Message, "500 internal server error")
		}
		if !strings.Contains(apiErr.Message, "vmid: invalid format - unable to parse") {
			t.Errorf("Message = %q, want it to contain the field error", apiErr.Message)
		}
	})

	t.Run("plain text body", func(t *testing.T) {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("authentication failure"))
		}))
		_, err := c.Version(context.Background())
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("err = %v (%T), want *pve.Error", err, err)
		}
		if apiErr.Status != http.StatusUnauthorized {
			t.Errorf("Status = %d, want 401", apiErr.Status)
		}
		if got, want := apiErr.Message, "401 unauthorized: authentication failure"; got != want {
			t.Errorf("Message = %q, want %q", got, want)
		}
	})
}

func TestFlexInt64Unmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want FlexInt64
	}{
		{`42`, 42},
		{`"42"`, 42},
		{`3.9`, 3},
		{`"3.9"`, 3},
		{`true`, 1},
		{`false`, 0},
		{`""`, 0},
		{`null`, 0},
	}
	for _, tc := range cases {
		var f FlexInt64
		if err := json.Unmarshal([]byte(tc.in), &f); err != nil {
			t.Errorf("FlexInt64 %s: %v", tc.in, err)
			continue
		}
		if f != tc.want {
			t.Errorf("FlexInt64 %s = %d, want %d", tc.in, f, tc.want)
		}
	}
	var f FlexInt64
	if err := json.Unmarshal([]byte(`"bogus"`), &f); err == nil {
		t.Error("FlexInt64 accepted non-numeric string")
	}
}

func TestFlexFloatUnmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want FlexFloat
	}{
		{`1.5`, 1.5},
		{`"1.5"`, 1.5},
		{`7`, 7},
		{`"7"`, 7},
		{`true`, 1},
		{`false`, 0},
		{`""`, 0},
		{`null`, 0},
	}
	for _, tc := range cases {
		var f FlexFloat
		if err := json.Unmarshal([]byte(tc.in), &f); err != nil {
			t.Errorf("FlexFloat %s: %v", tc.in, err)
			continue
		}
		if f != tc.want {
			t.Errorf("FlexFloat %s = %v, want %v", tc.in, f, tc.want)
		}
	}
}

func TestFlexBoolUnmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want FlexBool
	}{
		{`true`, true},
		{`false`, false},
		{`1`, true},
		{`0`, false},
		{`"1"`, true},
		{`"0"`, false},
		{`"true"`, true},
		{`"false"`, false},
		{`""`, false},
		{`null`, false},
	}
	for _, tc := range cases {
		var f FlexBool
		if err := json.Unmarshal([]byte(tc.in), &f); err != nil {
			t.Errorf("FlexBool %s: %v", tc.in, err)
			continue
		}
		if f != tc.want {
			t.Errorf("FlexBool %s = %v, want %v", tc.in, f, tc.want)
		}
	}
}

func TestSetGuestConfigForm(t *testing.T) {
	type call struct {
		method      string
		path        string
		contentType string
		form        url.Values
	}
	var calls []call
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		calls = append(calls, call{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.PostForm})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":null}`))
	}))
	ctx := context.Background()

	if err := c.SetGuestConfig(ctx, "pve1", "qemu", 100, map[string]string{"cores": "4", "onboot": "1"}); err != nil {
		t.Fatalf("SetGuestConfig qemu: %v", err)
	}
	if err := c.SetGuestConfig(ctx, "pve1", "lxc", 200, map[string]string{"memory": "2048"}); err != nil {
		t.Fatalf("SetGuestConfig lxc: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d requests, want 2", len(calls))
	}

	qemu := calls[0]
	if qemu.method != http.MethodPost {
		t.Errorf("qemu method = %s, want POST", qemu.method)
	}
	if qemu.path != "/api2/json/nodes/pve1/qemu/100/config" {
		t.Errorf("qemu path = %q", qemu.path)
	}
	if qemu.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("qemu Content-Type = %q", qemu.contentType)
	}
	if qemu.form.Get("cores") != "4" || qemu.form.Get("onboot") != "1" {
		t.Errorf("qemu form = %v", qemu.form)
	}

	lxc := calls[1]
	if lxc.method != http.MethodPut {
		t.Errorf("lxc method = %s, want PUT", lxc.method)
	}
	if lxc.path != "/api2/json/nodes/pve1/lxc/200/config" {
		t.Errorf("lxc path = %q", lxc.path)
	}
	if lxc.form.Get("memory") != "2048" {
		t.Errorf("lxc form = %v", lxc.form)
	}
}
