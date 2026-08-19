package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The sandbox identity every external tool is dropped to. 65534 is
// nobody/nogroup on Debian: no home, no shell, no ownership of anything the
// control plane cares about. A codec or parser CVE in ffmpeg/ImageMagick/
// libraw/pandoc therefore lands as nobody, not root.
const (
	dropUID = 65534
	dropGID = 65534
)

// procSem bounds the number of external tool processes running at once across
// every batch, so a flood of conversions never starves the control plane.
var procSem = make(chan struct{}, 2)

// Options are the per-item tunables the client may send. Nil means "use the
// edge's default"; out-of-range values are clamped.
type Options struct {
	Quality *int `json:"quality,omitempty"`
	CRF     *int `json:"crf,omitempty"`
}

func (o Options) valueFor(opt OptionDef) int {
	var p *int
	switch opt.Name {
	case "quality":
		p = o.Quality
	case "crf":
		p = o.CRF
	}
	if p == nil {
		return opt.Default
	}
	v := *p
	if v < opt.Min {
		v = opt.Min
	}
	if v > opt.Max {
		v = opt.Max
	}
	return v
}

// edgeInt resolves an option value for an edge, falling back to a default when
// the edge does not expose that option (e.g. quality on a lossless target,
// where the value is harmless but still passed to the tool).
func edgeInt(e Edge, o Options, name string, fallback int) int {
	for _, od := range e.Options {
		if od.Name == name {
			return o.valueFor(od)
		}
	}
	return fallback
}

// ---- command specs & builders ---------------------------------------------

// procSpec is one external process to run. args are the arguments AFTER the
// tool binary; the runner prepends nice/ionice and the resolved binary path.
// No user-supplied string ever reaches args — inputs/outputs are server
// generated paths — and there is never a shell.
type procSpec struct {
	tool       string   // logical tool: magick|ffmpeg|dcraw_emu|pandoc
	args       []string // arguments after the binary
	stdoutPath string   // when set, stdout is redirected to this file (dcraw)
	magick     bool     // apply the MAGICK_* resource limits
}

func (s procSpec) env() []string {
	// HOME/TMPDIR point at /tmp, which is world-traversable and world-writable
	// (1777): the dropped tool can always create scratch there, whereas the
	// batch dir's absolute path may sit under a 0700 ancestor it cannot reach.
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/tmp",
		"TMPDIR=/tmp",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	if s.magick {
		env = append(env,
			"MAGICK_MEMORY_LIMIT=1GiB",
			"MAGICK_MAP_LIMIT=2GiB",
			"MAGICK_DISK_LIMIT=4GiB",
			"MAGICK_WIDTH_LIMIT=30000",
			"MAGICK_HEIGHT_LIMIT=30000",
			"MAGICK_TMPDIR=/tmp",
		)
	}
	return env
}

const gifFilter = "fps=12,scale=480:-1:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer"

func imageArgs(from, in, out string, q int) []string {
	var a []string
	if from == "svg" {
		a = append(a, "-density", "150", "-background", "none")
	}
	inArg := in
	if from == "psd" {
		inArg = in + "[0]" // flatten to the composited layer only
	}
	a = append(a, inArg, "-auto-orient", "-quality", strconv.Itoa(q), out)
	return a
}

func audioArgs(to string) []string {
	switch to {
	case "mp3":
		return []string{"-vn", "-c:a", "libmp3lame", "-q:a", "2"}
	case "wav":
		return []string{"-vn", "-c:a", "pcm_s16le"}
	case "flac":
		return []string{"-vn", "-c:a", "flac"}
	case "ogg":
		return []string{"-vn", "-c:a", "libvorbis", "-q:a", "5"}
	case "opus":
		return []string{"-vn", "-c:a", "libopus", "-b:a", "128k"}
	case "m4a":
		return []string{"-vn", "-c:a", "aac", "-b:a", "192k"}
	case "aiff":
		return []string{"-vn", "-c:a", "pcm_s16be"}
	}
	return nil
}

func ffmpegOutputArgs(to string, crf int) []string {
	switch to {
	case "mp4", "mov":
		return []string{
			"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(crf),
			"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart",
		}
	case "webm":
		return []string{"-c:v", "libvpx-vp9", "-crf", strconv.Itoa(crf), "-b:v", "0", "-row-mt", "1", "-c:a", "libopus", "-b:a", "128k"}
	case "mkv":
		return []string{"-map", "0", "-c", "copy"}
	case "gif":
		return []string{"-t", "30", "-vf", gifFilter}
	case "apng":
		return []string{"-plays", "0", "-f", "apng"}
	case "png":
		return []string{"-frames:v", "1"}
	default:
		return audioArgs(to)
	}
}

func ffmpegArgs(fromCat Category, to, in, out string, crf int) []string {
	base := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}
	// pw restricts ffmpeg to local file/pipe protocols for the untrusted input,
	// so a crafted media file (HLS/concat referencing http://, etc.) can't turn
	// a conversion into a server-side network fetch. It is an INPUT option, so
	// it must sit immediately before each user `-i` (ffmpeg resets input opts).
	pw := []string{"-protocol_whitelist", "file,pipe"}
	// Audio → mp4: a black video track carrying the audio, for players/sites
	// that only accept video.
	if fromCat == CategoryAudio && to == "mp4" {
		a := append(base, "-f", "lavfi", "-i", "color=c=black:s=1280x720:r=30")
		a = append(a, pw...)
		return append(a,
			"-i", in, "-map", "0:v", "-map", "1:a", "-shortest",
			"-c:v", "libx264", "-tune", "stillimage", "-crf", "28", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", out)
	}
	// Poster frame from a video: seek before the input, take one frame.
	if fromCat == CategoryVideo && (to == "jpg" || to == "png") {
		a := append(base, "-ss", "1")
		a = append(a, pw...)
		a = append(a, "-i", in, "-frames:v", "1")
		if to == "jpg" {
			a = append(a, "-q:v", "3")
		}
		return append(a, out)
	}
	a := append(base, pw...)
	a = append(a, "-i", in)
	a = append(a, ffmpegOutputArgs(to, crf)...)
	return append(a, out)
}

func pandocReader(f string) string {
	switch f {
	case "md", "txt":
		return "markdown"
	default:
		return f
	}
}

func pandocWriter(f string) string {
	switch f {
	case "md":
		return "gfm"
	case "txt":
		return "plain"
	default:
		return f
	}
}

func pandocArgs(from, to, in, out string) []string {
	return []string{"--sandbox", "-f", pandocReader(from), "-t", pandocWriter(to), "--standalone", "-o", out, in}
}

// buildPlan turns an edge into the ordered process specs that realise it.
// Table edges are handled natively (never here).
func buildPlan(e Edge, in, out, tmpDir, itemID string, o Options) ([]procSpec, error) {
	from, ok := FormatByID(e.From)
	if !ok {
		return nil, fmt.Errorf("unknown source format %q", e.From)
	}
	switch e.Kind {
	case KindImage:
		q := edgeInt(e, o, "quality", 85)
		return []procSpec{{tool: "magick", magick: true, args: imageArgs(e.From, in, out, q)}}, nil
	case KindRaw:
		q := edgeInt(e, o, "quality", 85)
		tiff := filepath.Join(tmpDir, itemID+".raw.tiff")
		return []procSpec{
			{tool: "dcraw_emu", args: []string{"-w", "-o", "1", "-q", "3", "-T", "-Z", "-", in}, stdoutPath: tiff},
			{tool: "magick", magick: true, args: []string{tiff, "-quality", strconv.Itoa(q), out}},
		}, nil
	case KindFFmpeg:
		crf := edgeInt(e, o, "crf", 23)
		return []procSpec{{tool: "ffmpeg", args: ffmpegArgs(from.Category, e.To, in, out, crf)}}, nil
	case KindPandoc:
		return []procSpec{{tool: "pandoc", args: pandocArgs(e.From, e.To, in, out)}}, nil
	case KindTable:
		return nil, errors.New("table conversions run natively, not through the runner")
	}
	return nil, fmt.Errorf("unknown conversion kind %q", e.Kind)
}

// ---- reference templates (catalog/documentation only) ----------------------

func imageTemplate(from, _ string) string {
	switch from {
	case "svg":
		return "magick -density 150 -background none {in} -auto-orient -quality {q} {out}"
	case "psd":
		return "magick {in}[0] -auto-orient -quality {q} {out}"
	default:
		return "magick {in} -auto-orient -quality {q} {out}"
	}
}

func rawTemplate(_ string) string {
	return "dcraw_emu -w -o 1 -q 3 -T -Z - {in} >{tmp}.tiff ; magick {tmp}.tiff -quality {q} {out}"
}

func ffmpegTemplate(fromCat Category, _, to string) string {
	if fromCat == CategoryAudio && to == "mp4" {
		return "ffmpeg -f lavfi -i color=c=black:s=1280x720:r=30 -i {in} -map 0:v -map 1:a -shortest -c:v libx264 -tune stillimage -crf 28 -pix_fmt yuv420p -c:a aac -b:a 192k -movflags +faststart {out}"
	}
	if fromCat == CategoryVideo && (to == "jpg" || to == "png") {
		return "ffmpeg -ss 1 -i {in} -frames:v 1 -q:v 3 {out}"
	}
	return "ffmpeg -nostdin -hide_banner -loglevel error -y -i {in} " + strings.Join(ffmpegOutputArgs(to, 23), " ") + " {out}"
}

func pandocTemplate(from, to string) string {
	return "pandoc --sandbox -f " + pandocReader(from) + " -t " + pandocWriter(to) + " --standalone -o {out} {in}"
}

// ---- capability probe ------------------------------------------------------

// Caps records which engines and codecs are installed right now.
type Caps struct {
	// SandboxBroken: root without setpriv — external-tool edges are refused.
	SandboxBroken bool
	FFmpeg        bool
	ImageMagick   bool
	Libraw        bool
	Pandoc        bool

	encoders    map[string]bool
	magickRead  map[string]bool
	magickWrite map[string]bool
}

var magickModeRe = regexp.MustCompile(`^[r-][w-][+-]$`)

func probe() *Caps {
	c := &Caps{encoders: map[string]bool{}, magickRead: map[string]bool{}, magickWrite: map[string]bool{}}
	if bin, err := exec.LookPath("magick"); err == nil {
		c.ImageMagick = true
		if out, err := exec.Command(bin, "-list", "format").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 3 {
					continue
				}
				name := strings.ToUpper(strings.TrimRight(fields[0], "*"))
				if name == "" {
					continue
				}
				for _, f := range fields[1:] {
					if magickModeRe.MatchString(f) {
						if f[0] == 'r' {
							c.magickRead[name] = true
						}
						if f[1] == 'w' {
							c.magickWrite[name] = true
						}
						break
					}
				}
			}
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		c.FFmpeg = true
		if out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				flags := fields[0]
				if r := flags[0]; r != 'V' && r != 'A' && r != 'S' {
					continue
				}
				c.encoders[fields[1]] = true
			}
		}
	}
	if _, err := exec.LookPath("dcraw_emu"); err == nil {
		c.Libraw = true
	}
	if _, err := exec.LookPath("pandoc"); err == nil {
		c.Pandoc = true
	}
	return c
}

func magickCoder(id string) string {
	if id == "jpg" {
		return "JPEG"
	}
	return strings.ToUpper(id)
}

func (c *Caps) magickReadable(id string) bool {
	if id == "svg" {
		return c.magickRead["SVG"] || c.magickRead["MSVG"]
	}
	return c.magickRead[magickCoder(id)]
}

func (c *Caps) magickWritable(id string) bool {
	if id == "svg" {
		return c.magickWrite["SVG"] || c.magickWrite["MSVG"]
	}
	return c.magickWrite[magickCoder(id)]
}

// requiredEncoders lists the ffmpeg encoders an edge needs beyond the always
// present ones (pcm, gif, apng, png, stream copy).
func requiredEncoders(e Edge) []string {
	if from, ok := FormatByID(e.From); ok && from.Category == CategoryAudio && e.To == "mp4" {
		return []string{"libx264", "aac"}
	}
	switch e.To {
	case "mp4", "mov":
		return []string{"libx264", "aac"}
	case "webm":
		return []string{"libvpx-vp9", "libopus"}
	case "mp3":
		return []string{"libmp3lame"}
	case "m4a":
		return []string{"aac"}
	case "ogg":
		return []string{"libvorbis"}
	case "opus":
		return []string{"libopus"}
	case "flac":
		return []string{"flac"}
	}
	return nil
}

// Available reports whether an edge can run right now, with a user-facing
// reason when it cannot.
func (c *Caps) Available(e Edge) (bool, string) {
	switch e.Kind {
	case KindTable:
		return true, ""
	}
	if c.SandboxBroken {
		return false, "the conversion sandbox is unavailable on this host (setpriv missing)"
	}
	switch e.Kind {
	case KindImage:
		if !c.ImageMagick {
			return false, "ImageMagick is not installed"
		}
		if !c.magickReadable(e.From) {
			return false, "ImageMagick has no " + strings.ToUpper(e.From) + " reader"
		}
		if !c.magickWritable(e.To) {
			return false, "ImageMagick has no " + strings.ToUpper(e.To) + " writer"
		}
		return true, ""
	case KindRaw:
		if !c.Libraw {
			return false, "libraw (dcraw_emu) is not installed"
		}
		if !c.ImageMagick {
			return false, "ImageMagick is not installed"
		}
		if !c.magickWritable(e.To) {
			return false, "ImageMagick has no " + strings.ToUpper(e.To) + " writer"
		}
		return true, ""
	case KindPandoc:
		if !c.Pandoc {
			return false, "pandoc is not installed"
		}
		return true, ""
	case KindFFmpeg:
		if !c.FFmpeg {
			return false, "ffmpeg is not installed"
		}
		for _, enc := range requiredEncoders(e) {
			if !c.encoders[enc] {
				return false, "ffmpeg has no " + enc + " encoder"
			}
		}
		return true, ""
	}
	return false, "unsupported conversion"
}

// capsCache re-probes lazily (~5 min) so a freshly installed tool is picked up
// without restarting the server.
type capsCache struct {
	mu   sync.Mutex
	caps *Caps
	at   time.Time
}

func (c *capsCache) get() *Caps {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.caps == nil || time.Since(c.at) > 5*time.Minute {
		c.caps = probe()
		c.at = time.Now()
	}
	return c.caps
}

// ---- privilege-dropped, group-killed execution -----------------------------

// Runner executes conversion plans. When drop is set, each tool is launched
// through setpriv, which drops to nobody AFTER the (root) process has already
// chdir'd into the batch dir — so the tool addresses its files by paths
// relative to that dir and never has to traverse the 0700 DataDir ancestors as
// nobody. In a non-root dev environment (or when setpriv is missing) drop is
// false and tools run as the current user.
type Runner struct {
	drop bool
	// sandboxBroken means the drop was WANTED (we are root, parsing untrusted
	// bytes) but cannot be applied (setpriv missing). The sandbox fails
	// CLOSED: external-tool conversions are refused rather than run as root.
	sandboxBroken bool
	logger        *slog.Logger
	nicePath      string
	ionicePath    string
	setprivPath   string
	unsharePath   string
	// netns runs each tool in a fresh (network-less) namespace via `unshare
	// --net`, so no converter can make ANY outbound connection regardless of
	// input tricks — defense-in-depth for the SSRF surface. Enabled only when a
	// startup probe confirms the kernel/capabilities allow it, so hosts that
	// can't (unprivileged containers) simply fall back to the UID-drop sandbox.
	netns bool

	mu    sync.Mutex
	tools map[string]string
}

// SandboxBroken reports whether external-tool conversions are refused because
// the privilege drop is unavailable (root without setpriv).
func (r *Runner) SandboxBroken() bool { return r.sandboxBroken }

// NewRunner builds a runner. It resolves nice/ionice/setpriv once. wantDrop is
// the caller's intent (true when the process is root); the actual drop also
// requires setpriv to exist.
func NewRunner(wantDrop bool, logger *slog.Logger) *Runner {
	r := &Runner{logger: logger, tools: map[string]string{}}
	if p, err := exec.LookPath("nice"); err == nil {
		r.nicePath = p
	}
	if p, err := exec.LookPath("ionice"); err == nil {
		r.ionicePath = p
	}
	if p, err := exec.LookPath("setpriv"); err == nil {
		r.setprivPath = p
	}
	if wantDrop {
		if r.setprivPath != "" {
			r.drop = true
		} else {
			// Fail CLOSED: running converters as root on untrusted uploads is
			// the exact risk the sandbox exists to prevent.
			r.sandboxBroken = true
			logger.Error("setpriv not found: refusing external-tool conversions (install util-linux); table conversions still work")
		}
	}
	// Network-namespace isolation is best-effort: enable it only if `unshare
	// --net` actually works here (root + CAP_SYS_ADMIN, not blocked by the
	// container runtime). Otherwise the UID-drop + ffmpeg protocol whitelist
	// remain the sandbox.
	if r.drop {
		if p, err := exec.LookPath("unshare"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			probe := exec.CommandContext(ctx, p, "--net", "true")
			if probe.Run() == nil {
				r.unsharePath = p
				r.netns = true
				logger.Info("convert sandbox: network isolation enabled (unshare --net)")
			} else {
				logger.Info("convert sandbox: network isolation unavailable; relying on UID drop + ffmpeg protocol whitelist")
			}
			cancel()
		}
	}
	return r
}

func (r *Runner) toolPath(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.tools[name]; ok {
		if p == "" {
			return "", fmt.Errorf("%s not found in PATH", name)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		r.tools[name] = ""
		return "", fmt.Errorf("%s not found in PATH", name)
	}
	r.tools[name] = p
	return p, nil
}

// Convert runs every process for an edge in order. workDir is the absolute
// batch directory the tools run inside (their cwd); inRel/outRel/tmpRel and the
// plan's paths are all relative to it. The caller supplies a ctx whose deadline
// is the per-item timeout; on expiry the tool's whole process group is SIGKILLed.
func (r *Runner) Convert(ctx context.Context, e Edge, workDir, inRel, outRel, tmpRel, itemID string, o Options) error {
	specs, err := buildPlan(e, inRel, outRel, tmpRel, itemID, o)
	if err != nil {
		return err
	}
	for _, s := range specs {
		if err := r.run(ctx, s, workDir); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) run(ctx context.Context, spec procSpec, workDir string) error {
	if r.sandboxBroken {
		return fmt.Errorf("conversion sandbox unavailable on this host (setpriv missing) — external-tool conversions are disabled")
	}
	bin, err := r.toolPath(spec.tool)
	if err != nil {
		return err
	}

	// argv = [nice -n 10] [ionice -c3] [setpriv --reuid … --regid … …] <bin> <args…>.
	// nice/ionice/setpriv all run as root (Go chdir's into workDir as root first);
	// setpriv then drops to nobody and execs the tool with the cwd already set.
	// Absolute binary paths, relative file args, and never a shell.
	var argv []string
	if r.netns {
		// Outermost: create the network-less namespace as root, before the
		// nice/ionice/setpriv chain execs within it.
		argv = append(argv, r.unsharePath, "--net")
	}
	if r.nicePath != "" {
		argv = append(argv, r.nicePath, "-n", "10")
	}
	if r.ionicePath != "" {
		argv = append(argv, r.ionicePath, "-c3")
	}
	if r.drop {
		argv = append(argv, r.setprivPath,
			"--reuid", strconv.Itoa(dropUID), "--regid", strconv.Itoa(dropGID),
			"--clear-groups", "--no-new-privs")
	}
	argv = append(argv, bin)
	argv = append(argv, spec.args...)

	// Bound concurrent external processes across all batches.
	select {
	case procSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-procSem }()

	cmd := exec.Command(argv[0], argv[1:]...)
	// chdir happens as root (no Credential here); setpriv does the drop after,
	// so the sandboxed tool never traverses the DataDir ancestors as nobody.
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = spec.env()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if spec.stdoutPath != "" {
		// Opened (as root) at its absolute path; the dropped child writes
		// through the inherited fd, so it needs no path permission of its own.
		f, ferr := os.Create(filepath.Join(workDir, spec.stdoutPath))
		if ferr != nil {
			return ferr
		}
		defer f.Close()
		cmd.Stdout = f
	} else {
		cmd.Stdout = io.Discard
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", spec.tool, err)
	}
	// With Setpgid the child leads its own group; pgid == child pid.
	pgid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s timed out", spec.tool)
		}
		return ctx.Err()
	case werr := <-done:
		if werr != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = werr.Error()
			}
			if len(msg) > 400 {
				msg = msg[:400] + "…"
			}
			return fmt.Errorf("%s: %s", spec.tool, oneLine(msg))
		}
		return nil
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}
