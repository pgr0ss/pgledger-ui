package ledger

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// MetaField is one top-level metadata key. Traceable fields have a scalar
// value that equals the output of metadata->>key.
type MetaField struct {
	Key, Value string
	Traceable  bool
}

type Metadata struct {
	Raw    string
	Fields []MetaField
}

func ParseMetadata(b []byte) Metadata {
	if b == nil {
		return Metadata{}
	}
	md := Metadata{Raw: compact(b)}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return md
	}
	for k, v := range obj {
		f := MetaField{Key: k, Value: compact(v)}
		switch v[0] {
		case '"':
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				f.Value, f.Traceable = s, true
			}
		case '{', '[', 'n':
		default:
			f.Traceable = true
		}
		md.Fields = append(md.Fields, f)
	}
	slices.SortFunc(md.Fields, func(a, b MetaField) int { return strings.Compare(a.Key, b.Key) })
	return md
}

func compact(b []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return string(b)
	}
	return buf.String()
}
