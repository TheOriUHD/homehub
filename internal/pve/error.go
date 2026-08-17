package pve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Error is a non-2xx response from the Proxmox API.
type Error struct {
	// Status is the HTTP status code.
	Status int
	// Message is a human-readable description, e.g.
	// "401 unauthorized: authentication failure".
	Message string
}

func (e *Error) Error() string { return e.Message }

func newError(status int, statusLine string, body []byte) *Error {
	detail := errorDetail(body)
	if detail == "" {
		// Some PVE errors carry the message only in the HTTP reason phrase
		// ("500 unable to find VM 123"); use it unless it is the stock text.
		reason := strings.TrimSpace(strings.TrimPrefix(statusLine, strconv.Itoa(status)))
		if !strings.EqualFold(reason, http.StatusText(status)) {
			detail = reason
		}
	}
	msg := strconv.Itoa(status)
	if text := http.StatusText(status); text != "" {
		msg += " " + strings.ToLower(text)
	}
	if detail != "" {
		msg += ": " + detail
	}
	return &Error{Status: status, Message: msg}
}

// errorDetail extracts a human-useful message from an error body, which may
// be {"errors": {field: msg, ...}}, {"message": "..."}, or plain text.
func errorDetail(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	var payload struct {
		Errors  map[string]any `json:"errors"`
		Message string         `json:"message"`
	}
	if json.Unmarshal(trimmed, &payload) == nil && (len(payload.Errors) > 0 || payload.Message != "") {
		parts := make([]string, 0, len(payload.Errors)+1)
		if m := strings.TrimSpace(payload.Message); m != "" {
			parts = append(parts, m)
		}
		keys := make([]string, 0, len(payload.Errors))
		for k := range payload.Errors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, k+": "+strings.TrimSpace(fmt.Sprint(payload.Errors[k])))
		}
		return strings.Join(parts, "; ")
	}
	var s string
	if json.Unmarshal(trimmed, &s) == nil {
		return strings.TrimSpace(s)
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return "" // JSON without a recognizable message — don't dump it raw
	}
	return string(trimmed)
}
