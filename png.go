package comfymeta

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// ---------------------------------------------------------------------------
// PNG: tEXt / zTXt / iTXt chunks
// ---------------------------------------------------------------------------

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func readPNGChunks(path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sig := make([]byte, 8)
	if _, err := io.ReadFull(f, sig); err != nil {
		return nil, errors.New("cannot read: file too short")
	}
	if !bytes.Equal(sig, pngSignature) {
		return nil, errors.New("not a PNG file")
	}

	var chunks []Chunk
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		length := binary.BigEndian.Uint32(hdr[:4])
		ctype := string(hdr[4:8])
		switch ctype {
		case "tEXt", "zTXt", "iTXt":
			data := make([]byte, length)
			if _, err := io.ReadFull(f, data); err != nil {
				return nil, err
			}
			if _, err := f.Seek(4, io.SeekCurrent); err != nil {
				return nil, err
			}
			if c, ok := parsePNGTextChunk(ctype, data); ok {
				chunks = append(chunks, c)
			}
		case "IEND":
			return chunks, nil
		default:
			if _, err := f.Seek(int64(length)+4, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
	return chunks, nil
}

func parsePNGTextChunk(ctype string, data []byte) (Chunk, bool) {
	switch ctype {
	case "tEXt":
		i := bytes.IndexByte(data, 0)
		if i < 0 {
			return Chunk{}, false
		}
		return Chunk{Keyword: string(data[:i]), Text: string(data[i+1:])}, true
	case "zTXt":
		i := bytes.IndexByte(data, 0)
		if i < 0 || i+2 > len(data) {
			return Chunk{}, false
		}
		text, err := zlibDecompress(data[i+2:])
		if err != nil {
			return Chunk{}, false
		}
		return Chunk{Keyword: string(data[:i]), Text: text}, true
	case "iTXt":
		i := bytes.IndexByte(data, 0)
		if i < 0 || i+3 > len(data) {
			return Chunk{}, false
		}
		keyword := string(data[:i])
		rest := data[i+1:]
		compFlag := rest[0]
		rest = rest[2:]
		if j := bytes.IndexByte(rest, 0); j >= 0 {
			rest = rest[j+1:]
		} else {
			return Chunk{}, false
		}
		if k := bytes.IndexByte(rest, 0); k >= 0 {
			rest = rest[k+1:]
		} else {
			return Chunk{}, false
		}
		text := string(rest)
		if compFlag == 1 {
			t, err := zlibDecompress(rest)
			if err != nil {
				return Chunk{}, false
			}
			text = t
		}
		return Chunk{Keyword: keyword, Text: text}, true
	}
	return Chunk{}, false
}

func zlibDecompress(b []byte) (string, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
