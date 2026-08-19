package netscan

import (
	"bufio"
	_ "embed"
	"strings"
	"sync"
)

// ouiData is the full IEEE OUI registry (MA-L /24, MA-M /28, MA-S /36),
// compacted to "ASSIGNMENT<TAB>vendor" lines — see data/oui.tsv. Embedded so
// the lookup is offline and self-contained.
//
//go:embed data/oui.tsv
var ouiData string

var (
	ouiOnce sync.Once
	ouiMap  map[string]string
)

func loadOUI() {
	ouiMap = make(map[string]string, 55000)
	sc := bufio.NewScanner(strings.NewReader(ouiData))
	sc.Buffer(make([]byte, 0, 4096), 1<<16)
	for sc.Scan() {
		line := sc.Text()
		if tab := strings.IndexByte(line, '\t'); tab > 0 {
			ouiMap[line[:tab]] = line[tab+1:]
		}
	}
}

var macStrip = strings.NewReplacer(":", "", "-", "", ".", "", " ", "")

// vendorFor maps a MAC to its manufacturer via the IEEE registry, trying the
// most specific assignment first (/36 → /28 → /24), or "" if unknown.
func vendorFor(mac string) string {
	ouiOnce.Do(loadOUI)
	h := strings.ToUpper(macStrip.Replace(mac))
	if len(h) < 6 {
		return ""
	}
	if len(h) >= 9 {
		if v, ok := ouiMap[h[:9]]; ok {
			return v
		}
	}
	if len(h) >= 7 {
		if v, ok := ouiMap[h[:7]]; ok {
			return v
		}
	}
	if v, ok := ouiMap[h[:6]]; ok {
		return v
	}
	return ""
}
