package trace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonical returns the canonical JSON form of v: v is marshaled (a
// json.RawMessage or []byte is taken as JSON text) and re-encoded with
// object keys sorted, no insignificant whitespace and no HTML escaping;
// numbers keep their text. Equal values give equal bytes.
func Canonical(v any) ([]byte, error) {
	var raw []byte
	switch x := v.(type) {
	case json.RawMessage:
		raw = x
	case []byte:
		raw = x
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var x any
	if err := dec.Decode(&x); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(x); err != nil { // maps encode with sorted keys
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

// Digest returns the first 16 bytes of the SHA-256 of Canonical(v), hex
// encoded (32 characters): the state digest of decision events.
func Digest(v any) (string, error) {
	b, err := Canonical(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16]), nil
}

// Comparable returns the canonical JSON of e without its wall clock, for
// comparing a replayed trace with the original.
func Comparable(e Event) ([]byte, error) {
	e.Wall = 0
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	delete(m, "wall")
	return Canonical(m)
}
