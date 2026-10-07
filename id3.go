package comfymeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// ---------------------------------------------------------------------------
// MP3: ID3v2 tag at the start of the file ("prompt" / "workflow" TXXX frames)
// ---------------------------------------------------------------------------

func readID3Chunks(path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hdr := make([]byte, 10)
	if _, err := io.ReadFull(f, hdr); err != nil || string(hdr[:3]) != "ID3" {
		return nil, nil // no ID3v2 tag
	}
	major, flags := hdr[3], hdr[5]
	if major < 2 || major > 4 {
		return nil, fmt.Errorf("unsupported ID3v2.%d version", major)
	}
	tag := make([]byte, syncsafe(hdr[6:10]))
	if _, err := io.ReadFull(f, tag); err != nil {
		return nil, errors.New("truncated ID3 tag")
	}

	// Tag-level unsynchronisation (v2.2/v2.3; v2.4 applies it per frame).
	if flags&0x80 != 0 && major < 4 {
		tag = unsynchronise(tag)
	}
	p := 0
	if flags&0x40 != 0 && major >= 3 { // extended header
		if len(tag) < 4 {
			return nil, nil
		}
		if major == 4 {
			p = syncsafe(tag[:4]) // size includes these 4 bytes
		} else {
			p = 4 + int(binary.BigEndian.Uint32(tag[:4]))
		}
	}

	idLen, hdrLen := 4, 10
	if major == 2 {
		idLen, hdrLen = 3, 6
	}
	var chunks []Chunk
	for p+hdrLen <= len(tag) {
		id := string(tag[p : p+idLen])
		if tag[p] == 0 {
			break // padding
		}
		var size int
		var fflags uint16
		switch major {
		case 2:
			size = int(tag[p+3])<<16 | int(tag[p+4])<<8 | int(tag[p+5])
		case 3:
			size = int(binary.BigEndian.Uint32(tag[p+4 : p+8]))
			fflags = binary.BigEndian.Uint16(tag[p+8 : p+10])
		case 4:
			size = syncsafe(tag[p+4 : p+8])
			// some encoders write a non-syncsafe size in v2.4
			if (tag[p+4]|tag[p+5]|tag[p+6]|tag[p+7])&0x80 != 0 {
				size = int(binary.BigEndian.Uint32(tag[p+4 : p+8]))
			}
			fflags = binary.BigEndian.Uint16(tag[p+8 : p+10])
		}
		p += hdrLen
		if size < 0 || p+size > len(tag) {
			break
		}
		body := tag[p : p+size]
		p += size

		body, ok := id3FrameBody(major, fflags, body)
		if !ok {
			continue
		}
		if c, ok := parseID3TextFrame(id, body); ok {
			chunks = append(chunks, c)
		}
	}
	return chunks, nil
}

// id3FrameBody strips a frame's optional headers and decodes it
// (unsynchronisation, zlib compression). Encrypted frames are skipped.
func id3FrameBody(major byte, fflags uint16, body []byte) ([]byte, bool) {
	var compressed, encrypted bool
	switch major {
	case 3:
		compressed, encrypted = fflags&0x0080 != 0, fflags&0x0040 != 0
		if compressed { // decompressed size (4)
			if len(body) < 4 {
				return nil, false
			}
			body = body[4:]
		}
		if encrypted { // method (1)
			return nil, false
		}
		if fflags&0x0020 != 0 { // group identifier (1)
			if len(body) < 1 {
				return nil, false
			}
			body = body[1:]
		}
	case 4:
		compressed, encrypted = fflags&0x0008 != 0, fflags&0x0004 != 0
		if encrypted {
			return nil, false
		}
		if fflags&0x0040 != 0 { // group identifier (1)
			if len(body) < 1 {
				return nil, false
			}
			body = body[1:]
		}
		if fflags&0x0001 != 0 { // data length indicator (4)
			if len(body) < 4 {
				return nil, false
			}
			body = body[4:]
		}
		if fflags&0x0002 != 0 {
			body = unsynchronise(body)
		}
	}
	if compressed {
		s, err := zlibDecompress(body)
		if err != nil {
			return nil, false
		}
		body = []byte(s)
	}
	return body, true
}

// parseID3TextFrame: TXXX/TXX -> (description, value); other T*** -> (id, text).
func parseID3TextFrame(id string, body []byte) (Chunk, bool) {
	if len(body) < 1 || id[0] != 'T' {
		return Chunk{}, false
	}
	enc, rest := body[0], body[1:]
	if id == "TXXX" || id == "TXX" {
		desc, value := splitID3String(enc, rest)
		return Chunk{Keyword: decodeID3Text(enc, desc), Text: decodeID3Text(enc, value)}, true
	}
	return Chunk{Keyword: id, Text: decodeID3Text(enc, rest)}, true
}

// splitID3String splits at the first terminator (1 null byte, or 2 aligned ones in UTF-16).
func splitID3String(enc byte, b []byte) ([]byte, []byte) {
	if enc == 1 || enc == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return b[:i], b[i+2:]
			}
		}
		return b, nil
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i], b[i+1:]
	}
	return b, nil
}

// decodeID3Text: 0 = ISO-8859-1, 1 = UTF-16 with BOM, 2 = UTF-16BE, 3 = UTF-8.
func decodeID3Text(enc byte, b []byte) string {
	switch enc {
	case 0:
		r := make([]rune, 0, len(b))
		for _, c := range b {
			r = append(r, rune(c))
		}
		return strings.TrimRight(string(r), "\x00")
	case 1, 2:
		bigEndian := enc == 2
		if len(b) >= 2 {
			switch {
			case b[0] == 0xFF && b[1] == 0xFE:
				bigEndian, b = false, b[2:]
			case b[0] == 0xFE && b[1] == 0xFF:
				bigEndian, b = true, b[2:]
			}
		}
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if bigEndian {
				u = append(u, binary.BigEndian.Uint16(b[i:]))
			} else {
				u = append(u, binary.LittleEndian.Uint16(b[i:]))
			}
		}
		return strings.TrimRight(string(utf16.Decode(u)), "\x00")
	default:
		return strings.TrimRight(string(b), "\x00")
	}
}

func syncsafe(b []byte) int {
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}

// unsynchronise reverses the ID3 unsynchronisation scheme (0xFF 0x00 -> 0xFF).
func unsynchronise(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xFF && i+1 < len(b) && b[i+1] == 0x00 {
			i++
		}
	}
	return out
}
