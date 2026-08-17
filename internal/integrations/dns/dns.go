// Package dns defines the normalized surface every DNS integration
// (Pi-hole v5/v6, AdGuard Home) presents, so the app renders one unified
// module across mixed fleets and the sync/compare view can diff instances of
// different types.
package dns

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported is returned for operations an instance's upstream version
// cannot perform; the UI hides those affordances via Capabilities.
var ErrUnsupported = errors.New("not supported by this DNS server version")

// Capabilities reports what a live instance can do.
type Capabilities struct {
	GroupCRUD   bool `json:"groupCrud"`
	ClientCRUD  bool `json:"clientCrud"`
	RegexLists  bool `json:"regexLists"`
	CustomRules bool `json:"customRules"`
	ListComment bool `json:"listComment"`
}

// Stats is the headline summary of one instance.
type Stats struct {
	QueriesToday   int64      `json:"queriesToday"`
	BlockedToday   int64      `json:"blockedToday"`
	BlockedPct     float64    `json:"blockedPct"`
	ClientsSeen    int64      `json:"clientsSeen"`
	DomainsOnLists int64      `json:"domainsOnLists"`
	Enabled        bool       `json:"enabled"`
	DisabledUntil  *time.Time `json:"disabledUntil,omitempty"`
	Version        string     `json:"version"`
}

// QueryStatus is the normalized verdict for one query.
type QueryStatus string

const (
	StatusAllowed QueryStatus = "allowed"
	StatusBlocked QueryStatus = "blocked"
	StatusCached  QueryStatus = "cached"
)

type QueryEntry struct {
	Time       time.Time   `json:"time"`
	Domain     string      `json:"domain"`
	Client     string      `json:"client"`
	ClientName string      `json:"clientName,omitempty"`
	Type       string      `json:"type"`
	Status     QueryStatus `json:"status"`
	// Reason carries the upstream-specific detail: blocklist name, upstream
	// server, filter rule, cache state.
	Reason string `json:"reason,omitempty"`
}

type QueryFilter struct {
	Search string // domain substring
	Client string
	Status QueryStatus // empty = all
	Limit  int
}

type ListType string

const (
	ListAllow      ListType = "allow"
	ListDeny       ListType = "deny"
	ListAllowRegex ListType = "allow_regex"
	ListDenyRegex  ListType = "deny_regex"
	// ListCustom is a raw rule in the upstream's own syntax (AdGuard rules
	// that don't map onto the four normalized types).
	ListCustom ListType = "custom"
)

type ListEntry struct {
	ID      string   `json:"id"`
	Type    ListType `json:"type"`
	Domain  string   `json:"domain"`
	Enabled bool     `json:"enabled"`
	Comment string   `json:"comment,omitempty"`
}

type Client struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Identifiers []string `json:"identifiers"`
	GroupIDs    []string `json:"groupIds"`
}

type Group struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type BlockingState struct {
	Enabled bool       `json:"enabled"`
	Until   *time.Time `json:"until,omitempty"`
}

// Provider is the normalized operation set. Connections of both driver
// types implement it; unsupported operations return ErrUnsupported.
type Provider interface {
	Capabilities() Capabilities
	Stats(ctx context.Context) (*Stats, error)
	Queries(ctx context.Context, filter QueryFilter) ([]QueryEntry, error)
	Lists(ctx context.Context) ([]ListEntry, error)
	AddListEntry(ctx context.Context, entry ListEntry) error
	RemoveListEntry(ctx context.Context, entry ListEntry) error
	Blocking(ctx context.Context) (*BlockingState, error)
	SetBlocking(ctx context.Context, enabled bool, duration time.Duration) error
	Clients(ctx context.Context) ([]Client, error)
	Groups(ctx context.Context) ([]Group, error)
	UpsertClient(ctx context.Context, client Client) error
	DeleteClient(ctx context.Context, id string) error
	UpsertGroup(ctx context.Context, group Group) error
	DeleteGroup(ctx context.Context, id string) error
}
