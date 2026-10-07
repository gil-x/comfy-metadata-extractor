package comfymeta

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// ---------------------------------------------------------------------------
// MP4: QuickTime/ISO metadata table (moov/udta/meta: keys + ilst)
// ---------------------------------------------------------------------------

type box struct {
	typ          string
	payloadStart int
	end          int
}

func boxesIn(data []byte, start, end int) []box {
	var bs []box
	off := start
	for off+8 <= end {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		typ := string(data[off+4 : off+8])
		hdr := 8
		switch {
		case size == 1:
			if off+16 > end {
				return bs
			}
			size = int(binary.BigEndian.Uint64(data[off+8 : off+16]))
			hdr = 16
		case size == 0:
			size = end - off
		}
		if size < hdr || off+size > end {
			return bs
		}
		bs = append(bs, box{typ: typ, payloadStart: off + hdr, end: off + size})
		off += size
	}
	return bs
}

func findBox(data []byte, start, end int, typ string) (box, bool) {
	for _, b := range boxesIn(data, start, end) {
		if b.typ == typ {
			return b, true
		}
	}
	return box{}, false
}

func findBoxPath(data []byte, path ...string) (box, bool) {
	start, end := 0, len(data)
	var cur box
	for _, want := range path {
		b, ok := findBox(data, start, end, want)
		if !ok {
			return box{}, false
		}
		cur = b
		start, end = b.payloadStart, b.end
	}
	return cur, true
}

func readMP4Chunks(path string) ([]Chunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return nil, errors.New("unrecognized MP4 container")
	}
	meta, ok := findBoxPath(data, "moov", "udta", "meta")
	if !ok {
		return nil, nil // no embedded metadata
	}

	// The 'meta' box is sometimes a FullBox (4 bytes of version/flags before
	// the children, ISO/ffmpeg style) and sometimes not (QuickTime style). We
	// detect which by looking for the 'hdlr' box that almost always opens it.
	childStart := meta.payloadStart
	if isTypeAt(data, meta.payloadStart, meta.end, "hdlr") {
		// QuickTime: children start right away
	} else if isTypeAt(data, meta.payloadStart+4, meta.end, "hdlr") {
		childStart = meta.payloadStart + 4 // ISO: skip version/flags
	} else {
		childStart = meta.payloadStart + 4 // default: ISO
	}

	keysBox, hasKeys := findBox(data, childStart, meta.end, "keys")
	ilstBox, hasIlst := findBox(data, childStart, meta.end, "ilst")
	if !hasKeys || !hasIlst {
		return nil, nil
	}
	keys := parseKeysBox(data, keysBox.payloadStart, keysBox.end)
	return parseIlstBox(data, ilstBox.payloadStart, ilstBox.end, keys), nil
}

func isTypeAt(data []byte, off, end int, typ string) bool {
	return off+8 <= end && string(data[off+4:off+8]) == typ
}

// parseKeysBox: version/flags(4) count(4) [size(4) namespace(4) key(...)]*
func parseKeysBox(data []byte, start, end int) []string {
	p := start
	if p+8 > end {
		return nil
	}
	p += 4 // version/flags
	count := int(binary.BigEndian.Uint32(data[p : p+4]))
	p += 4
	keys := make([]string, 0, count)
	for i := 0; i < count && p+8 <= end; i++ {
		sz := int(binary.BigEndian.Uint32(data[p : p+4]))
		if sz < 8 || p+sz > end {
			break
		}
		keys = append(keys, string(data[p+8:p+sz])) // skip size(4)+namespace(4)
		p += sz
	}
	return keys
}

// parseIlstBox: [itemSize(4) index(4) | data box]*
// data box: size(4) 'data'(4) type(4) locale(4) payload
func parseIlstBox(data []byte, start, end int, keys []string) []Chunk {
	var out []Chunk
	p := start
	for p+8 <= end {
		isize := int(binary.BigEndian.Uint32(data[p : p+4]))
		if isize < 8 || p+isize > end {
			break
		}
		index := int(binary.BigEndian.Uint32(data[p+4 : p+8]))
		dp := p + 8
		if dp+16 <= p+isize {
			dsize := int(binary.BigEndian.Uint32(data[dp : dp+4]))
			if string(data[dp+4:dp+8]) == "data" && dsize >= 16 && dp+dsize <= p+isize {
				payload := data[dp+16 : dp+dsize]
				key := fmt.Sprintf("#%d", index)
				if index >= 1 && index <= len(keys) {
					key = keys[index-1]
				}
				out = append(out, Chunk{Keyword: key, Text: string(payload)})
			}
		}
		p += isize
	}
	return out
}
