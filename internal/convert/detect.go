package convert

import (
	"path/filepath"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// Detection resolves an uploaded file to a matrix format. Content is sniffed
// with libmagic-style magic bytes (never trusting the extension alone), but a
// few families are genuinely ambiguous by content — RAW files masquerade as
// TIFF/ISO-BMFF/octet-stream, and CSV/TSV/Markdown/plain text share one magic
// signature — so within those families the original extension disambiguates.

var extFormat = func() map[string]string {
	m := map[string]string{}
	for _, f := range formats {
		for _, e := range f.Exts {
			m[e] = f.ID
		}
	}
	return m
}()

// mimeFormats maps a base MIME type to the format ids that sniff as it, in
// matrix order (the first is the canonical fallback when the extension does
// not disambiguate).
var mimeFormats = func() map[string][]string {
	m := map[string][]string{}
	for _, f := range formats {
		for _, mime := range f.MIMEs {
			m[mime] = append(m[mime], f.ID)
		}
	}
	// Opus lives in an Ogg container and sniffs as audio/ogg.
	m["audio/ogg"] = appendUnique(m["audio/ogg"], "opus")
	return m
}()

// textFamily is the set an unspecific text/plain sniff can refine to by
// extension; txt is the default.
var textFamily = []string{"txt", "csv", "tsv", "md", "json", "html"}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// Detect resolves a file on disk (with its original upload name for extension
// hints) to a format. ok is false when nothing in the matrix matches.
func Detect(path, originalName string) (Format, bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(originalName), "."))
	extFmt := extFormat[ext]

	// RAW files win on extension: they sniff as their container, not as RAW.
	if extFmt != "" {
		if f, ok := formatByID[extFmt]; ok && f.Category == CategoryRaw {
			return f, true
		}
	}

	if mt, err := mimetype.DetectFile(path); err == nil {
		for m := mt; m != nil; m = m.Parent() {
			base := strings.ToLower(strings.TrimSpace(strings.SplitN(m.String(), ";", 2)[0]))
			if base == "text/plain" {
				if f, ok := refine(textFamily, extFmt, "txt"); ok {
					return f, true
				}
			}
			if candidates, ok := mimeFormats[base]; ok {
				if f, ok := refine(candidates, extFmt, candidates[0]); ok {
					return f, true
				}
			}
		}
	}

	// Last resort: trust the extension (obscure formats sniffing as
	// octet-stream still convert if the extension is one we know).
	if f, ok := formatByID[extFmt]; ok {
		return f, true
	}
	return Format{}, false
}

// refine picks the extension's format when it is among the candidates,
// otherwise the given fallback id.
func refine(candidates []string, extFmt, fallback string) (Format, bool) {
	if extFmt != "" {
		for _, c := range candidates {
			if c == extFmt {
				return formatByID[c], true
			}
		}
	}
	f, ok := formatByID[fallback]
	return f, ok
}
