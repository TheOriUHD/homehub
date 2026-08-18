// Package convert is HomeHub's universal file converter. It owns a single
// source-of-truth format matrix (formats + directed conversion edges), a
// content-sniffing detector, native table transforms, and a runner that
// executes external tools (ffmpeg, ImageMagick, libraw, pandoc) privilege-
// dropped, timeout-killed as a process group, and resource-limited — because
// the control plane runs as root and these tools parse untrusted uploads.
//
// Everything downstream (upload validation, target validation, dispatch, the
// /convert/formats catalog the UI renders) derives from the matrix in this
// file. Nothing else hard-codes the conversion graph.
package convert

import "sort"

// Category groups formats for the UI's picker and for dispatch. A format has
// exactly one home category; edges may still cross categories (an image →
// gif, an audio → mp4), which the UI labels.
type Category string

const (
	CategoryImage     Category = "image"
	CategoryRaw       Category = "raw"
	CategoryVideo     Category = "video"
	CategoryAudio     Category = "audio"
	CategoryAnimation Category = "animation"
	CategoryTable     Category = "table"
	CategoryDocument  Category = "document"
)

// Kind names the engine that services an edge. Video, audio and animation all
// run through ffmpeg; the source category still decides the exact argv.
const (
	KindImage  = "magick"
	KindRaw    = "raw"
	KindFFmpeg = "ffmpeg"
	KindTable  = "table"
	KindPandoc = "pandoc"
)

// Format is a node in the conversion graph.
type Format struct {
	ID       string
	Category Category
	Label    string
	Exts     []string // canonical extensions, primary first (no leading dot)
	MIMEs    []string // MIME types this format sniffs as (for detection)
}

// PrimaryExt is the extension server-generated output files carry.
func (f Format) PrimaryExt() string {
	if len(f.Exts) == 0 {
		return f.ID
	}
	return f.Exts[0]
}

// OptionDef describes a single tunable an edge exposes (a slider in the UI).
type OptionDef struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Default int    `json:"default"`
}

func qualityOpt() OptionDef {
	return OptionDef{Name: "quality", Label: "Quality", Min: 1, Max: 100, Default: 85}
}
func crfOpt(def int) OptionDef {
	return OptionDef{Name: "crf", Label: "Quality (CRF, lower is better)", Min: 0, Max: 51, Default: def}
}

// Edge is a directed conversion from one format to another via one engine.
// Template is the canonical, human-readable command spec (for the catalog and
// for review); the runner builds the real argv array in code so no shell and
// no interpolation of paths into a single string is ever involved.
type Edge struct {
	From     string
	To       string
	Kind     string
	Template string
	Options  []OptionDef
}

// ---- format table ----------------------------------------------------------

// formats is the ordered node list. Order matters for detection: when several
// formats claim one MIME type, the earlier one is the canonical fallback.
var formats = []Format{
	// image
	{ID: "jpg", Category: CategoryImage, Label: "JPEG", Exts: []string{"jpg", "jpeg"}, MIMEs: []string{"image/jpeg"}},
	{ID: "png", Category: CategoryImage, Label: "PNG", Exts: []string{"png"}, MIMEs: []string{"image/png"}},
	{ID: "webp", Category: CategoryImage, Label: "WebP", Exts: []string{"webp"}, MIMEs: []string{"image/webp"}},
	{ID: "heic", Category: CategoryImage, Label: "HEIC", Exts: []string{"heic", "heif"}, MIMEs: []string{"image/heic", "image/heif"}},
	{ID: "avif", Category: CategoryImage, Label: "AVIF", Exts: []string{"avif"}, MIMEs: []string{"image/avif"}},
	{ID: "tiff", Category: CategoryImage, Label: "TIFF", Exts: []string{"tiff", "tif"}, MIMEs: []string{"image/tiff"}},
	{ID: "bmp", Category: CategoryImage, Label: "BMP", Exts: []string{"bmp"}, MIMEs: []string{"image/bmp", "image/x-ms-bmp"}},
	{ID: "psd", Category: CategoryImage, Label: "PSD", Exts: []string{"psd"}, MIMEs: []string{"image/vnd.adobe.photoshop"}},
	{ID: "svg", Category: CategoryImage, Label: "SVG", Exts: []string{"svg"}, MIMEs: []string{"image/svg+xml"}},
	{ID: "pdf", Category: CategoryImage, Label: "PDF", Exts: []string{"pdf"}, MIMEs: []string{"application/pdf"}},

	// raw (extension is authoritative — RAW files sniff as tiff/octet-stream)
	{ID: "cr2", Category: CategoryRaw, Label: "Canon CR2", Exts: []string{"cr2"}, MIMEs: []string{"image/x-canon-cr2"}},
	{ID: "cr3", Category: CategoryRaw, Label: "Canon CR3", Exts: []string{"cr3"}, MIMEs: []string{"image/x-canon-cr3"}},
	{ID: "nef", Category: CategoryRaw, Label: "Nikon NEF", Exts: []string{"nef"}, MIMEs: []string{"image/x-nikon-nef"}},
	{ID: "arw", Category: CategoryRaw, Label: "Sony ARW", Exts: []string{"arw"}, MIMEs: []string{"image/x-sony-arw"}},
	{ID: "dng", Category: CategoryRaw, Label: "Adobe DNG", Exts: []string{"dng"}, MIMEs: []string{"image/x-adobe-dng"}},
	{ID: "orf", Category: CategoryRaw, Label: "Olympus ORF", Exts: []string{"orf"}, MIMEs: []string{"image/x-olympus-orf"}},
	{ID: "rw2", Category: CategoryRaw, Label: "Panasonic RW2", Exts: []string{"rw2"}, MIMEs: []string{"image/x-panasonic-rw2"}},
	{ID: "raf", Category: CategoryRaw, Label: "Fuji RAF", Exts: []string{"raf"}, MIMEs: []string{"image/x-fuji-raf"}},
	{ID: "pef", Category: CategoryRaw, Label: "Pentax PEF", Exts: []string{"pef"}, MIMEs: []string{"image/x-pentax-pef"}},
	{ID: "srw", Category: CategoryRaw, Label: "Samsung SRW", Exts: []string{"srw"}, MIMEs: []string{"image/x-samsung-srw"}},

	// video
	{ID: "mp4", Category: CategoryVideo, Label: "MP4", Exts: []string{"mp4"}, MIMEs: []string{"video/mp4"}},
	{ID: "mkv", Category: CategoryVideo, Label: "MKV", Exts: []string{"mkv"}, MIMEs: []string{"video/x-matroska"}},
	{ID: "mov", Category: CategoryVideo, Label: "MOV", Exts: []string{"mov"}, MIMEs: []string{"video/quicktime"}},
	{ID: "avi", Category: CategoryVideo, Label: "AVI", Exts: []string{"avi"}, MIMEs: []string{"video/x-msvideo"}},
	{ID: "webm", Category: CategoryVideo, Label: "WebM", Exts: []string{"webm"}, MIMEs: []string{"video/webm"}},
	{ID: "m4v", Category: CategoryVideo, Label: "M4V", Exts: []string{"m4v"}, MIMEs: []string{"video/x-m4v", "video/mp4"}},
	{ID: "mpeg", Category: CategoryVideo, Label: "MPEG", Exts: []string{"mpeg", "mpg"}, MIMEs: []string{"video/mpeg"}},
	{ID: "ts", Category: CategoryVideo, Label: "MPEG-TS", Exts: []string{"ts"}, MIMEs: []string{"video/mp2t"}},
	{ID: "wmv", Category: CategoryVideo, Label: "WMV", Exts: []string{"wmv"}, MIMEs: []string{"video/x-ms-wmv"}},
	{ID: "flv", Category: CategoryVideo, Label: "FLV", Exts: []string{"flv"}, MIMEs: []string{"video/x-flv"}},
	{ID: "3gp", Category: CategoryVideo, Label: "3GP", Exts: []string{"3gp"}, MIMEs: []string{"video/3gpp"}},

	// audio
	{ID: "mp3", Category: CategoryAudio, Label: "MP3", Exts: []string{"mp3"}, MIMEs: []string{"audio/mpeg"}},
	{ID: "wav", Category: CategoryAudio, Label: "WAV", Exts: []string{"wav"}, MIMEs: []string{"audio/wav", "audio/x-wav"}},
	{ID: "flac", Category: CategoryAudio, Label: "FLAC", Exts: []string{"flac"}, MIMEs: []string{"audio/flac", "audio/x-flac"}},
	{ID: "ogg", Category: CategoryAudio, Label: "OGG", Exts: []string{"ogg", "oga"}, MIMEs: []string{"audio/ogg"}},
	{ID: "opus", Category: CategoryAudio, Label: "Opus", Exts: []string{"opus"}, MIMEs: []string{"audio/opus"}},
	{ID: "m4a", Category: CategoryAudio, Label: "M4A", Exts: []string{"m4a"}, MIMEs: []string{"audio/mp4", "audio/x-m4a"}},
	{ID: "wma", Category: CategoryAudio, Label: "WMA", Exts: []string{"wma"}, MIMEs: []string{"audio/x-ms-wma"}},
	{ID: "aiff", Category: CategoryAudio, Label: "AIFF", Exts: []string{"aiff", "aif"}, MIMEs: []string{"audio/aiff", "audio/x-aiff"}},

	// animation
	{ID: "gif", Category: CategoryAnimation, Label: "GIF", Exts: []string{"gif"}, MIMEs: []string{"image/gif"}},
	{ID: "apng", Category: CategoryAnimation, Label: "APNG", Exts: []string{"apng"}, MIMEs: []string{"image/apng", "image/vnd.mozilla.apng"}},

	// table
	{ID: "csv", Category: CategoryTable, Label: "CSV", Exts: []string{"csv"}, MIMEs: []string{"text/csv"}},
	{ID: "tsv", Category: CategoryTable, Label: "TSV", Exts: []string{"tsv"}, MIMEs: []string{"text/tab-separated-values"}},
	{ID: "xlsx", Category: CategoryTable, Label: "Excel", Exts: []string{"xlsx"}, MIMEs: []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}},
	{ID: "json", Category: CategoryTable, Label: "JSON", Exts: []string{"json"}, MIMEs: []string{"application/json"}},

	// document
	{ID: "md", Category: CategoryDocument, Label: "Markdown", Exts: []string{"md", "markdown"}, MIMEs: []string{"text/markdown"}},
	{ID: "html", Category: CategoryDocument, Label: "HTML", Exts: []string{"html", "htm"}, MIMEs: []string{"text/html"}},
	{ID: "txt", Category: CategoryDocument, Label: "Text", Exts: []string{"txt"}, MIMEs: []string{"text/plain"}},
	{ID: "docx", Category: CategoryDocument, Label: "Word", Exts: []string{"docx"}, MIMEs: []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}},
	{ID: "odt", Category: CategoryDocument, Label: "OpenDocument", Exts: []string{"odt"}, MIMEs: []string{"application/vnd.oasis.opendocument.text"}},
	{ID: "rtf", Category: CategoryDocument, Label: "RTF", Exts: []string{"rtf"}, MIMEs: []string{"application/rtf", "text/rtf"}},
	{ID: "epub", Category: CategoryDocument, Label: "EPUB", Exts: []string{"epub"}, MIMEs: []string{"application/epub+zip"}},
}

// CategoryOrder is the display order for the UI's category picker.
var CategoryOrder = []Category{
	CategoryImage, CategoryRaw, CategoryVideo, CategoryAudio, CategoryAnimation, CategoryTable, CategoryDocument,
}

var formatByID = func() map[string]Format {
	m := make(map[string]Format, len(formats))
	for _, f := range formats {
		m[f.ID] = f
	}
	return m
}()

// FormatByID returns the format and whether it exists.
func FormatByID(id string) (Format, bool) {
	f, ok := formatByID[id]
	return f, ok
}

// Formats returns the full node list in display order.
func Formats() []Format { return formats }

// ---- edge table ------------------------------------------------------------

// edges is built once from the category rules below. (From,To) is unique: when
// two categories would produce the same pair, the first one added wins, which
// is why image is generated before animation (an animated webp → gif is served
// by ImageMagick, which coalesces frames, rather than duplicated under ffmpeg).
var edges, edgeIndex = buildEdges()

func buildEdges() ([]Edge, map[string]Edge) {
	var out []Edge
	idx := map[string]Edge{}
	add := func(e Edge) {
		if e.From == e.To { // never an identity edge
			return
		}
		key := e.From + ">" + e.To
		if _, ok := idx[key]; ok {
			return
		}
		idx[key] = e
		out = append(out, e)
	}

	// image (ImageMagick)
	imageIn := []string{"jpg", "png", "webp", "heic", "avif", "tiff", "bmp", "psd", "svg"}
	imageOut := []string{"jpg", "png", "webp", "avif", "tiff", "bmp", "gif", "pdf"}
	lossy := map[string]bool{"jpg": true, "webp": true, "avif": true}
	for _, from := range imageIn {
		for _, to := range imageOut {
			e := Edge{From: from, To: to, Kind: KindImage, Template: imageTemplate(from, to)}
			if lossy[to] {
				e.Options = []OptionDef{qualityOpt()}
			}
			add(e)
		}
	}

	// raw (libraw → ImageMagick)
	rawIn := []string{"cr2", "cr3", "nef", "arw", "dng", "orf", "rw2", "raf", "pef", "srw"}
	rawOut := []string{"jpg", "png", "tiff", "webp"}
	for _, from := range rawIn {
		for _, to := range rawOut {
			e := Edge{From: from, To: to, Kind: KindRaw, Template: rawTemplate(to)}
			if lossy[to] {
				e.Options = []OptionDef{qualityOpt()}
			}
			add(e)
		}
	}

	// video (ffmpeg)
	videoIn := []string{"mp4", "mkv", "mov", "avi", "webm", "m4v", "mpeg", "ts", "wmv", "flv", "3gp"}
	videoOut := []string{"mp4", "webm", "mkv", "mov", "gif", "mp3", "m4a", "wav", "flac", "ogg", "opus", "jpg", "png"}
	for _, from := range videoIn {
		for _, to := range videoOut {
			e := Edge{From: from, To: to, Kind: KindFFmpeg, Template: ffmpegTemplate(CategoryVideo, from, to)}
			if crf, ok := crfDefault(to); ok {
				e.Options = []OptionDef{crfOpt(crf)}
			}
			add(e)
		}
	}

	// audio (ffmpeg), plus the cross edge to a black-video mp4
	audioIn := []string{"mp3", "wav", "flac", "ogg", "opus", "m4a", "wma", "aiff"}
	audioOut := []string{"mp3", "wav", "flac", "ogg", "opus", "m4a", "aiff", "mp4"}
	for _, from := range audioIn {
		for _, to := range audioOut {
			add(Edge{From: from, To: to, Kind: KindFFmpeg, Template: ffmpegTemplate(CategoryAudio, from, to)})
		}
	}

	// animation (ffmpeg). webp→gif and webp→png are already served by image.
	animIn := []string{"gif", "apng", "webp"}
	animOut := []string{"mp4", "webm", "gif", "apng", "png"}
	for _, from := range animIn {
		for _, to := range animOut {
			e := Edge{From: from, To: to, Kind: KindFFmpeg, Template: ffmpegTemplate(CategoryAnimation, from, to)}
			if crf, ok := crfDefault(to); ok {
				e.Options = []OptionDef{crfOpt(crf)}
			}
			add(e)
		}
	}

	// table (native Go — always available)
	tableIn := []string{"csv", "tsv", "xlsx", "json"}
	tableOut := []string{"csv", "tsv", "xlsx", "json", "md", "html"}
	for _, from := range tableIn {
		for _, to := range tableOut {
			add(Edge{From: from, To: to, Kind: KindTable, Template: "native: parse " + from + " → write " + to})
		}
	}

	// document (pandoc)
	docSet := []string{"md", "html", "txt", "docx", "odt", "rtf", "epub"}
	for _, from := range docSet {
		for _, to := range docSet {
			add(Edge{From: from, To: to, Kind: KindPandoc, Template: pandocTemplate(from, to)})
		}
	}

	return out, idx
}

// crfDefault returns the default CRF for a video/animation target, and whether
// that target takes a CRF option at all.
func crfDefault(to string) (int, bool) {
	switch to {
	case "mp4", "mov":
		return 23, true
	case "webm":
		return 32, true
	}
	return 0, false
}

// Edges returns the full edge list.
func Edges() []Edge { return edges }

// EdgeFor returns the edge for a (from,to) pair, if the conversion exists.
func EdgeFor(from, to string) (Edge, bool) {
	e, ok := edgeIndex[from+">"+to]
	return e, ok
}

// Outputs returns every edge leaving a source format, sorted by target id.
func Outputs(from string) []Edge {
	var out []Edge
	for _, e := range edges {
		if e.From == from {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].To < out[j].To })
	return out
}

// IsInput reports whether a format has at least one outgoing edge (i.e. it can
// be uploaded and converted from). pdf is the one output-only format.
func IsInput(id string) bool {
	for _, e := range edges {
		if e.From == id {
			return true
		}
	}
	return false
}
