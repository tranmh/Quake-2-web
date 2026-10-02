package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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

// "*" would reflect every Origin with credentials and disable the CSRF
// origin check (api.allowedOrigin): refuse it.
func TestWildcardCORSOriginRejected(t *testing.T) {
	if _, err := FromMap(map[string]string{"Q2_CORS_ORIGINS": "http://localhost:3000,*"}); err == nil {
		t.Fatal("wildcard CORS origin accepted")
	}
}

func TestBotDefaultsAndOverrides(t *testing.T) {
	c, err := FromMap(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := c.Bots
	if b.Enabled || b.Max != 4 || b.PerUser != 1 || b.AllowUsers || b.MaxViewers != 8 || b.RunsDir != "./data/bots" ||
		b.RunsKeep != 50 || b.MaxRun != 30*time.Minute || b.BudgetUSDPerRun != 1 || b.BudgetUSDPerDay != 5 ||
		b.APIKey.IsSet() || b.JevModel != DefaultJevModel || b.JevMaxQPS != 20 {
		t.Fatalf("defaults %+v", b)
	}
	c, err = FromMap(map[string]string{
		"Q2_BOTS_ENABLED": "true", "Q2_BOTS_MAX": "2", "Q2_BOTS_PER_USER": "3", "Q2_BOTS_ALLOW_USERS": "1",
		"Q2_BOT_MAX_VIEWERS": "5", "Q2_BOT_RUNS_DIR": "/tmp/bots", "Q2_BOT_RUNS_KEEP": "0", "Q2_BOT_MAX_RUN": "90s",
		"Q2_BOT_BUDGET_USD_PER_RUN": "0.25", "Q2_BOT_BUDGET_USD_PER_DAY": "0", "TYPESAFE_API_KEY": "  sk-test-123  ",
		"JEV_BASE_URL": "http://127.0.0.1:9", "JEV_ALLOW_CUSTOM_BASE": "1", "Q2_JEV_MODEL": "jev-1.14.0", "Q2_JEV_MAX_QPS": "7.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	b = c.Bots
	if !b.Enabled || b.Max != 2 || b.PerUser != 3 || !b.AllowUsers || b.MaxViewers != 5 || b.RunsDir != "/tmp/bots" ||
		b.RunsKeep != 0 || b.MaxRun != 90*time.Second || b.BudgetUSDPerRun != 0.25 || b.BudgetUSDPerDay != 0 ||
		b.APIKey.Reveal() != "sk-test-123" || b.JevBaseURL != "http://127.0.0.1:9" || !b.JevAllowCustomBase ||
		b.JevModel != "jev-1.14.0" || b.JevMaxQPS != 7.5 {
		t.Fatalf("overrides %+v", b)
	}
	for k, v := range map[string]string{
		"Q2_BOTS_MAX": "0", "Q2_BOTS_PER_USER": "-1", "Q2_BOT_MAX_VIEWERS": "x", "Q2_BOT_RUNS_KEEP": "-2",
		"Q2_BOT_MAX_RUN": "0s", "Q2_BOT_BUDGET_USD_PER_RUN": "-1", "Q2_BOT_BUDGET_USD_PER_DAY": "NaN",
		"Q2_JEV_MAX_QPS": "fast", "JEV_BASE_URL": "ftp://x", "Q2_BOTS_ENABLED": "maybe",
	} {
		if _, err := FromMap(map[string]string{k: v}); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
	if _, err := FromMap(map[string]string{"Q2_BOTS_ENABLED": "1", "Q2_BOT_RUNS_DIR": ""}); err == nil {
		t.Error("enabled bots without a runs directory accepted")
	}
}

// The API key never shows: not in any fmt verb of the secret or of a
// Config holding it, not in JSON, text or slog output.
func TestSecretRedaction(t *testing.T) {
	const key = "sk-live-0123456789abcdef"
	c, err := FromMap(map[string]string{"TYPESAFE_API_KEY": key})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Bots.APIKey
	if !s.IsSet() || s.Reveal() != key {
		t.Fatalf("secret %v not kept", s)
	}
	var outs []string
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%10s"} {
		outs = append(outs, fmt.Sprintf(f, s), fmt.Sprintf(f, c), fmt.Sprintf(f, &c), fmt.Sprintf(f, c.Bots))
	}
	outs = append(outs, s.String(), s.GoString(), fmt.Sprint(s), fmt.Sprintln(c))
	j, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	outs = append(outs, string(j))
	txt, _ := s.MarshalText()
	outs = append(outs, string(txt))
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("cfg", "key", s, "bots", c.Bots, "config", c)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("cfg", "key", s, "bots", c.Bots)
	outs = append(outs, buf.String())
	for _, o := range outs {
		if strings.Contains(o, key) || strings.Contains(o, "0123456789") || strings.Contains(o, "736b2d6c697665") {
			t.Fatalf("key leaked: %s", o)
		}
	}
	if !strings.Contains(string(j), Redacted) || s.String() != Redacted {
		t.Errorf("not marked redacted: %s / %s", j, s)
	}
	var empty Secret
	if empty.String() != "" || empty.Reveal() != "" || empty.IsSet() || NewSecret("").IsSet() {
		t.Error("empty secret")
	}
}
