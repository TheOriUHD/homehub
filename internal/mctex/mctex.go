// Package mctex installs the Minecraft item and GUI textures the inventory
// viewer draws with.
//
// The textures are Mojang's artwork, so they are not shipped with HomeHub.
// Instead the operator runs `homehub minecraft-textures` once: it downloads
// the official client jar straight from Mojang's launcher servers (verifying
// the published SHA-1), copies out the handful of images the UI needs and
// keeps them in the data directory. Nothing leaves the machine and nothing
// is redistributed. Until it has been run the inventory view falls back to
// text labels.
package mctex

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

// minTextures is the fewest textures a plausible client jar yields; fewer
// means the jar layout changed and the install should fail loudly.
var minTextures = 100

// maxJar bounds the client download; current jars are well under 100 MB.
const maxJar = 400 << 20

// Dir is where installed textures live under the data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, "mc-tex") }

// Result describes a finished install.
type Result struct {
	Version string
	Files   int
}

// special maps jar paths to the underscore-prefixed names the UI requests.
var special = map[string]string{
	"assets/minecraft/textures/font/ascii.png":                            "_font_ascii.png",
	"assets/minecraft/textures/gui/sprites/hud/hotbar_selection.png":      "_hotbar_selection.png",
	"assets/minecraft/textures/gui/sprites/recipe_book/button.png":        "_recipe_book.png",
	"assets/minecraft/textures/gui/sprites/container/slot/helmet.png":     "_slot_helmet.png",
	"assets/minecraft/textures/gui/sprites/container/slot/chestplate.png": "_slot_chestplate.png",
	"assets/minecraft/textures/gui/sprites/container/slot/leggings.png":   "_slot_leggings.png",
	"assets/minecraft/textures/gui/sprites/container/slot/boots.png":      "_slot_boots.png",
	"assets/minecraft/textures/gui/sprites/container/slot/shield.png":     "_slot_shield.png",
}

// Install downloads the client for version ("" = latest release) and writes
// the textures into Dir(dataDir), replacing any previous install.
func Install(ctx context.Context, dataDir, version string, logf func(format string, args ...any)) (Result, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	hc := &http.Client{Timeout: 10 * time.Minute}

	var manifest struct {
		Latest   struct{ Release string } `json:"latest"`
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := getJSON(ctx, hc, manifestURL, &manifest); err != nil {
		return Result{}, fmt.Errorf("read the version manifest: %w", err)
	}
	if version == "" {
		version = manifest.Latest.Release
	}
	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == version {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		return Result{}, fmt.Errorf("Minecraft version %q is not in Mojang's manifest", version)
	}
	var meta struct {
		Downloads struct {
			Client struct {
				URL  string `json:"url"`
				SHA1 string `json:"sha1"`
				Size int64  `json:"size"`
			} `json:"client"`
		} `json:"downloads"`
	}
	if err := getJSON(ctx, hc, versionURL, &meta); err != nil {
		return Result{}, fmt.Errorf("read version %s: %w", version, err)
	}
	c := meta.Downloads.Client
	if c.URL == "" || c.Size <= 0 || c.Size > maxJar {
		return Result{}, fmt.Errorf("version %s has no usable client download", version)
	}

	logf("downloading the %s client (%d MB) from Mojang", version, c.Size>>20)
	tmp, err := os.CreateTemp(dataDir, "mc-client-*.jar")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("client download: %s", resp.Status)
	}
	sum := sha1.New()
	if _, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(resp.Body, maxJar)); err != nil {
		return Result{}, err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != c.SHA1 {
		return Result{}, fmt.Errorf("client jar checksum mismatch (got %s, want %s)", got, c.SHA1)
	}

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return Result{}, err
	}
	defer zr.Close()

	// target name -> (priority, zip entry). Items beat blocks so a name that
	// exists as both shows the item sprite, as the inventory does.
	type pick struct {
		prio int
		f    *zip.File
	}
	picks := map[string]pick{}
	set := func(name string, prio int, f *zip.File) {
		if cur, ok := picks[name]; !ok || prio > cur.prio {
			picks[name] = pick{prio, f}
		}
	}
	for _, f := range zr.File {
		n := f.Name
		if !strings.HasPrefix(n, "assets/minecraft/textures/") || !strings.HasSuffix(n, ".png") {
			continue
		}
		if to, ok := special[n]; ok {
			set(to, 3, f)
			continue
		}
		switch path.Dir(n) {
		case "assets/minecraft/textures/item":
			set(path.Base(n), 2, f)
		case "assets/minecraft/textures/block":
			set(path.Base(n), 1, f)
		case "assets/minecraft/textures/entity/player/wide":
			if path.Base(n) == "steve.png" {
				set("_steve.png", 3, f)
			}
		}
	}
	if len(picks) < minTextures {
		return Result{}, fmt.Errorf("version %s: found only %d textures; the jar layout may have changed", version, len(picks))
	}

	out := Dir(dataDir)
	stage := out + ".new"
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return Result{}, err
	}
	for name, p := range picks {
		data, err := readEntry(p.f)
		if err != nil {
			return Result{}, err
		}
		if !strings.HasPrefix(name, "_") {
			data = firstFrame(data)
		}
		if err := os.WriteFile(filepath.Join(stage, name), data, 0o644); err != nil {
			return Result{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "_craft_arrow.png"), craftArrow(), 0o644); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		return Result{}, err
	}
	_ = os.RemoveAll(out)
	if err := os.Rename(stage, out); err != nil {
		return Result{}, err
	}
	return Result{Version: version, Files: len(picks) + 1}, nil
}

func getJSON(ctx context.Context, hc *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v)
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 8<<20))
}

// firstFrame crops an animated strip (height a whole multiple of width) down
// to its first frame, since the UI draws one square sprite.
func firstFrame(data []byte) []byte {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if h <= w || h%w != 0 {
		return data
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return data
	}
	var buf bytes.Buffer
	if png.Encode(&buf, sub.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+w))) != nil {
		return data
	}
	return buf.Bytes()
}

// craftArrow draws the recipe arrow: a plain grey arrow, generated rather
// than copied so no artwork is involved.
func craftArrow() []byte {
	const w, h = 16, 13
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill := color.NRGBA{0x8b, 0x8b, 0x8b, 0xff}
	edge := color.NRGBA{0x37, 0x37, 0x37, 0xff}
	set := func(x, y int, c color.NRGBA) { img.SetNRGBA(x, y, c) }
	for y := 4; y <= 8; y++ { // shaft
		for x := 0; x <= 9; x++ {
			set(x, y, fill)
		}
	}
	for x := 0; x <= 9; x++ {
		set(x, 3, edge)
		set(x, 9, edge)
	}
	for i := 0; i <= 6; i++ { // head
		for y := i; y <= 12-i; y++ {
			set(9+i, y, fill)
		}
		set(9+i, i, edge)
		set(9+i, 12-i, edge)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
