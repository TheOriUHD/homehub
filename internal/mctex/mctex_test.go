package mctex

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fakeJar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	add("assets/minecraft/textures/item/diamond_sword.png", pngBytes(t, 16, 16))
	add("assets/minecraft/textures/block/stone.png", pngBytes(t, 16, 16))
	add("assets/minecraft/textures/block/diamond_sword.png", pngBytes(t, 16, 32)) // item must win
	add("assets/minecraft/textures/block/water_still.png", pngBytes(t, 16, 64))   // animated strip
	add("assets/minecraft/textures/font/ascii.png", pngBytes(t, 128, 128))
	add("assets/minecraft/textures/entity/player/wide/steve.png", pngBytes(t, 64, 64))
	add("assets/minecraft/textures/entity/zombie/zombie.png", pngBytes(t, 64, 64)) // not wanted
	add("net/minecraft/Main.class", []byte("not a texture"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstall(t *testing.T) {
	jar := fakeJar(t)
	sum := sha1.Sum(jar)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"latest":{"release":"9.9"},"versions":[{"id":"9.9","url":"%s/version"}]}`, srv.URL)
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"downloads":{"client":{"url":"%s/client.jar","sha1":"%s","size":%d}}}`,
			srv.URL, hex.EncodeToString(sum[:]), len(jar))
	})
	mux.HandleFunc("/client.jar", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(jar) })
	srv = httptest.NewServer(mux)
	defer srv.Close()

	oldURL, oldMin := manifestURL, minTextures
	manifestURL, minTextures = srv.URL+"/manifest", 1
	defer func() { manifestURL, minTextures = oldURL, oldMin }()

	dir := t.TempDir()
	res, err := Install(context.Background(), dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "9.9" {
		t.Fatalf("version = %q", res.Version)
	}
	out := Dir(dir)
	for _, name := range []string{"diamond_sword.png", "stone.png", "water_still.png", "_font_ascii.png", "_steve.png", "_craft_arrow.png"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "zombie.png")); err == nil {
		t.Error("unrelated entity texture was installed")
	}
	// the item texture (16x16) beat the taller block texture of the same name
	f, _ := os.Open(filepath.Join(out, "diamond_sword.png"))
	defer f.Close()
	cfg, _ := png.DecodeConfig(f)
	if cfg.Width != 16 || cfg.Height != 16 {
		t.Errorf("diamond_sword is %dx%d, want the 16x16 item sprite", cfg.Width, cfg.Height)
	}
	// the animated strip was cropped to its first (square) frame
	g, _ := os.Open(filepath.Join(out, "water_still.png"))
	defer g.Close()
	cfg, _ = png.DecodeConfig(g)
	if cfg.Width != 16 || cfg.Height != 16 {
		t.Errorf("water_still is %dx%d, want one 16x16 frame", cfg.Width, cfg.Height)
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	jar := fakeJar(t)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"latest":{"release":"9.9"},"versions":[{"id":"9.9","url":"%s/version"}]}`, srv.URL)
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"downloads":{"client":{"url":"%s/client.jar","sha1":"deadbeef","size":%d}}}`, srv.URL, len(jar))
	})
	mux.HandleFunc("/client.jar", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(jar) })
	srv = httptest.NewServer(mux)
	defer srv.Close()
	old := manifestURL
	manifestURL = srv.URL + "/manifest"
	defer func() { manifestURL = old }()

	dir := t.TempDir()
	if _, err := Install(context.Background(), dir, "", nil); err == nil {
		t.Fatal("install accepted a jar that does not match Mojang's checksum")
	}
	if _, err := os.Stat(Dir(dir)); err == nil {
		t.Fatal("a failed install left a textures directory behind")
	}
}
