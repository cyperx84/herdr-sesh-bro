package herdrx

import (
	"encoding/json"
	"strings"
)

// CurrentWorkspaceID resolves "the workspace this invocation belongs to",
// matching current_workspace_id() in BEHAVIOUR.md §1.7:
//
//  1. workspaceIDEnv ($HERDR_WORKSPACE_ID) verbatim, if non-empty — herdr
//     injects this into every pane it spawns, and it is the only path ever
//     observed live (§1.7).
//  2. Otherwise, a search of contextJSONEnv ($HERDR_PLUGIN_CONTEXT_JSON) for
//     the first "workspace_id" key at any depth whose value is neither null
//     nor false — jq's `.. | .workspace_id? // empty`, first result in
//     preorder document order. This whole path is [UNVERIFIED] per §5: no
//     live pane has ever been observed with this variable set. Malformed or
//     absent JSON silently yields "".
//  3. Otherwise "".
//
// current == "" is meaningful to callers, not an error case: it disables
// current-first sort priority and disables --hide-current filtering
// entirely (§2.2.3).
func CurrentWorkspaceID(workspaceIDEnv, contextJSONEnv string) string {
	if workspaceIDEnv != "" {
		return workspaceIDEnv
	}
	if contextJSONEnv == "" {
		return ""
	}
	v, err := decodeOrdered(json.NewDecoder(strings.NewReader(contextJSONEnv)))
	if err != nil {
		return ""
	}
	if id, ok := findWorkspaceID(v); ok {
		return id
	}
	return ""
}

// orderedField is one key/value pair of a JSON object, in source order.
type orderedField struct {
	key   string
	value any
}

// orderedObject is a JSON object decoded with its field order preserved.
//
// This exists because encoding/json's usual map[string]any decoding does
// NOT preserve key order, and jq's `..` traversal (recurse(.[]?)) visits an
// object's fields in the order they appear in the source. Go map iteration
// order is randomized per-process — using one here would make
// CurrentWorkspaceID's result nondeterministic across runs on the exact
// same input, which is worse than any documented behavioural divergence.
type orderedObject []orderedField

// decodeOrdered parses the next complete JSON value from dec, preserving
// object field order via orderedObject. Arrays decode to []any; scalars
// decode to whatever json.Decoder.Token returns for them (string, float64,
// bool, or nil).
func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, float64, bool, or nil
	}
	switch delim {
	case '{':
		var obj orderedObject
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, _ := keyTok.(string)
			val, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			obj = append(obj, orderedField{key, val})
		}
		if _, err := dec.Token(); err != nil { // consume the closing '}'
			return nil, err
		}
		return obj, nil
	case '[':
		var arr []any
		for dec.More() {
			val, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil { // consume the closing ']'
			return nil, err
		}
		return arr, nil
	default:
		return tok, nil
	}
}

// findWorkspaceID performs jq's `.. | .workspace_id? // empty` search:
// preorder DFS over the tree. At each object node, `.workspace_id?`
// evaluates once against that node — a present key with a null or false
// value counts as no match at THAT node (jq's `//` falsy set), but the
// search still descends into the node's children afterward, since `..`
// visits every node in the tree regardless of what any single node's
// `.workspace_id?` produced. The first matching value anywhere in that
// order wins.
func findWorkspaceID(v any) (string, bool) {
	switch t := v.(type) {
	case orderedObject:
		for _, f := range t {
			if f.key == "workspace_id" {
				if s, ok := workspaceIDValue(f.value); ok {
					return s, true
				}
				break // key was present but falsy; this node doesn't match — fall through to descend
			}
		}
		for _, f := range t {
			if s, ok := findWorkspaceID(f.value); ok {
				return s, true
			}
		}
		return "", false
	case []any:
		for _, item := range t {
			if s, ok := findWorkspaceID(item); ok {
				return s, true
			}
		}
	}
	return "", false
}

// workspaceIDValue converts a decoded workspace_id value the way jq's
// `// empty` filters it: null and false are falsy (no match). Only a JSON
// string is treated as a match — live herdr always sends workspace_id as a
// string, so a truthy non-string value (a number, an object) is treated as
// no match here rather than guessing at jq -r's stringification of it.
func workspaceIDValue(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	if b, ok := v.(bool); ok && !b {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}
