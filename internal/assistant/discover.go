package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// LM Studio serves the OpenAI dialect on :1234 with an unauthenticated
// /v1/models. Discovery sweeps the host's directly-attached /24 networks
// (plus loopback) for that signature — small nets only, so a scan stays
// a two-second courtesy, not a portscan.

const lmStudioPort = 1234

// FoundInstance is one discovered runtime. RequiresKey marks a server
// that answered the probe with 401/403 — present, but locked.
type FoundInstance struct {
	BaseURL     string   `json:"baseUrl"`
	Models      []string `json:"models"`
	RequiresKey bool     `json:"requiresKey"`
}

// DiscoverLMStudio probes candidate hosts concurrently and returns every
// responder that speaks /v1/models.
func DiscoverLMStudio(ctx context.Context) []FoundInstance {
	return discoverLMStudio(ctx, candidateHosts(), lmStudioPort)
}

func discoverLMStudio(ctx context.Context, hosts []string, port int) []FoundInstance {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	sem := make(chan struct{}, 64)
	var mu sync.Mutex
	var found []FoundInstance
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if inst, ok := probeLMStudio(ctx, net.JoinHostPort(host, fmt.Sprint(port))); ok {
				mu.Lock()
				found = append(found, inst)
				mu.Unlock()
			}
		}(host)
	}
	wg.Wait()
	sort.Slice(found, func(i, j int) bool { return found[i].BaseURL < found[j].BaseURL })
	return found
}

// probeLMStudio: fast TCP knock, then the models list.
func probeLMStudio(ctx context.Context, addr string) (FoundInstance, bool) {
	dialer := net.Dialer{Timeout: 350 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return FoundInstance{}, false
	}
	_ = conn.Close()

	reqCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/v1/models", nil)
	if err != nil {
		return FoundInstance{}, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return FoundInstance{}, false
	}
	defer resp.Body.Close()
	// A 401/403 is still LM Studio's signature — running, but with its
	// API-key requirement on. Hiding it made the scan look broken.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return FoundInstance{BaseURL: "http://" + addr + "/v1", Models: []string{}, RequiresKey: true}, true
	}
	if resp.StatusCode != http.StatusOK {
		return FoundInstance{}, false
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return FoundInstance{}, false
	}
	inst := FoundInstance{BaseURL: "http://" + addr + "/v1", Models: []string{}}
	for _, m := range parsed.Data {
		if m.ID != "" {
			inst.Models = append(inst.Models, m.ID)
		}
	}
	return inst, true
}

// candidateHosts enumerates loopback plus every address in the host's
// attached IPv4 networks of /24 or smaller. Bigger networks are skipped —
// sweeping a /16 is not a favor to anyone.
func candidateHosts() []string {
	seen := map[string]bool{"127.0.0.1": true}
	hosts := []string{"127.0.0.1"}
	ifaces, err := net.Interfaces()
	if err != nil {
		return hosts
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits != 32 || ones < 24 {
				continue
			}
			network := ipnet.IP.Mask(ipnet.Mask).To4()
			size := 1 << (bits - ones)
			for i := 1; i < size-1; i++ {
				ip := make(net.IP, 4)
				copy(ip, network)
				ip[3] = network[3] + byte(i)
				s := ip.String()
				if !seen[s] {
					seen[s] = true
					hosts = append(hosts, s)
				}
			}
		}
	}
	return hosts
}

// VerifyOpenAIEndpoint checks a user-entered base URL by asking it for
// its model list, and answers with diagnosis-quality messages: the
// difference between "nothing is listening" and "listening, but the URL
// is missing /v1" decides what the user fixes next.
func VerifyOpenAIEndpoint(ctx context.Context, baseURL, apiKey string) (models []string, err error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("the URL must start with http:// or https://")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("that is not a valid URL")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nothing answered at %s — is the server running and reachable from the HomeHub host?", base)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		if !strings.HasSuffix(base, "/v1") {
			return nil, fmt.Errorf("the server answered, but %s/models does not exist — the URL usually needs to end in /v1", base)
		}
		return nil, fmt.Errorf("the server answered, but it does not expose an OpenAI-compatible /models endpoint")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if apiKey != "" {
			return nil, fmt.Errorf("the server rejected the API key — check it matches the one configured on the server")
		}
		return nil, fmt.Errorf("the server requires an API key — enter it in the API key field below (in LM Studio this is configured on the Developer tab)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&parsed) != nil {
		return nil, fmt.Errorf("the server answered, but not with an OpenAI-compatible model list")
	}
	models = []string{}
	for _, m := range parsed.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}
