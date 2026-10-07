// Command comfymeta extracts ComfyUI metadata from PNG images, MP4 videos and
// MP3 audio files.
//
// Usage:
//
//	comfymeta [options] <path> [more paths ...]
//
// A path can be a file (.png/.mp4/.mov/.mp3) or a directory (every supported
// file in it is processed; -r to include subdirectories).
//
// Content (WHAT to print):
//
//	(default)   human-readable summary of the generation parameters
//	-summary    force the human-readable summary
//	-json       extracted parameters as JSON
//	-prompt     the "prompt" JSON (indented)
//	-workflow   the "workflow" JSON (indented)
//	-raw        every raw metadata entry
//	-keys       list the metadata keys present
//
// Destination (WHERE to write):
//
//	(default)   standard output
//	-export     write to a file named after the source
//	            (e.g. clip_00254_.mp4 -> clip_00254_.json)
//	-outdir DIR put exported files in DIR
//	-r          walk subdirectories recursively
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	comfymeta "github.com/gil-x/comfy-metadata-extractor"
)

// ---------------------------------------------------------------------------
// Content modes
// ---------------------------------------------------------------------------

type mode int

const (
	modeSummary mode = iota
	modeJSON
	modePrompt
	modeWorkflow
	modeRaw
	modeKeys
)

func modeSuffix(m mode) string {
	switch m {
	case modeJSON:
		return ".json"
	case modePrompt:
		return ".prompt.json"
	case modeWorkflow:
		return ".workflow.json"
	case modeRaw:
		return ".raw.txt"
	case modeKeys:
		return ".keys.txt"
	default:
		return ".summary.txt"
	}
}

func main() {
	var (
		fSummary  = flag.Bool("summary", false, "force the human-readable summary")
		fJSON     = flag.Bool("json", false, "extracted parameters as JSON")
		fPrompt   = flag.Bool("prompt", false, `the "prompt" JSON, indented`)
		fWorkflow = flag.Bool("workflow", false, `the "workflow" JSON, indented`)
		fRaw      = flag.Bool("raw", false, "every raw metadata entry")
		fKeys     = flag.Bool("keys", false, "list the metadata keys")

		fExport = flag.Bool("export", false, "write to a file named after the source")
		fOutdir = flag.String("outdir", "", "destination `DIR` for exported files")
		fRec    = flag.Bool("r", false, "walk subdirectories recursively")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "comfymeta — extract ComfyUI metadata from PNG images, MP4 videos and MP3 audio\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <path> [more paths ...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A path can be a .png/.mp4/.mov/.mp3 file or a directory.\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	m := modeSummary
	defaultUsed := true
	switch {
	case *fKeys:
		m, defaultUsed = modeKeys, false
	case *fRaw:
		m, defaultUsed = modeRaw, false
	case *fPrompt:
		m, defaultUsed = modePrompt, false
	case *fWorkflow:
		m, defaultUsed = modeWorkflow, false
	case *fJSON:
		m, defaultUsed = modeJSON, false
	case *fSummary:
		m, defaultUsed = modeSummary, false
	}
	if defaultUsed && *fExport {
		m = modeJSON
	}

	if *fOutdir != "" {
		if err := os.MkdirAll(*fOutdir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "outdir: %v\n", err)
			os.Exit(1)
		}
	}

	files, statErrs := gatherInputs(args, *fRec)
	exitCode := 0
	for _, e := range statErrs {
		fmt.Fprintln(os.Stderr, e)
		exitCode = 1
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no files to process")
		os.Exit(1)
	}

	multiple := len(files) > 1
	headerMode := m == modeSummary || m == modeRaw || m == modeKeys

	for _, path := range files {
		chunks, err := comfymeta.Read(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s : %v\n", path, err)
			exitCode = 1
			continue
		}

		content, ok := render(m, path, chunks)

		if *fExport {
			if !ok {
				fmt.Fprintf(os.Stderr, "%s: skipped (no usable metadata)\n", path)
				continue
			}
			outPath := exportPath(path, modeSuffix(m), *fOutdir)
			if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "%s: cannot write: %v\n", outPath, err)
				exitCode = 1
				continue
			}
			fmt.Printf("%s → %s\n", path, outPath)
			continue
		}

		if multiple && headerMode {
			fmt.Printf("===== %s =====\n", path)
		}
		fmt.Print(content)
		if multiple && headerMode {
			fmt.Println()
		}
	}
	os.Exit(exitCode)
}

func render(m mode, path string, chunks []comfymeta.Chunk) (string, bool) {
	switch m {
	case modeKeys:
		if len(chunks) == 0 {
			return "(no metadata)\n", false
		}
		var b strings.Builder
		for _, c := range chunks {
			b.WriteString(c.Keyword + "\n")
		}
		return b.String(), true

	case modeRaw:
		if len(chunks) == 0 {
			return "(no metadata)\n", false
		}
		var b strings.Builder
		for _, c := range chunks {
			fmt.Fprintf(&b, "--- %s ---\n%s\n", c.Keyword, c.Text)
		}
		return b.String(), true

	case modePrompt:
		return jsonChunk(chunks, "prompt")

	case modeWorkflow:
		return jsonChunk(chunks, "workflow")

	case modeJSON:
		p := comfymeta.Extract(path, chunks)
		out, _ := json.MarshalIndent(p, "", "  ")
		_, has := comfymeta.Find(chunks, "prompt")
		return string(out) + "\n", has

	default:
		return summaryString(path, chunks)
	}
}

func jsonChunk(chunks []comfymeta.Chunk, keyword string) (string, bool) {
	c, ok := comfymeta.Find(chunks, keyword)
	if !ok {
		return fmt.Sprintf("(no %q metadata)\n", keyword), false
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(c.Text), "", "  ") == nil {
		return pretty.String() + "\n", true
	}
	return c.Text + "\n", true
}

// ---------------------------------------------------------------------------
// Gathering inputs
// ---------------------------------------------------------------------------

func gatherInputs(args []string, recursive bool) ([]string, []string) {
	var out []string
	var errs []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s : %v", a, err))
			continue
		}
		if !info.IsDir() {
			add(a)
			continue
		}
		var found []string
		if recursive {
			_ = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && comfymeta.Supported(p) {
					found = append(found, p)
				}
				return nil
			})
		} else {
			entries, err := os.ReadDir(a)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s : %v", a, err))
				continue
			}
			for _, e := range entries {
				if !e.IsDir() && comfymeta.Supported(e.Name()) {
					found = append(found, filepath.Join(a, e.Name()))
				}
			}
		}
		sort.Strings(found)
		for _, p := range found {
			add(p)
		}
	}
	return out, errs
}

func exportPath(src, suffix, outdir string) string {
	dir := filepath.Dir(src)
	if outdir != "" {
		dir = outdir
	}
	base := filepath.Base(src)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(dir, base+suffix)
}

// ---------------------------------------------------------------------------
// Human-readable summary
// ---------------------------------------------------------------------------

func summaryString(path string, chunks []comfymeta.Chunk) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "File: %s\n", path)

	if _, ok := comfymeta.Find(chunks, "prompt"); !ok {
		if len(chunks) == 0 {
			b.WriteString("  (no metadata — not a ComfyUI output?)\n")
		} else {
			kw := make([]string, len(chunks))
			for i, c := range chunks {
				kw[i] = c.Keyword
			}
			fmt.Fprintf(&b, "  Keys present: %s\n", strings.Join(kw, ", "))
		}
		return b.String(), false
	}

	p := comfymeta.Extract(path, chunks)
	line := func(label, val string) {
		if val != "" {
			// align the following lines of a multi-line value under the first one
			val = strings.ReplaceAll(strings.TrimSpace(val), "\n", "\n"+strings.Repeat(" ", 19))
			fmt.Fprintf(&b, "  %-16s %s\n", label+":", val)
		}
	}
	iline := func(label string, v *int64) {
		if v != nil {
			line(label, strconv.FormatInt(*v, 10))
		}
	}
	fline := func(label string, v *float64) {
		if v != nil {
			line(label, strconv.FormatFloat(*v, 'g', -1, 64))
		}
	}

	if p.MediaType == "video" {
		line("Type", "video")
		line("Checkpoint", p.Model)
		line("Distilled LoRA", p.DistilledLora)
		line("Text encoder", p.TextEncoder)
		line("Latent upscaler", p.LatentUpscaleModel)
		if len(p.LoadedImages) > 0 {
			line("Source image", strings.Join(p.LoadedImages, ", "))
		}
		line("Positive", p.Positive)
		line("Negative", p.Negative)
		iline("Noise seed", p.Seed)
		if p.Width != nil && p.Height != nil {
			line("Dimensions", fmt.Sprintf("%d x %d", *p.Width, *p.Height))
		}
		iline("Length", p.Length)
		iline("FPS", p.Fps)
		return b.String(), true
	}

	if p.MediaType == "audio" {
		line("Type", "audio")
		line("Model", p.Model)
		if len(p.LoadedAudio) > 0 {
			line("Source audio", strings.Join(p.LoadedAudio, ", "))
		}
		line("Tags", p.Tags)
		if p.Positive != p.Tags {
			line("Positive", p.Positive)
		}
		line("Lyrics", p.Lyrics)
		fline("Duration (s)", p.Duration)
		iline("BPM", p.BPM)
		line("Time signature", p.TimeSignature)
		line("Key", p.KeyScale)
		line("Language", p.Language)
		iline("Seed", p.Seed)
		iline("Steps", p.Steps)
		fline("CFG", p.CFG)
		line("Sampler", p.Sampler)
		line("Scheduler", p.Scheduler)
		fline("Denoise", p.Denoise)
		return b.String(), true
	}

	// image
	line("Type", "image")
	line("Model", p.Model)
	if len(p.LoadedImages) > 0 {
		line("Source image", strings.Join(p.LoadedImages, ", "))
	}
	line("Positive", p.Positive)
	line("Negative", p.Negative)
	iline("Seed", p.Seed)
	iline("Steps", p.Steps)
	fline("CFG", p.CFG)
	line("Sampler", p.Sampler)
	line("Scheduler", p.Scheduler)
	fline("Denoise", p.Denoise)
	if p.Width != nil && p.Height != nil {
		line("Dimensions", fmt.Sprintf("%d x %d", *p.Width, *p.Height))
	}
	return b.String(), true
}
