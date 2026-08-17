// Package caddyfile splits a Caddyfile into blocks without ever parsing it.
//
// The invariant (from references/caddy/caddy.md §7): for any file, splitting
// and re-rendering WITHOUT edits produces byte-identical text — and therefore
// byte-identical adapted JSON. Every block keeps its exact original text in
// Raw, and Render is nothing but their concatenation. That is what makes
// "HomeHub owns the file" safe to say: nothing the model does not understand
// can be dropped, because nothing is ever regenerated from a model.
//
// A site block is promoted to a structured, editable entry only when it is a
// simple reverse proxy the model covers completely; everything else is
// preserved verbatim as a raw block with a stated reason.
package caddyfile

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type BlockKind string

const (
	KindGlobal BlockKind = "global" // the leading address-less { … }
	KindEntry  BlockKind = "entry"  // structured-editable simple reverse proxy
	KindRaw    BlockKind = "raw"    // preserved verbatim, edited as text
	KindGap    BlockKind = "gap"    // comments and blank space between blocks
)

type Block struct {
	Kind BlockKind
	Raw  string
	// Entry is set for KindEntry.
	Entry *Entry
	// Addresses and Reason are set for KindRaw site blocks.
	Addresses []string
	Reason    string
}

// Entry is a simple reverse-proxy site the editor fully models.
type Entry struct {
	Domains  []string // first is primary, the rest extra addresses
	Upstream Upstream
	// DNSChallenge args are kept verbatim — they are usually a placeholder
	// like {env.CLOUDFLARE_API_TOKEN}, and re-quoting or expanding one would
	// either break it or write a secret into the file.
	DNSChallenge     *DNSChallenge
	Encode           string // arguments of `encode`, empty when absent
	HeaderUp         []HeaderUp
	InsecureUpstream bool
	// UnknownDirectives are bare single-line directives the model does not
	// know, re-emitted verbatim so editing a port never drops them.
	UnknownDirectives []string
}

type Upstream struct {
	// Scheme is written exactly as found — "http://x" and "x" adapt
	// identically, but dropping an explicit scheme would silently edit a
	// line nobody asked to change.
	Scheme string
	Host   string
	Port   string // empty when absent
}

type DNSChallenge struct {
	Provider string
	Args     string // verbatim remainder, may be empty
}

type HeaderUp struct {
	Name  string
	Value string
}

// Document is a split file. When Refusal is non-empty, Blocks is empty and
// NOTHING is manageable — half a model of a file that routes production
// traffic is worse than none.
type Document struct {
	Blocks  []Block
	Refusal string
}

// Render reassembles the file. This is the whole function on purpose.
func Render(doc Document) string {
	var b strings.Builder
	for _, blk := range doc.Blocks {
		b.WriteString(blk.Raw)
	}
	return b.String()
}

// DefaultHeaderUp lists the three headers Caddy already sets on proxied
// requests; a block that only restates them is redundant.
var DefaultHeaderUp = []HeaderUp{
	{Name: "Host", Value: "{host}"},
	{Name: "X-Forwarded-For", Value: "{remote_host}"},
	{Name: "X-Forwarded-Proto", Value: "{scheme}"},
}

// HeadersAreDefaults reports whether the given header_up set only restates
// what Caddy sets anyway.
func HeadersAreDefaults(headers []HeaderUp) bool {
	if len(headers) == 0 {
		return false
	}
	for _, h := range headers {
		found := false
		for _, d := range DefaultHeaderUp {
			if h == d {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ---- scanning ---------------------------------------------------------------

// stripComment removes a trailing #-comment from a line, respecting double
// quotes (with backslash escapes) and backticks.
func stripComment(line string) string {
	inQuote, inBacktick, escaped := false, false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case escaped:
			escaped = false
		case inQuote:
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inQuote = false
			}
		case inBacktick:
			if c == '`' {
				inBacktick = false
			}
		case c == '"':
			inQuote = true
		case c == '`':
			inBacktick = true
		case c == '#':
			return line[:i]
		}
	}
	return line
}

// opensBlock reports whether a line opens a block: after comment stripping,
// `{` must be the last thing on the line. Caddy requires this, so the
// distinction is exact rather than a guess — without it, `redir
// https://x{uri}` reads as a nested block.
func opensBlock(line string) bool {
	s := strings.TrimRight(stripComment(line), " \t\r\n")
	return strings.HasSuffix(s, "{")
}

// closesBlock reports whether a line is a bare closing brace.
func closesBlock(line string) bool {
	return strings.TrimSpace(stripComment(line)) == "}"
}

// isGapLine reports whether a line is blank or comment-only.
func isGapLine(line string) bool {
	return strings.TrimSpace(stripComment(line)) == ""
}

// splitLines cuts text into lines, each keeping its trailing newline, so
// concatenating them reproduces the input byte-for-byte.
func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

const (
	RefusalShorthand = "This Caddyfile uses the single-site shorthand (an address with no { }). HomeHub will not edit it, because splitting that form reliably needs a real Caddyfile parser. Wrap the site in braces and it can."
	RefusalBraces    = "The braces in this Caddyfile do not balance, so it cannot be read safely."
)

// Split cuts the file at top-level brace boundaries. It never interprets
// anything below the block level except to recognise simple entries.
func Split(text string) Document {
	lines := splitLines(text)

	// Balance check first: count block opens/closes by the same rules the
	// scanner uses, so the two can never disagree.
	depth := 0
	for _, line := range lines {
		if opensBlock(line) {
			depth++
		} else if closesBlock(line) {
			depth--
			if depth < 0 {
				return Document{Refusal: RefusalBraces}
			}
		}
	}
	if depth != 0 {
		return Document{Refusal: RefusalBraces}
	}

	var blocks []Block
	var gap strings.Builder
	sawBlock := false

	flushGap := func() {
		if gap.Len() > 0 {
			blocks = append(blocks, Block{Kind: KindGap, Raw: gap.String()})
			gap.Reset()
		}
	}

	i := 0
	for i < len(lines) {
		line := lines[i]
		switch {
		case isGapLine(line):
			gap.WriteString(line)
			i++
		case opensBlock(line):
			flushGap()
			var raw strings.Builder
			raw.WriteString(line)
			depth := 1
			j := i + 1
			for j < len(lines) && depth > 0 {
				if opensBlock(lines[j]) {
					depth++
				} else if closesBlock(lines[j]) {
					depth--
				}
				raw.WriteString(lines[j])
				j++
			}
			header := strings.TrimSpace(stripComment(line))
			header = strings.TrimSpace(strings.TrimSuffix(header, "{"))
			blk := buildBlock(header, raw.String(), !sawBlock)
			blocks = append(blocks, blk)
			sawBlock = true
			i = j
		default:
			// Top-level content that is not a gap and opens no block: the
			// single-site shorthand. Refuse the whole file.
			return Document{Refusal: RefusalShorthand}
		}
	}
	flushGap()
	return Document{Blocks: blocks}
}

// buildBlock classifies one top-level block.
func buildBlock(header, raw string, first bool) Block {
	if header == "" {
		if first {
			return Block{Kind: KindGlobal, Raw: raw}
		}
		return Block{Kind: KindRaw, Raw: raw, Reason: "address-less block that is not the leading global options"}
	}
	addresses := splitAddresses(header)
	entry, reason := recognise(addresses, raw)
	if entry != nil {
		return Block{Kind: KindEntry, Raw: raw, Entry: entry, Addresses: addresses}
	}
	return Block{Kind: KindRaw, Raw: raw, Addresses: addresses, Reason: reason}
}

func splitAddresses(header string) []string {
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---- recognising a simple reverse proxy ------------------------------------

// recognise promotes a site block to an entry only if it contains exactly one
// reverse_proxy with exactly one argument, and every other directive is
// either modelled or a plain single-line directive. The reason for refusing
// is surfaced in the UI — "this block is advanced" is unhelpful.
func recognise(addresses []string, raw string) (*Entry, string) {
	lines := splitLines(raw)
	if len(lines) < 2 {
		return nil, "no reverse_proxy"
	}
	body := lines[1 : len(lines)-1] // between the header line and the closing }

	entry := &Entry{Domains: addresses}
	proxyCount := 0

	i := 0
	for i < len(body) {
		line := body[i]
		if isGapLine(line) {
			i++
			continue
		}
		content := strings.TrimSpace(stripComment(line))
		fields := strings.Fields(strings.TrimSuffix(content, "{"))
		if len(fields) == 0 {
			i++
			continue
		}
		directive := fields[0]

		if opensBlock(line) {
			// Collect the sub-block.
			depth := 1
			j := i + 1
			var sub []string
			for j < len(body) && depth > 0 {
				if opensBlock(body[j]) {
					depth++
				} else if closesBlock(body[j]) {
					depth--
					if depth == 0 {
						break
					}
				}
				sub = append(sub, body[j])
				j++
			}
			switch directive {
			case "reverse_proxy":
				proxyCount++
				if proxyCount > 1 {
					return nil, "more than one reverse_proxy"
				}
				if reason := parseReverseProxyArgs(entry, fields[1:]); reason != "" {
					return nil, reason
				}
				if reason := parseReverseProxyBlock(entry, sub); reason != "" {
					return nil, reason
				}
			case "tls":
				if len(fields) > 1 {
					return nil, "tls block does more than a DNS challenge"
				}
				if reason := parseTLSBlock(entry, sub); reason != "" {
					return nil, reason
				}
			default:
				return nil, "contains a nested block"
			}
			i = j + 1
			continue
		}

		// Bare single-line directive.
		switch directive {
		case "reverse_proxy":
			proxyCount++
			if proxyCount > 1 {
				return nil, "more than one reverse_proxy"
			}
			if reason := parseReverseProxyArgs(entry, fields[1:]); reason != "" {
				return nil, reason
			}
		case "encode":
			entry.Encode = strings.TrimSpace(strings.TrimPrefix(content, "encode"))
		default:
			entry.UnknownDirectives = append(entry.UnknownDirectives, content)
		}
		i++
	}

	if proxyCount == 0 {
		return nil, "no reverse_proxy"
	}
	return entry, ""
}

func parseReverseProxyArgs(entry *Entry, args []string) string {
	if len(args) != 1 {
		return "reverse_proxy has matchers or several upstreams"
	}
	arg := args[0]
	if strings.HasPrefix(arg, "@") || strings.HasPrefix(arg, "/") {
		return "reverse_proxy has matchers or several upstreams"
	}
	up, err := ParseUpstream(arg)
	if err != nil {
		return "reverse_proxy has matchers or several upstreams"
	}
	entry.Upstream = up
	return ""
}

// parseReverseProxyBlock handles the body of `reverse_proxy … { … }`.
func parseReverseProxyBlock(entry *Entry, sub []string) string {
	i := 0
	for i < len(sub) {
		line := sub[i]
		if isGapLine(line) {
			i++
			continue
		}
		content := strings.TrimSpace(stripComment(line))
		fields := strings.Fields(strings.TrimSuffix(content, "{"))
		if len(fields) == 0 {
			i++
			continue
		}
		if opensBlock(line) {
			// Only `transport http { tls_insecure_skip_verify }` is modelled.
			if len(fields) != 2 || fields[0] != "transport" || fields[1] != "http" {
				return "unrecognised transport"
			}
			depth := 1
			j := i + 1
			for j < len(sub) && depth > 0 {
				if opensBlock(sub[j]) {
					depth++
				} else if closesBlock(sub[j]) {
					depth--
					if depth == 0 {
						break
					}
				}
				inner := strings.TrimSpace(stripComment(sub[j]))
				if inner != "" && inner != "tls_insecure_skip_verify" {
					return "unrecognised transport"
				}
				j++
			}
			entry.InsecureUpstream = true
			i = j + 1
			continue
		}
		switch fields[0] {
		case "header_up":
			if len(fields) < 3 {
				return "unrecognised reverse_proxy option: header_up"
			}
			entry.HeaderUp = append(entry.HeaderUp, HeaderUp{
				Name:  fields[1],
				Value: strings.Join(fields[2:], " "),
			})
		default:
			return "unrecognised reverse_proxy option: " + fields[0]
		}
		i++
	}
	return ""
}

// parseTLSBlock accepts only a single `dns <provider> <args…>` line.
func parseTLSBlock(entry *Entry, sub []string) string {
	for _, line := range sub {
		if isGapLine(line) {
			continue
		}
		content := strings.TrimSpace(stripComment(line))
		fields := strings.Fields(content)
		if len(fields) < 2 || fields[0] != "dns" {
			return "tls block does more than a DNS challenge"
		}
		if entry.DNSChallenge != nil {
			return "tls block does more than a DNS challenge"
		}
		args := ""
		if len(fields) > 2 {
			args = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(content, "dns"), " "+fields[1]))
			args = strings.TrimSpace(args)
		}
		entry.DNSChallenge = &DNSChallenge{Provider: fields[1], Args: args}
	}
	if entry.DNSChallenge == nil {
		return "tls block does more than a DNS challenge"
	}
	return ""
}

// ParseUpstream parses `[scheme://]host[:port]`, handling bracketed IPv6.
func ParseUpstream(s string) (Upstream, error) {
	var up Upstream
	rest := s
	if idx := strings.Index(rest, "://"); idx >= 0 {
		up.Scheme = rest[:idx]
		rest = rest[idx+3:]
	}
	if rest == "" {
		return up, fmt.Errorf("empty upstream")
	}
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return up, fmt.Errorf("unclosed bracket in %q", s)
		}
		up.Host = rest[1:end]
		rest = rest[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return up, fmt.Errorf("junk after IPv6 address in %q", s)
			}
			up.Port = rest[1:]
		}
		return up, nil
	}
	if idx := strings.LastIndex(rest, ":"); idx >= 0 && !strings.Contains(rest, "]") && strings.Count(rest, ":") == 1 {
		up.Host, up.Port = rest[:idx], rest[idx+1:]
	} else if strings.Count(rest, ":") > 1 {
		// Unbracketed IPv6 — treat the whole thing as the host.
		up.Host = rest
	} else {
		up.Host = rest
	}
	if up.Host == "" {
		return up, fmt.Errorf("empty host in %q", s)
	}
	return up, nil
}

// ---- rendering an entry -----------------------------------------------------

// RenderEntry produces the canonical shape used by the file (§7.6).
func RenderEntry(e Entry) string {
	var b strings.Builder
	b.WriteString(strings.Join(e.Domains, ", "))
	b.WriteString(" {\n")

	if e.DNSChallenge != nil {
		b.WriteString("\ttls {\n\t\tdns " + e.DNSChallenge.Provider)
		if e.DNSChallenge.Args != "" {
			b.WriteString(" " + e.DNSChallenge.Args)
		}
		b.WriteString("\n\t}\n\n")
	}
	if e.Encode != "" {
		b.WriteString("\tencode " + e.Encode + "\n")
	}
	for _, d := range e.UnknownDirectives {
		b.WriteString("\t" + d + "\n")
	}

	b.WriteString("\treverse_proxy " + e.Upstream.String())
	needsBlock := e.InsecureUpstream || len(e.HeaderUp) > 0
	if needsBlock {
		b.WriteString(" {\n")
		if e.InsecureUpstream {
			b.WriteString("\t\ttransport http {\n\t\t\ttls_insecure_skip_verify\n\t\t}\n")
		}
		for _, h := range e.HeaderUp {
			b.WriteString("\t\theader_up " + h.Name + " " + h.Value + "\n")
		}
		b.WriteString("\t}\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// String renders the upstream exactly: scheme preserved, IPv6 bracketed.
func (u Upstream) String() string {
	host := u.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	out := host
	if u.Port != "" {
		out += ":" + u.Port
	}
	if u.Scheme != "" {
		out = u.Scheme + "://" + out
	}
	return out
}

// ---- validation (§8.4) ------------------------------------------------------

var domainPattern = regexp.MustCompile(`^[a-z0-9*]([a-z0-9.-]*[a-z0-9])?$`)

// NormalizeDomain lowercases, strips a trailing dot, and rejects what Caddy
// would reject plus a few things Caddy would accept but nobody means.
func NormalizeDomain(raw string) (string, error) {
	d := strings.TrimSpace(strings.ToLower(raw))
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return "", fmt.Errorf("a domain is required")
	}
	if idx := strings.Index(d, "://"); idx >= 0 {
		return "", fmt.Errorf("just the domain, without the scheme: %s", d[idx+3:])
	}
	if strings.Contains(d, "/") {
		return "", fmt.Errorf("just the domain, without a path: %s", d)
	}
	if !domainPattern.MatchString(d) {
		return "", fmt.Errorf("%q is not a valid domain", d)
	}
	return d, nil
}

// ValidateUpstreamHost rejects URLs pasted into an address field.
func ValidateUpstreamHost(host string) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("an upstream host is required")
	}
	if strings.ContainsAny(host, " \t") {
		return fmt.Errorf("the upstream host must not contain whitespace")
	}
	if strings.Contains(host, "/") {
		return fmt.Errorf("the target is an address and port, not a URL")
	}
	return nil
}

// ValidatePort accepts 1–65535, or an empty string for "no port".
func ValidatePort(port string) error {
	if port == "" {
		return nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("the port must be between 1 and 65535")
	}
	return nil
}
