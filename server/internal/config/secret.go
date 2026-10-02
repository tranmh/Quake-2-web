package config

import (
	"fmt"
	"log/slog"
)

// Redacted is what a set Secret prints as.
const Redacted = "[REDACTED]"

// Secret holds a credential read from the environment (TYPESAFE_API_KEY)
// that must never reach a log line, an API response or an error: every
// formatting path (fmt verbs including %#v, JSON, text marshaling, slog)
// prints Redacted, or "" when the secret is empty. The value lives in a
// closure, so fmt's reflection over an enclosing struct (Config printed
// with %+v) cannot reach it either. The zero Secret is empty.
type Secret struct {
	reveal func() string
}

// NewSecret wraps s ("" is the empty secret).
func NewSecret(s string) Secret {
	if s == "" {
		return Secret{}
	}
	return Secret{reveal: func() string { return s }}
}

// Reveal returns the value, for the one place that sends it.
func (s Secret) Reveal() string {
	if s.reveal == nil {
		return ""
	}
	return s.reveal()
}

// IsSet reports whether the secret is non-empty.
func (s Secret) IsSet() bool { return s.reveal != nil }

func (s Secret) text() string {
	if s.reveal == nil {
		return ""
	}
	return Redacted
}

// String implements fmt.Stringer.
func (s Secret) String() string { return s.text() }

// GoString implements fmt.GoStringer.
func (s Secret) GoString() string { return "config.Secret(" + s.text() + ")" }

// Format implements fmt.Formatter for every verb.
func (s Secret) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = fmt.Fprint(f, s.GoString())
		return
	}
	_, _ = fmt.Fprint(f, s.text())
}

// MarshalJSON implements json.Marshaler.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + s.text() + `"`), nil }

// MarshalText implements encoding.TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.text()), nil }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.text()) }
