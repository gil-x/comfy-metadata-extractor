package comfymeta

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// The "prompt" graph (ComfyUI API format) and traversal functions
// ---------------------------------------------------------------------------

type promptNode struct {
	Inputs    map[string]json.RawMessage `json:"inputs"`
	ClassType string                     `json:"class_type"`
	Meta      struct {
		Title string `json:"title"`
	} `json:"_meta"`
}

type promptGraph map[string]promptNode

// --- graph helpers ---

func (g promptGraph) findKSampler() (string, bool) {
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "KSampler") && !strings.Contains(g[id].ClassType, "Select") {
			return id, true
		}
	}
	for _, id := range g.sortedIDs() {
		if strings.Contains(g[id].ClassType, "SamplerCustom") {
			return id, true
		}
	}
	return "", false
}

func (g promptGraph) intByTitle(titles ...string) *int64 {
	for _, id := range g.sortedIDs() {
		t := strings.ToLower(strings.TrimSpace(g[id].Meta.Title))
		for _, want := range titles {
			if t == want {
				if v, ok := g.scalarInt(g[id], "value"); ok {
					return &v
				}
			}
		}
	}
	return nil
}

// isOutputNode reports whether ComfyUI treats a node as an output, i.e. a node
// it executes along with everything upstream of it.
func isOutputNode(classType string) bool {
	return strings.Contains(classType, "Save") || strings.Contains(classType, "Preview") ||
		strings.Contains(classType, "VideoCombine")
}

// executed returns the subgraph ComfyUI actually runs: the output nodes and
// every node upstream of them. Dangling branches (e.g. a LoadAudio node whose
// result is never used) are dropped. If no output node is recognized, the
// graph is returned unchanged.
func (g promptGraph) executed() promptGraph {
	var stack []string
	for id, n := range g {
		if isOutputNode(n.ClassType) {
			stack = append(stack, id)
		}
	}
	if len(stack) == 0 {
		return g
	}
	out := promptGraph{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n, ok := g[id]
		if _, seen := out[id]; seen || !ok {
			continue
		}
		out[id] = n
		stack = append(stack, g.inputLinks(id)...)
	}
	return out
}

func (g promptGraph) sortedIDs() []string {
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ni, ei := strconv.Atoi(ids[i])
		nj, ej := strconv.Atoi(ids[j])
		if ei == nil && ej == nil {
			return ni < nj
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (g promptGraph) linkTarget(node promptNode, input string) (string, bool) {
	raw, ok := node.Inputs[input]
	if !ok {
		return "", false
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) < 1 {
		return "", false
	}
	var id string
	if json.Unmarshal(arr[0], &id) != nil {
		return "", false
	}
	return id, true
}

func (g promptGraph) resolveText(id string, depth int) string {
	if depth > 8 {
		return ""
	}
	node, ok := g[id]
	if !ok {
		return ""
	}
	for _, key := range []string{"text", "text_g", "text_l", "string", "value", "prompt", "wildcard_text", "populated_text"} {
		raw, ok := node.Inputs[key]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		if tid, ok := g.linkTarget(node, key); ok {
			if t := g.resolveText(tid, depth+1); t != "" {
				return t
			}
		}
	}
	return ""
}

func (g promptGraph) scalarString(node promptNode, key string) (string, bool) {
	raw, ok := node.Inputs[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return "", false
}

func (g promptGraph) scalarInt(node promptNode, key string) (int64, bool) {
	raw, ok := node.Inputs[key]
	if !ok {
		return 0, false
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		if f, err := n.Float64(); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}

func (g promptGraph) scalarFloat(node promptNode, key string) (float64, bool) {
	raw, ok := node.Inputs[key]
	if !ok {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, true
	}
	return 0, false
}
