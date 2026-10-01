package trace

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// Redacted is what a Secret prints as.
const Redacted = "[REDACTED]"

// Secret holds a credential (an API key) that must never reach a log, a
// trace or an error: every formatting path (fmt verbs, %#v, JSON, text,
// slog) prints Redacted. The value lives in a closure, so even fmt's
// reflection on an enclosing struct (unexported fields) cannot print it.
// The zero Secret is empty.
type Secret struct {
	reveal func() string
}

// NewSecret wraps s.
func NewSecret(s string) Secret {
	if s == "" {
		return Secret{}
	}
	return Secret{reveal: func() string { return s }}
}

// Reveal returns the value, for the one place that needs it (an
// Authorization header).
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
func (s Secret) GoString() string { return "trace.Secret(" + s.text() + ")" }

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

// sensitiveHeaders are redacted by RedactHeader.
var sensitiveHeaders = []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "X-Api-Key"}

// RedactHeader returns a copy of h with credential headers replaced by
// Redacted (for recording HTTP exchanges).
func RedactHeader(h http.Header) http.Header {
	out := h.Clone()
	for _, k := range sensitiveHeaders {
		if vs, ok := out[k]; ok {
			for i := range vs {
				vs[i] = Redacted
			}
		}
	}
	return out
}

// Redact replaces every occurrence of the secrets' values in text (an error
// message that echoes a request, say) with Redacted.
func Redact(text string, secrets ...Secret) string {
	for _, s := range secrets {
		if v := s.Reveal(); v != "" {
			text = strings.ReplaceAll(text, v, Redacted)
		}
	}
	return text
}
