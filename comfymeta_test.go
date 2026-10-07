package comfymeta

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecimens(t *testing.T) {
	tests := []struct {
		file      string
		mediaType string
		model     string
		positive  string
		seed      int64
		check     func(t *testing.T, p Params)
	}{
		{
			file:      "specimen.png",
			mediaType: "image",
			model:     "sd_xl_base_1.0.safetensors",
			positive:  "claymation, man",
			seed:      833437719952121,
			check: func(t *testing.T, p Params) {
				if p.Steps == nil || *p.Steps != 20 || p.Sampler != "dpmpp_2m" || p.Scheduler != "karras" {
					t.Errorf("unexpected KSampler: steps=%v sampler=%q scheduler=%q", p.Steps, p.Sampler, p.Scheduler)
				}
			},
		},
		{
			file:      "specimen.mp4",
			mediaType: "video",
			model:     "10Eros_v1-fp8mixed_learned.safetensors",
			positive:  `Character asks "You're talking to me?"`,
			seed:      529889386378618,
			check: func(t *testing.T, p Params) {
				if p.Width == nil || *p.Width != 640 || p.Height == nil || *p.Height != 480 {
					t.Errorf("unexpected dimensions: %v x %v", p.Width, p.Height)
				}
				if p.Fps == nil || *p.Fps != 24 || p.Length == nil || *p.Length != 48 {
					t.Errorf("unexpected fps/length: %v / %v", p.Fps, p.Length)
				}
			},
		},
		{
			file:      "specimen.mp3",
			mediaType: "audio",
			model:     "ace_step_1.5_turbo_aio.safetensors",
			positive:  "industrial techno",
			seed:      712861949169538,
			check: func(t *testing.T, p Params) {
				if p.BPM == nil || *p.BPM != 128 || p.KeyScale != "F minor" {
					t.Errorf("unexpected BPM/key: %v / %q", p.BPM, p.KeyScale)
				}
				if p.Duration == nil || *p.Duration != 40 {
					t.Errorf("unexpected duration: %v", p.Duration)
				}
				if len(p.LoadedAudio) != 1 || p.LoadedAudio[0] != "Painkiller - Forest fight.mp3" {
					t.Errorf("unexpected source audio: %v", p.LoadedAudio)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("testdata", tt.file)
			chunks, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"prompt", "workflow"} {
				if _, ok := Find(chunks, key); !ok {
					t.Errorf("missing %q chunk", key)
				}
			}
			p := Extract(path, chunks)
			if p.MediaType != tt.mediaType {
				t.Errorf("media_type = %q, want %q", p.MediaType, tt.mediaType)
			}
			if p.Model != tt.model {
				t.Errorf("model = %q, want %q", p.Model, tt.model)
			}
			if !strings.HasPrefix(p.Positive, tt.positive) {
				t.Errorf("positive = %q, want prefix %q", p.Positive, tt.positive)
			}
			if p.Seed == nil || *p.Seed != tt.seed {
				t.Errorf("seed = %v, want %d", p.Seed, tt.seed)
			}
			tt.check(t, p)
		})
	}
}

// id3Tag builds a minimal ID3v2 tag from already-encoded frames.
func id3Tag(major byte, frames ...[]byte) []byte {
	body := bytes.Join(frames, nil)
	n := len(body)
	hdr := []byte{'I', 'D', '3', major, 0, 0,
		byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(hdr, body...)
}

func id3v23Frame(id string, payload []byte) []byte {
	n := len(payload)
	f := []byte(id)
	f = append(f, byte(n>>24), byte(n>>16), byte(n>>8), byte(n), 0, 0)
	return append(f, payload...)
}

func TestID3Encodings(t *testing.T) {
	// TXXX in UTF-16 with BOM (encoding 1), as ffmpeg writes it in ID3v2.3.
	utf16le := func(s string) []byte {
		b := []byte{0xFF, 0xFE}
		for _, r := range s {
			b = append(b, byte(r), byte(r>>8))
		}
		return b
	}
	txxx := append([]byte{1}, utf16le("prompt")...)
	txxx = append(txxx, 0, 0)
	txxx = append(txxx, utf16le(`{"é":1}`)...)

	// TXXX in ISO-8859-1 (encoding 0).
	latin := append([]byte{0}, []byte("workflow\x00caf\xe9")...)

	path := filepath.Join(t.TempDir(), "t.mp3")
	data := id3Tag(3, id3v23Frame("TXXX", txxx), id3v23Frame("TXXX", latin), make([]byte, 16))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := Find(chunks, "prompt"); !ok || c.Text != `{"é":1}` {
		t.Errorf("prompt UTF-16 = %q, %v", c.Text, ok)
	}
	if c, ok := Find(chunks, "workflow"); !ok || c.Text != "café" {
		t.Errorf("workflow latin-1 = %q, %v", c.Text, ok)
	}
}

func TestUnsynchronise(t *testing.T) {
	got := unsynchronise([]byte{0xFF, 0x00, 0xE0, 0x01, 0xFF, 0x00, 0x00})
	want := []byte{0xFF, 0xE0, 0x01, 0xFF, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("unsynchronise = % x, want % x", got, want)
	}
}

func TestReadWithoutMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.mp3")
	if err := os.WriteFile(path, []byte{0xFF, 0xFB, 0x90, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := Read(path)
	if err != nil || len(chunks) != 0 {
		t.Errorf("MP3 without a tag: chunks=%v err=%v", chunks, err)
	}
}
