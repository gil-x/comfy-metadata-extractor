// Package comfymeta extracts the metadata ComfyUI embeds in PNG images, MP4
// videos and MP3 audio files: the "prompt" JSON (API-format graph) and the
// "workflow" JSON (editor graph), and derives the usual generation parameters
// from them (model, prompts, seed…).
//
//   - PNG: tEXt/zTXt/iTXt text chunks.
//   - MP4: QuickTime/ISO metadata table (moov/udta/meta: keys + ilst).
//   - MP3: ID3v2 tag (TXXX frames, v2.2 to v2.4).
//
// No external dependencies: standard library only.
package comfymeta

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Reading metadata: PNG / MP4 / MP3 dispatch
// ---------------------------------------------------------------------------

// Chunk is a text metadata entry: a key (e.g. "prompt", "workflow") and its
// raw value.
type Chunk struct {
	Keyword string
	Text    string
}

// Read reads the text metadata of a PNG, MP4/MOV or MP3 file. The format is
// detected from the file signature, falling back to the extension. A file
// without metadata yields an empty list and a nil error.
func Read(path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 12)
	n, _ := io.ReadFull(f, head)
	f.Close()

	switch {
	case n >= 8 && bytes.Equal(head[:8], pngSignature):
		return readPNGChunks(path)
	case n >= 8 && string(head[4:8]) == "ftyp":
		return readMP4Chunks(path)
	case n >= 3 && string(head[:3]) == "ID3":
		return readID3Chunks(path)
	}
	// fall back to the extension
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return readPNGChunks(path)
	case ".mp4", ".mov", ".m4v":
		return readMP4Chunks(path)
	case ".mp3":
		return nil, nil // MP3 without an ID3v2 tag: no metadata
	}
	return nil, errors.New("unrecognized format (not PNG, MP4 or MP3)")
}

// Find returns the first chunk whose key is keyword.
func Find(chunks []Chunk, keyword string) (Chunk, bool) {
	for _, c := range chunks {
		if c.Keyword == keyword {
			return c, true
		}
	}
	return Chunk{}, false
}

// Supported reports whether the file extension is one of the readable formats.
func Supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".mp4", ".mov", ".m4v", ".mp3":
		return true
	}
	return false
}
