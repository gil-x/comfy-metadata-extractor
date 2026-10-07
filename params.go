package comfymeta

import (
	"encoding/json"
	"strings"
)

// ---------------------------------------------------------------------------
// Extracting parameters from the "prompt" graph
// ---------------------------------------------------------------------------

// Params holds the usual generation parameters for images, videos and audio.
// Empty fields are omitted from the JSON (omitempty).
type Params struct {
	File      string `json:"file"`
	MediaType string `json:"media_type,omitempty"` // "image" | "video" | "audio"
	Model     string `json:"model,omitempty"`      // checkpoint
	Positive  string `json:"positive,omitempty"`
	Negative  string `json:"negative,omitempty"`
	Seed      *int64 `json:"seed,omitempty"` // KSampler.seed or RandomNoise.noise_seed (base pass)

	// image / audio (KSampler)
	Steps     *int64   `json:"steps,omitempty"`
	CFG       *float64 `json:"cfg,omitempty"`
	Sampler   string   `json:"sampler,omitempty"`
	Scheduler string   `json:"scheduler,omitempty"`
	Denoise   *float64 `json:"denoise,omitempty"`

	Width  *int64 `json:"width,omitempty"`
	Height *int64 `json:"height,omitempty"`

	// video (LTX-2.3 and similar)
	Length             *int64 `json:"length,omitempty"`
	Fps                *int64 `json:"fps,omitempty"`
	DistilledLora      string `json:"distilled_lora,omitempty"`
	TextEncoder        string `json:"text_encoder,omitempty"`
	LatentUpscaleModel string `json:"latent_upscale_model,omitempty"`

	// audio (ACE-Step and similar)
	Tags          string   `json:"tags,omitempty"`
	Lyrics        string   `json:"lyrics,omitempty"`
	Duration      *float64 `json:"duration,omitempty"` // seconds
	BPM           *int64   `json:"bpm,omitempty"`
	TimeSignature string   `json:"time_signature,omitempty"`
	KeyScale      string   `json:"key_scale,omitempty"`
	Language      string   `json:"language,omitempty"`
	LoadedAudio   []string `json:"loaded_audio,omitempty"`

	LoadedImages []string `json:"loaded_images,omitempty"`
}

// Extract parses the "prompt" chunk and returns the generation parameters.
// If the chunk is missing or unreadable, only File is set.
func Extract(path string, chunks []Chunk) Params {
	p := Params{File: path}
	c, ok := Find(chunks, "prompt")
	if !ok {
		return p
	}
	var g promptGraph
	if err := json.Unmarshal([]byte(c.Text), &g); err != nil {
		return p
	}

	p.LoadedImages = g.loadedImages()
	p.Model = g.checkpoint()

	if g.isVideo() {
		p.MediaType = "video"
		if s, ok := g.baseNoiseSeed(); ok {
			p.Seed = &s
		}
		pos, neg := g.ltxConditioning()
		p.Positive, p.Negative = pos, neg
		p.Width = g.intByTitle("width")
		p.Height = g.intByTitle("height")
		p.Length = g.intByTitle("length")
		p.Fps = g.intByTitle("frame rate", "framerate", "fps")
		p.DistilledLora = strings.Join(g.loras(), ", ")
		p.TextEncoder = g.firstInputString("text_encoder")
		p.LatentUpscaleModel = g.latentUpscaleModel()
	} else if g.isAudio() {
		p.MediaType = "audio"
		g.fillSampler(&p)
		p.LoadedAudio = g.loadedAudio()
		g.fillAudio(&p)
	} else {
		p.MediaType = "image"
		g.fillSampler(&p)
	}

	// fallback: if there is still no prompt, try the CLIPTextEncode nodes
	if p.Positive == "" && p.Negative == "" {
		p.Positive, p.Negative = g.clipTextHeuristic()
	}
	return p
}

// fillSampler reads the KSampler settings (seed, steps, cfg…), the prompts
// wired into it and the latent dimensions.
func (g promptGraph) fillSampler(p *Params) {
	sid, ok := g.findKSampler()
	if !ok {
		return
	}
	node := g[sid]
	if v, ok := g.scalarInt(node, "seed"); ok {
		p.Seed = &v
	} else if v, ok := g.scalarInt(node, "noise_seed"); ok {
		p.Seed = &v
	}
	if v, ok := g.scalarInt(node, "steps"); ok {
		p.Steps = &v
	}
	if v, ok := g.scalarFloat(node, "cfg"); ok {
		p.CFG = &v
	}
	if v, ok := g.scalarString(node, "sampler_name"); ok {
		p.Sampler = v
	}
	if v, ok := g.scalarString(node, "scheduler"); ok {
		p.Scheduler = v
	}
	if v, ok := g.scalarFloat(node, "denoise"); ok {
		p.Denoise = &v
	}
	if id, ok := g.linkTarget(node, "positive"); ok {
		p.Positive = g.resolveText(id, 0)
	}
	if id, ok := g.linkTarget(node, "negative"); ok {
		p.Negative = g.resolveText(id, 0)
	}
	if id, ok := g.linkTarget(node, "latent_image"); ok {
		ln := g[id]
		if w, ok := g.scalarInt(ln, "width"); ok {
			p.Width = &w
		}
		if h, ok := g.scalarInt(ln, "height"); ok {
			p.Height = &h
		}
		if d, ok := g.scalarFloat(ln, "seconds"); ok {
			p.Duration = &d
		}
	}
}

// --- video / audio detection ---

func (g promptGraph) isVideo() bool {
	for _, n := range g {
		ct := n.ClassType
		if strings.Contains(ct, "LTXV") || ct == "SaveVideo" ||
			strings.Contains(ct, "CreateVideo") || strings.Contains(ct, "VideoCombine") ||
			strings.Contains(ct, "SaveWEBM") {
			return true
		}
	}
	return false
}

func (g promptGraph) isAudio() bool {
	for _, n := range g {
		ct := n.ClassType
		if strings.Contains(ct, "SaveAudio") || strings.Contains(ct, "PreviewAudio") ||
			strings.Contains(ct, "VAEDecodeAudio") || strings.Contains(ct, "AceStep") ||
			strings.Contains(ct, "StableAudio") {
			return true
		}
	}
	return false
}

// --- audio: music text encoder (TextEncodeAceStepAudio…) ---

// fillAudio reads the music fields from the first node that has "tags" or
// "lyrics" inputs (ACE-Step encoders); its explicit duration takes precedence
// over the latent's.
func (g promptGraph) fillAudio(p *Params) {
	for _, id := range g.sortedIDs() {
		n := g[id]
		_, hasTags := n.Inputs["tags"]
		_, hasLyrics := n.Inputs["lyrics"]
		if !hasTags && !hasLyrics {
			continue
		}
		if s, ok := g.scalarString(n, "tags"); ok {
			p.Tags = s
		} else if t, ok := g.linkTarget(n, "tags"); ok {
			p.Tags = g.resolveText(t, 0)
		}
		if s, ok := g.scalarString(n, "lyrics"); ok {
			p.Lyrics = s
		} else if t, ok := g.linkTarget(n, "lyrics"); ok {
			p.Lyrics = g.resolveText(t, 0)
		}
		if v, ok := g.scalarFloat(n, "duration"); ok {
			p.Duration = &v
		}
		if v, ok := g.scalarInt(n, "bpm"); ok {
			p.BPM = &v
		}
		if s, ok := g.scalarString(n, "timesignature"); ok {
			p.TimeSignature = s
		}
		if s, ok := g.scalarString(n, "keyscale"); ok {
			p.KeyScale = s
		}
		if s, ok := g.scalarString(n, "language"); ok {
			p.Language = s
		}
		break
	}
	if p.Duration == nil {
		for _, id := range g.sortedIDs() {
			if v, ok := g.scalarFloat(g[id], "seconds"); ok {
				p.Duration = &v
				break
			}
		}
	}
	// the "positive" prompt of a music encoder is its tag list
	if p.Positive == "" {
		p.Positive = p.Tags
	}
}

func (g promptGraph) loadedAudio() []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range g.sortedIDs() {
		if !strings.Contains(g[id].ClassType, "LoadAudio") {
			continue
		}
		if s, ok := g.scalarString(g[id], "audio"); ok && s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// --- loaded images (LoadImage) ---

func (g promptGraph) loadedImages() []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range g.sortedIDs() {
		ct := g[id].ClassType
		if !(strings.Contains(ct, "LoadImage") || (strings.Contains(ct, "Image") && strings.Contains(ct, "Load"))) {
			continue
		}
		for _, key := range []string{"image", "image_path", "file", "filename", "path"} {
			if s, ok := g.scalarString(g[id], key); ok && s != "" {
				if !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
				break
			}
		}
	}
	return out
}

// --- checkpoint / loras / encoders ---

func (g promptGraph) checkpoint() string {
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "CheckpointLoader") {
			if s, ok := g.scalarString(g[id], "ckpt_name"); ok {
				return s
			}
		}
	}
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "UNETLoader") {
			if s, ok := g.scalarString(g[id], "unet_name"); ok {
				return s
			}
		}
	}
	return ""
}

func (g promptGraph) loras() []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "LoraLoader") {
			if s, ok := g.scalarString(g[id], "lora_name"); ok && s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func (g promptGraph) firstInputString(key string) string {
	for _, id := range g.sortedIDs() {
		if s, ok := g.scalarString(g[id], key); ok && s != "" {
			return s
		}
	}
	return ""
}

func (g promptGraph) latentUpscaleModel() string {
	for _, id := range g.sortedIDs() {
		ct := g[id].ClassType
		if strings.Contains(ct, "LatentUpscale") && strings.Contains(ct, "Loader") {
			for _, k := range []string{"model_name", "upscale_model", "model"} {
				if s, ok := g.scalarString(g[id], k); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// --- positive / negative prompts ---

func (g promptGraph) ltxConditioning() (pos, neg string) {
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "LTXVConditioning") {
			if t, ok := g.linkTarget(g[id], "positive"); ok {
				pos = g.resolveText(t, 0)
			}
			if t, ok := g.linkTarget(g[id], "negative"); ok {
				neg = g.resolveText(t, 0)
			}
			return
		}
	}
	return
}

// clipTextHeuristic: without explicit wiring, assumes that a CLIPTextEncode
// whose text is a link (to a "Prompt" node) is the positive prompt, and that a
// literal text is the negative one.
func (g promptGraph) clipTextHeuristic() (pos, neg string) {
	for _, id := range g.sortedIDs() {
		if !strings.Contains(g[id].ClassType, "CLIPTextEncode") {
			continue
		}
		if raw, ok := g[id].Inputs["text"]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				if neg == "" {
					neg = s
				}
			} else if tid, ok := g.linkTarget(g[id], "text"); ok {
				if t := g.resolveText(tid, 0); t != "" && pos == "" {
					pos = t
				}
			}
		}
	}
	return
}

// --- base pass seed (multi-pass video) ---

func (g promptGraph) genSamplers() []string {
	var s []string
	for _, id := range g.sortedIDs() {
		n := g[id]
		if strings.Contains(n.ClassType, "Sampler") {
			if _, ok := n.Inputs["latent_image"]; ok {
				s = append(s, id)
			}
		}
	}
	return s
}

func (g promptGraph) dependsOnAny(start string, targets map[string]bool) bool {
	seen := map[string]bool{}
	stack := g.inputLinks(start)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		if targets[id] {
			return true
		}
		stack = append(stack, g.inputLinks(id)...)
	}
	return false
}

func (g promptGraph) inputLinks(id string) []string {
	var out []string
	for _, raw := range g[id].Inputs {
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil && len(arr) >= 1 {
			var t string
			if json.Unmarshal(arr[0], &t) == nil {
				out = append(out, t)
			}
		}
	}
	return out
}

func (g promptGraph) baseNoiseSeed() (int64, bool) {
	samplers := g.genSamplers()
	if len(samplers) == 0 {
		return 0, false
	}
	set := map[string]bool{}
	for _, s := range samplers {
		set[s] = true
	}
	base := samplers[0]
	for _, s := range samplers {
		others := map[string]bool{}
		for k := range set {
			if k != s {
				others[k] = true
			}
		}
		if !g.dependsOnAny(s, others) {
			base = s // pass with no upstream sampler
			break
		}
	}
	if nid, ok := g.linkTarget(g[base], "noise"); ok {
		if v, ok := g.scalarInt(g[nid], "noise_seed"); ok {
			return v, true
		}
		if v, ok := g.scalarInt(g[nid], "seed"); ok {
			return v, true
		}
	}
	// fallback: largest noise_seed found
	best, found := int64(0), false
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "Noise") {
			if v, ok := g.scalarInt(g[id], "noise_seed"); ok {
				if !found || v > best {
					best, found = v, true
				}
			}
		}
	}
	return best, found
}
