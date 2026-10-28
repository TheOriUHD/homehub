package nettools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"homehub/internal/netguard"
)

// DNSRecord is one answer row.
type DNSRecord struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   string `json:"ttl,omitempty"` // net.Resolver hides TTLs; left blank
}

type DNSResult struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Resolver string      `json:"resolver"`
	Server   string      `json:"server,omitempty"`
	QueryMs  float64     `json:"queryMs"`
	Records  []DNSRecord `json:"records"`
	Error    string      `json:"error,omitempty"`
}

// resolverFor builds a *net.Resolver that queries a specific server, or the
// system resolver when server is "". A user-named server is dialed through
// the egress guard (the system resolver, often 127.0.0.53, is HomeHub's own
// configuration and stays usable).
func resolverFor(server string) *net.Resolver {
	if server == "" {
		return net.DefaultResolver
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return netguard.Dialer(5*time.Second).DialContext(ctx, network, server)
		},
	}
}

// Lookup runs one DNS query of the given record type against the chosen
// server (host:port, or "" for the system resolver).
func Lookup(ctx context.Context, name, rrType, server string) DNSResult {
	res := DNSResult{Name: name, Type: strings.ToUpper(rrType), Server: server}
	r := resolverFor(server)
	qctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	start := time.Now()

	var err error
	switch res.Type {
	case "A", "AAAA":
		var ips []net.IPAddr
		ips, err = r.LookupIPAddr(qctx, name)
		for _, ip := range ips {
			is4 := ip.IP.To4() != nil
			if (res.Type == "A") == is4 {
				res.Records = append(res.Records, DNSRecord{Type: res.Type, Value: ip.IP.String()})
			}
		}
	case "CNAME":
		var cname string
		cname, err = r.LookupCNAME(qctx, name)
		// Go's LookupCNAME returns the name's own canonical FQDN (with no error)
		// when the host has address records but no CNAME. Only report a real
		// CNAME — one whose canonical target differs from the queried name.
		canon := strings.TrimSuffix(cname, ".")
		if canon != "" && !strings.EqualFold(canon, strings.TrimSuffix(name, ".")) {
			res.Records = append(res.Records, DNSRecord{Type: "CNAME", Value: canon})
		}
	case "TXT":
		var txts []string
		txts, err = r.LookupTXT(qctx, name)
		for _, t := range txts {
			res.Records = append(res.Records, DNSRecord{Type: "TXT", Value: t})
		}
	case "MX":
		var mxs []*net.MX
		mxs, err = r.LookupMX(qctx, name)
		sort.SliceStable(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
		for _, m := range mxs {
			res.Records = append(res.Records, DNSRecord{Type: "MX", Value: fmt.Sprintf("%d %s", m.Pref, strings.TrimSuffix(m.Host, "."))})
		}
	case "NS":
		var nss []*net.NS
		nss, err = r.LookupNS(qctx, name)
		for _, n := range nss {
			res.Records = append(res.Records, DNSRecord{Type: "NS", Value: strings.TrimSuffix(n.Host, ".")})
		}
	case "PTR":
		var names []string
		names, err = r.LookupAddr(qctx, name)
		for _, n := range names {
			res.Records = append(res.Records, DNSRecord{Type: "PTR", Value: strings.TrimSuffix(n, ".")})
		}
	default:
		err = fmt.Errorf("unsupported record type %q", res.Type)
	}

	res.QueryMs = float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		msg := err.Error()
		var blocked *netguard.ErrBlocked
		if errors.As(err, &blocked) {
			msg = blocked.Error()
		} else if dnsErr, ok := err.(*net.DNSError); ok {
			if dnsErr.IsNotFound {
				msg = "no records found"
			} else {
				msg = dnsErr.Err
			}
		}
		res.Error = msg
	}
	return res
}
