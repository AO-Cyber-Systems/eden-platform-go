package cms

// Block is one entry of a Page's ordered block list. Type is OPAQUE — the
// concrete vocabulary (whatever kinds a consumer's editor and renderer agree
// on) belongs entirely to the consumer; this package never inspects,
// validates, or special-cases it. Data is likewise an opaque bag of
// block-specific payload.
//
// A Block's position in its containing slice IS its render order. ID is a
// caller-assigned identifier (typically a uuid string) so two blocks sharing
// one Type still coexist, and a specific instance can be targeted for
// reorder or delete.
//
// JSON tags mirror the on-disk JSONB shape so a Block round-trips through
// storage unchanged (see this package's Store, a later TRD in this
// objective).
type Block struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// ParseBlocks projects extra["blocks"] — a decoded JSON value, so a JSON
// array lands as []any and each element as map[string]any — into an
// ordered []Block.
//
// It is TOLERANT BY CONSTRUCTION, porting eden-biz's block_schema.go
// ParseBlocks semantics exactly. This is a published-site availability
// property, not a style choice: a malformed or partial blocks payload must
// never take the whole page down with it.
//
//   - a nil extra, an absent "blocks" key, or a non-array value -> nil
//     (an empty list), never an error
//   - an array element that is not a JSON object -> SKIPPED, never panics
//   - an object with no "type" (or a non-string "type") -> SKIPPED, since an
//     untyped block is unrenderable
//   - null or missing "data" -> nil Data map (renders as an empty block)
//
// Order is preserved exactly as stored; duplicate Types with distinct IDs
// all survive. One malformed block is dropped so its siblings still render.
func ParseBlocks(extra map[string]any) []Block {
	if extra == nil {
		return nil
	}
	arr, ok := extra["blocks"].([]any)
	if !ok {
		return nil // absent, null, or non-array "blocks" -> empty list
	}
	out := make([]Block, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue // non-object element - skip, never panic
		}
		typ, _ := m["type"].(string)
		if typ == "" {
			continue // a block with no type is unrenderable - drop it
		}
		id, _ := m["id"].(string)
		data, _ := m["data"].(map[string]any) // null/missing -> nil map (tolerated)
		out = append(out, Block{ID: id, Type: typ, Data: data})
	}
	return out
}
