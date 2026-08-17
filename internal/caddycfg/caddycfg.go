// Package caddycfg reads Caddy's *running* configuration — the JSON the
// admin API serves, or the projection a caddy-reader helper returns (the two
// have the same nested shape by contract, references/caddy/caddy.md §5.3).
//
// Caddyfile text is deliberately unsupported here: its grammar has snippets,
// imports, environment substitution and matcher shorthands, and a parser for
// it would be a half-correct reimplementation of Caddy that silently
// disagrees with the real one. The JSON is what Caddy actually runs.
package caddycfg

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Upstream is one (domain → dial address) fact from the running config.
type Upstream struct {
	Domain string `json:"domain"`
	Host   string `json:"host"`
	Port   string `json:"port"`
	Dial   string `json:"dial"`
}

// Unresolvable is a domain that IS proxied, somewhere unknowable — a
// different fact from "not proxied", so it carries its reason.
type Unresolvable struct {
	Domain string `json:"domain"`
	Reason string `json:"reason"`
}

type Result struct {
	Upstreams           []Upstream     `json:"upstreams"`
	Unresolvable        []Unresolvable `json:"unresolvable"`
	DomainsWithoutProxy []string       `json:"domainsWithoutProxy"`
	Warnings            []string       `json:"warnings"`
}

const maxRouteDepth = 25

// Parse walks an adapted/running config. Every field is treated as
// untrusted; whatever cannot be read is reported rather than fatal — a
// config with one exotic handler should still yield the other nine domains.
func Parse(raw []byte) (*Result, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("this is not JSON — paste the output of `caddy adapt`, or a GET of the admin API's /config/")
	}
	return ParseConfig(config), nil
}

func ParseConfig(config map[string]any) *Result {
	res := &Result{
		Upstreams:           []Upstream{},
		Unresolvable:        []Unresolvable{},
		DomainsWithoutProxy: []string{},
		Warnings:            []string{},
	}

	servers := dig(config, "apps", "http", "servers")
	serversMap, ok := servers.(map[string]any)
	if !ok {
		res.Warnings = append(res.Warnings, "the config has no apps.http.servers — nothing is being served")
		return res
	}

	seenUpstream := map[string]bool{}      // domain+"\x00"+dial
	seenUnresolvable := map[string]bool{}  // domain+"\x00"+reason
	proxiedDomains := map[string]bool{}    // any domain that hit a reverse_proxy
	servedDomains := map[string]bool{}     // any domain seen in a host matcher

	// servers is a map with arbitrary keys — `caddy adapt` happens to name
	// them srv0, srv1…, but a hand-written config names them anything.
	// Iterate deterministically for stable output.
	names := make([]string, 0, len(serversMap))
	for name := range serversMap {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		server, ok := serversMap[name].(map[string]any)
		if !ok {
			continue
		}
		routes, _ := server["routes"].([]any)
		walkRoutes(routes, nil, 0, res, seenUpstream, seenUnresolvable, proxiedDomains, servedDomains)
	}

	// Domains served by something that is not a reverse proxy — minus any
	// domain that WAS proxied somewhere else in the config.
	for domain := range servedDomains {
		if !proxiedDomains[domain] {
			res.DomainsWithoutProxy = append(res.DomainsWithoutProxy, domain)
		}
	}
	sort.Strings(res.DomainsWithoutProxy)
	sort.Slice(res.Upstreams, func(i, j int) bool { return res.Upstreams[i].Domain < res.Upstreams[j].Domain })
	sort.Slice(res.Unresolvable, func(i, j int) bool { return res.Unresolvable[i].Domain < res.Unresolvable[j].Domain })
	return res
}

// walkRoutes carries host matchers downward: an inner route with no host
// matcher inherits the outer one. Reading only the top level finds NOTHING
// in a config with more than one site.
func walkRoutes(routes []any, inheritedHosts []string, depth int,
	res *Result, seenUp, seenUn, proxied, served map[string]bool,
) {
	if depth > maxRouteDepth {
		res.Warnings = append(res.Warnings, fmt.Sprintf("route nesting exceeds %d levels — the config may reference itself; deeper routes were not read", maxRouteDepth))
		return
	}
	for _, r := range routes {
		route, ok := r.(map[string]any)
		if !ok {
			continue
		}
		hosts := inheritedHosts
		if matches, ok := route["match"].([]any); ok {
			for _, m := range matches {
				matcher, ok := m.(map[string]any)
				if !ok {
					continue
				}
				if hostList, ok := matcher["host"].([]any); ok {
					var found []string
					for _, h := range hostList {
						if s, ok := h.(string); ok && s != "" {
							found = append(found, strings.ToLower(s))
						}
					}
					if len(found) > 0 {
						hosts = found
					}
				}
			}
		}
		for _, domain := range hosts {
			served[domain] = true
		}

		handlers, _ := route["handle"].([]any)
		for _, h := range handlers {
			handler, ok := h.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := handler["handler"].(string)
			switch kind {
			case "subroute":
				sub, _ := handler["routes"].([]any)
				walkRoutes(sub, hosts, depth+1, res, seenUp, seenUn, proxied, served)
			case "reverse_proxy":
				if len(hosts) == 0 {
					// A reverse_proxy with no host matcher answers for any
					// domain — there is nothing to report from it.
					res.Warnings = append(res.Warnings, "a reverse_proxy has no host matcher — it answers for any domain and was skipped")
					continue
				}
				recordProxy(handler, hosts, res, seenUp, seenUn, proxied)
			}
		}
	}
}

func recordProxy(handler map[string]any, hosts []string,
	res *Result, seenUp, seenUn, proxied map[string]bool,
) {
	for _, domain := range hosts {
		proxied[domain] = true
	}

	addUnresolvable := func(reason string) {
		for _, domain := range hosts {
			key := domain + "\x00" + reason
			if !seenUn[key] {
				seenUn[key] = true
				res.Unresolvable = append(res.Unresolvable, Unresolvable{Domain: domain, Reason: reason})
			}
		}
	}

	if _, hasDynamic := handler["dynamic_upstreams"]; hasDynamic {
		addUnresolvable("upstreams are chosen dynamically at request time")
		return
	}
	upstreams, _ := handler["upstreams"].([]any)
	if len(upstreams) == 0 {
		addUnresolvable("the reverse proxy lists no upstream address")
		return
	}
	for _, u := range upstreams {
		up, ok := u.(map[string]any)
		if !ok {
			continue
		}
		dial, _ := up["dial"].(string)
		switch {
		case dial == "":
			addUnresolvable("empty upstream address")
		case strings.Contains(dial, "{"):
			addUnresolvable("address is a placeholder: " + dial)
		case strings.HasPrefix(dial, "unix/"):
			addUnresolvable("unix socket: " + dial)
		default:
			host, port := ParseDial(dial)
			if host == "" {
				addUnresolvable("unreadable upstream address: " + dial)
				continue
			}
			for _, domain := range hosts {
				key := domain + "\x00" + dial
				if !seenUp[key] {
					seenUp[key] = true
					res.Upstreams = append(res.Upstreams, Upstream{
						Domain: domain, Host: host, Port: port, Dial: dial,
					})
				}
			}
		}
	}
}

// ParseDial strips an optional scheme (`h2c://10.0.0.5:8080` is valid) and
// handles bracketed IPv6 — a naive split(':') turns `[::1]:8080` into
// nonsense rather than failing.
func ParseDial(dial string) (host, port string) {
	rest := dial
	if idx := strings.Index(rest, "://"); idx >= 0 {
		rest = rest[idx+3:]
	}
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return "", ""
		}
		host = rest[1:end]
		rest = rest[end+1:]
		if strings.HasPrefix(rest, ":") {
			port = rest[1:]
		}
		return host, port
	}
	if strings.Count(rest, ":") == 1 {
		idx := strings.Index(rest, ":")
		return rest[:idx], rest[idx+1:]
	}
	if strings.Count(rest, ":") > 1 {
		// Unbracketed IPv6, no port to separate.
		return rest, ""
	}
	return rest, ""
}

func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, key := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[key]
	}
	return cur
}
