package config

import (
	"testing"
	"time"
)

func TestDefaultsAndOverrides(t *testing.T) {
	c, err := FromMap(nil)
	if err != nil || c.HTTPAddr != ":8080" || !c.CookieSecure || c.DemoPak == "" || c.MaxUploadBytes != 1<<30 {
		t.Fatal(c, err)
	}
	c, err = FromMap(map[string]string{
		"Q2_HTTP_ADDR": ":9", "DATABASE_URL": "postgres://x", "Q2_COOKIE_SECURE": "false",
		"Q2_CORS_ORIGINS": "http://a:3000/, http://b", "Q2_DEMO_PAK": "off", "Q2_SESSION_TTL": "1h",
		"Q2_MAX_UPLOAD_BYTES": "1000", "Q2_TRUST_PROXY": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":9" || c.CookieSecure || c.DemoPak != "" || c.SessionTTL != time.Hour ||
		len(c.CORSOrigins) != 2 || c.CORSOrigins[0] != "http://a:3000" || c.MaxUploadBytes != 1000 || !c.TrustProxy {
		t.Fatalf("%+v", c)
	}
	if _, err := FromMap(map[string]string{"Q2_COOKIE_SECURE": "maybe", "Q2_LOG_LEVEL": "loud"}); err == nil {
		t.Fatal("bad values accepted")
	}
}
