package caddyfile

import (
	"fmt"
	"strings"
)

// FindEntry locates an entry block by primary-or-any domain,
// case-insensitively. Returns the index in Blocks, or -1.
func FindEntry(doc Document, domain string) int {
	needle := strings.ToLower(strings.TrimSpace(domain))
	for i, blk := range doc.Blocks {
		if blk.Kind != KindEntry {
			continue
		}
		for _, d := range blk.Entry.Domains {
			if strings.ToLower(d) == needle {
				return i
			}
		}
	}
	return -1
}

// domainTaken reports whether any entry OR raw site block already serves the
// domain, excluding the block at skip (-1 to exclude nothing).
func domainTaken(doc Document, domain string, skip int) bool {
	needle := strings.ToLower(strings.TrimSpace(domain))
	for i, blk := range doc.Blocks {
		if i == skip {
			continue
		}
		var addrs []string
		switch blk.Kind {
		case KindEntry:
			addrs = blk.Entry.Domains
		case KindRaw:
			addrs = blk.Addresses
		default:
			continue
		}
		for _, a := range addrs {
			if strings.ToLower(a) == needle {
				return true
			}
		}
	}
	return false
}

// Create appends a new entry. It refuses a duplicate domain, and pads with
// gaps so the file stays readable rather than accreting a wall of blocks.
func Create(doc Document, entry Entry) (Document, error) {
	for _, d := range entry.Domains {
		if domainTaken(doc, d, -1) {
			return doc, fmt.Errorf("%s is already served by this Caddyfile", d)
		}
	}
	blocks := append([]Block{}, doc.Blocks...)
	if len(blocks) > 0 {
		last := blocks[len(blocks)-1]
		if last.Kind != KindGap || !strings.HasSuffix(last.Raw, "\n") {
			blocks = append(blocks, Block{Kind: KindGap, Raw: "\n"})
		} else if !strings.HasSuffix(last.Raw, "\n\n") && !strings.HasSuffix(Render(Document{Blocks: blocks}), "\n\n") {
			blocks = append(blocks, Block{Kind: KindGap, Raw: "\n"})
		}
	}
	raw := RenderEntry(entry)
	blocks = append(blocks, Block{Kind: KindEntry, Raw: raw, Entry: &entry, Addresses: entry.Domains})
	return Document{Blocks: blocks}, nil
}

// Update replaces the entry currently serving oldDomain. It refuses a rename
// onto an existing domain, and carries UnknownDirectives across from the
// block being replaced — changing a port must not drop the header line above
// it.
func Update(doc Document, oldDomain string, entry Entry) (Document, error) {
	idx := FindEntry(doc, oldDomain)
	if idx < 0 {
		return doc, fmt.Errorf("no editable site serves %s", oldDomain)
	}
	for _, d := range entry.Domains {
		if domainTaken(doc, d, idx) {
			return doc, fmt.Errorf("%s is already served by this Caddyfile", d)
		}
	}
	entry.UnknownDirectives = doc.Blocks[idx].Entry.UnknownDirectives
	blocks := append([]Block{}, doc.Blocks...)
	raw := RenderEntry(entry)
	blocks[idx] = Block{Kind: KindEntry, Raw: raw, Entry: &entry, Addresses: entry.Domains}
	return Document{Blocks: blocks}, nil
}

// Remove deletes the entry serving the domain, and the gap that follows it
// only if that gap contains no comment — someone wrote that on purpose.
func Remove(doc Document, domain string) (Document, error) {
	idx := FindEntry(doc, domain)
	if idx < 0 {
		return doc, fmt.Errorf("no editable site serves %s", domain)
	}
	blocks := append([]Block{}, doc.Blocks...)
	end := idx + 1
	if end < len(blocks) && blocks[end].Kind == KindGap && !strings.Contains(blocks[end].Raw, "#") {
		end++
	}
	blocks = append(blocks[:idx], blocks[end:]...)
	return Document{Blocks: blocks}, nil
}
