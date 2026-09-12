package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndEnvironment(t *testing.T) {
	t.Setenv("FORMS_DATABASE_URL", "postgres://example")
	t.Setenv("FORMS_DATA_DIR", t.TempDir())
	t.Setenv("FORMS_HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("FORMS_INSECURE_COOKIE", "true")
	t.Setenv("FORMS_TRUSTED_PROXY_CIDRS", "127.0.0.1/32, ::1/128")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.HTTPAddr != "127.0.0.1:9000" || got.CookieSecure {
		t.Fatalf("Load() = %#v", got)
	}
	if len(got.TrustedProxyCIDRs) != 2 {
		t.Fatalf("trusted proxy CIDRs = %#v", got.TrustedProxyCIDRs)
	}
}

func TestLoadRejectsInvalidTrustedProxyCIDR(t *testing.T) {
	t.Setenv("FORMS_DATABASE_URL", "postgres://example")
	t.Setenv("FORMS_DATA_DIR", t.TempDir())
	t.Setenv("FORMS_TRUSTED_PROXY_CIDRS", "not-a-network")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CIDR") {
		t.Fatalf("Load() error = %v, want invalid CIDR", err)
	}
}

func TestLoadRequiresDatabase(t *testing.T) {
	t.Setenv("FORMS_DATABASE_URL", "")
	t.Setenv("FORMS_DATA_DIR", t.TempDir())
	if _, err := Load(); err == nil {
		t.Fatal("Load() unexpectedly succeeded")
	}
}

func TestLoadRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"database_url":"postgres://example"} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORMS_CONFIG_FILE", path)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("Load() error = %v, want trailing JSON rejection", err)
	}
}
