package convert

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMatrixIntegrity(t *testing.T) {
	for _, f := range Formats() {
		if f.Category == "" {
			t.Errorf("format %q has no category", f.ID)
		}
		if len(f.Exts) == 0 {
			t.Errorf("format %q has no extensions", f.ID)
		}
		if f.Label == "" {
			t.Errorf("format %q has no label", f.ID)
		}
	}

	seen := map[string]bool{}
	for _, e := range Edges() {
		key := e.From + ">" + e.To
		if e.From == e.To {
			t.Errorf("identity edge %q must not exist", key)
		}
		if seen[key] {
			t.Errorf("duplicate edge %q", key)
		}
		seen[key] = true
		if _, ok := FormatByID(e.From); !ok {
			t.Errorf("edge %q references unknown source format", key)
		}
		if _, ok := FormatByID(e.To); !ok {
			t.Errorf("edge %q references unknown target format", key)
		}
		if e.Kind == "" {
			t.Errorf("edge %q has no kind", key)
		}
		if strings.TrimSpace(e.Template) == "" {
			t.Errorf("edge %q has no template", key)
		}
		for _, od := range e.Options {
			if od.Name == "" || od.Max <= od.Min || od.Default < od.Min || od.Default > od.Max {
				t.Errorf("edge %q has invalid option %+v", key, od)
			}
		}
	}
}

// Every non-table edge must build a dispatchable plan that references its input
// and output paths — this is what guarantees the matrix and runner stay in sync.
func TestEveryEdgeBuildsPlan(t *testing.T) {
	const in, out, tmp = "/b/in/item.src", "/b/out/item.dst", "/b/tmp"
	for _, e := range Edges() {
		if e.Kind == KindTable {
			continue
		}
		specs, err := buildPlan(e, in, out, tmp, "item", Options{})
		if err != nil {
			t.Errorf("edge %s>%s: buildPlan: %v", e.From, e.To, err)
			continue
		}
		if len(specs) == 0 {
			t.Errorf("edge %s>%s: empty plan", e.From, e.To)
			continue
		}
		var sawIn, sawOut bool
		for _, s := range specs {
			if s.tool == "" {
				t.Errorf("edge %s>%s: spec with no tool", e.From, e.To)
			}
			for _, a := range s.args {
				if strings.Contains(a, in) {
					sawIn = true
				}
				if a == out {
					sawOut = true
				}
			}
		}
		if !sawIn {
			t.Errorf("edge %s>%s: plan never references the input", e.From, e.To)
		}
		if !sawOut {
			t.Errorf("edge %s>%s: plan never references the output", e.From, e.To)
		}
	}
}

func TestTableRoundTrip(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "in.csv")
	const original = "name,age\nAlice,30\nBob,25\n"
	if err := os.WriteFile(csvPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	jsonPath := filepath.Join(dir, "mid.json")
	if err := ConvertTable(csvPath, jsonPath, "csv", "json"); err != nil {
		t.Fatalf("csv→json: %v", err)
	}
	if body, _ := os.ReadFile(jsonPath); !strings.Contains(string(body), `"age": 30`) {
		t.Errorf("json did not type the age column as a number: %s", body)
	}

	backPath := filepath.Join(dir, "back.csv")
	if err := ConvertTable(jsonPath, backPath, "json", "csv"); err != nil {
		t.Fatalf("json→csv: %v", err)
	}
	got, _ := os.ReadFile(backPath)
	if string(got) != original {
		t.Errorf("round trip mismatch:\n got %q\nwant %q", got, original)
	}

	// Markdown and HTML render a header and rows.
	mdPath := filepath.Join(dir, "out.md")
	if err := ConvertTable(csvPath, mdPath, "csv", "md"); err != nil {
		t.Fatalf("csv→md: %v", err)
	}
	md, _ := os.ReadFile(mdPath)
	if !strings.Contains(string(md), "| name | age |") || !strings.Contains(string(md), "| Alice | 30 |") {
		t.Errorf("markdown table malformed:\n%s", md)
	}

	// xlsx round-trips through the neutral table too.
	xlsxPath := filepath.Join(dir, "out.xlsx")
	if err := ConvertTable(csvPath, xlsxPath, "csv", "xlsx"); err != nil {
		t.Fatalf("csv→xlsx: %v", err)
	}
	fromXlsx := filepath.Join(dir, "from_xlsx.csv")
	if err := ConvertTable(xlsxPath, fromXlsx, "xlsx", "csv"); err != nil {
		t.Fatalf("xlsx→csv: %v", err)
	}
	if got, _ := os.ReadFile(fromXlsx); string(got) != original {
		t.Errorf("xlsx round trip mismatch:\n got %q\nwant %q", got, original)
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()

	pngPath := filepath.Join(dir, "pic.png")
	writePNG(t, pngPath)
	if f, ok := Detect(pngPath, "pic.png"); !ok || f.ID != "png" {
		t.Errorf("png by name: got %q ok=%v", f.ID, ok)
	}
	// Content beats a wrong extension.
	if f, ok := Detect(pngPath, "pic.txt"); !ok || f.ID != "png" {
		t.Errorf("png sniffed under .txt name: got %q ok=%v", f.ID, ok)
	}

	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("a,b,c\n1,2,3\n4,5,6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, ok := Detect(csvPath, "data.csv"); !ok || f.ID != "csv" {
		t.Errorf("csv: got %q ok=%v", f.ID, ok)
	}

	jsonPath := filepath.Join(dir, "d.json")
	if err := os.WriteFile(jsonPath, []byte(`[{"a":1,"b":2}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, ok := Detect(jsonPath, "d.json"); !ok || f.ID != "json" {
		t.Errorf("json: got %q ok=%v", f.ID, ok)
	}

	// RAW wins by extension even though the bytes look like TIFF/garbage.
	rawPath := filepath.Join(dir, "shot.cr2")
	if err := os.WriteFile(rawPath, []byte("II*\x00\x08\x00\x00\x00 not really raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, ok := Detect(rawPath, "shot.cr2"); !ok || f.ID != "cr2" {
		t.Errorf("cr2: got %q ok=%v", f.ID, ok)
	}

	// Nothing in the matrix → unsupported.
	binPath := filepath.Join(dir, "blob.qqq")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02, 0x03, 0x7f}, 0o644); err != nil {
		t.Fatal(err)
	}
	if f, ok := Detect(binPath, "blob.qqq"); ok {
		t.Errorf("expected unsupported, got %q", f.ID)
	}
}

func TestProbeTablesAlwaysAvailable(t *testing.T) {
	caps := probe()
	e, ok := EdgeFor("csv", "json")
	if !ok {
		t.Fatal("csv→json edge missing")
	}
	if avail, reason := caps.Available(e); !avail {
		t.Errorf("table edge should always be available, got %q", reason)
	}
}

// The runner actually shells out to ImageMagick (privilege drop off so the test
// is portable) and must produce a non-empty output.
func TestRunnerConvertsPNGToJPG(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("magick not installed")
	}
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "in.png"))

	e, ok := EdgeFor("png", "jpg")
	if !ok {
		t.Fatal("png→jpg edge missing")
	}
	r := NewRunner(false, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Convert(ctx, e, dir, "in.png", "out.jpg", ".", "item", Options{}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	out := filepath.Join(dir, "out.jpg")
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("output is empty")
	}
	// The output must sniff back as a JPEG.
	if f, ok := Detect(out, "out.jpg"); !ok || f.ID != "jpg" {
		t.Errorf("output did not detect as jpg: %q ok=%v", f.ID, ok)
	}
}

// TestRunnerPrivilegeDrop exercises the production path: root prepares the batch
// dirs, then the tool runs privilege-dropped via setpriv. The proof is that the
// output file ends up owned by the sandbox uid, not root.
func TestRunnerPrivilegeDrop(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("not root")
	}
	if _, err := exec.LookPath("setpriv"); err != nil {
		t.Skip("setpriv not installed")
	}
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("magick not installed")
	}
	dataDir := t.TempDir()
	svc := NewService(nil, dataDir, slog.Default(), nil, nil)
	if !svc.runner.drop {
		t.Fatal("expected privilege drop to be active as root with setpriv present")
	}
	convID := uuid.New()
	inDir, _, _, err := svc.PrepareDirs(convID)
	if err != nil {
		t.Fatal(err)
	}
	itemID := uuid.New()
	writePNG(t, svc.InputPath(inDir, itemID, "png"))

	e, _ := EdgeFor("png", "jpg")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inRel := filepath.Join("in", itemID.String()+".png")
	outRel := filepath.Join("out", itemID.String()+".jpg")
	if err := svc.runner.Convert(ctx, e, svc.batchDir(convID), inRel, outRel, "tmp", itemID.String(), Options{}); err != nil {
		t.Fatalf("dropped convert: %v", err)
	}
	fi, err := os.Stat(filepath.Join(svc.batchDir(convID), outRel))
	if err != nil || fi.Size() == 0 {
		t.Fatalf("no output: %v", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no syscall.Stat_t on this platform")
	}
	if st.Uid != dropUID {
		t.Errorf("output owned by uid %d, want %d — privilege drop did not take effect", st.Uid, dropUID)
	}
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 60, B: 60, A: 255})
		}
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
