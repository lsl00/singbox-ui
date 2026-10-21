package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers")
	content := "# comment line\n" +
		"\n" +
		"main 127.0.0.1:9090\n" +
		"backup example.com:9443\n" +
		"custom http://1.2.3.4:8080\n" +
		"mihomo mihomo.local:9090 clash\n" +
		"broken\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	entries := LoadServerEntries(path)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "main" || entries[0].BaseURL != "http://127.0.0.1:9090" {
		t.Fatalf("unexpected entry 0: %+v", entries[0])
	}
	if entries[1].Name != "backup" || entries[1].BaseURL != "http://example.com:9443" {
		t.Fatalf("unexpected entry 1: %+v", entries[1])
	}
	if entries[2].BaseURL != "http://1.2.3.4:8080" {
		t.Fatalf("an explicit scheme must be preserved: %+v", entries[2])
	}
	if entries[0].Protocol != ProtocolSingBox {
		t.Fatalf("the protocol must default to sing-box: %+v", entries[0])
	}
	if entries[3].Protocol != ProtocolClash {
		t.Fatalf("expected the clash protocol: %+v", entries[3])
	}
}

func TestLoadServerEntriesMissingFile(t *testing.T) {
	entries := LoadServerEntries(filepath.Join(t.TempDir(), "missing"))
	if len(entries) != 0 {
		t.Fatalf("expected no entries for a missing file, got %+v", entries)
	}
}

func TestLoadServerEntriesEmptyPath(t *testing.T) {
	if entries := LoadServerEntries(""); entries != nil {
		t.Fatalf("expected nil entries for an empty path, got %+v", entries)
	}
}

func TestParseCLIFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cli, err := ParseCLI([]string{"--url", "http://127.0.0.1:1234", "--secret", "abc", "--servers", "/tmp/servers"})
	if err != nil {
		t.Fatal(err)
	}
	if cli.API.URL != "http://127.0.0.1:1234" {
		t.Fatalf("unexpected url: %q", cli.API.URL)
	}
	if cli.API.Secret != "abc" {
		t.Fatalf("unexpected secret: %q", cli.API.Secret)
	}
	if cli.ServersPath != "/tmp/servers" {
		t.Fatalf("unexpected servers path: %q", cli.ServersPath)
	}
	if !cli.AutoConnect {
		t.Fatalf("the dashboard must connect by default")
	}

	noConnect, err := ParseCLI([]string{"--no-connect"})
	if err != nil {
		t.Fatal(err)
	}
	if noConnect.AutoConnect {
		t.Fatalf("--no-connect must disable the automatic connection")
	}
}

func TestParseCLIRejectsUnknownArguments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := ParseCLI([]string{"unexpected"}); err == nil {
		t.Fatal("expected an error for an unexpected argument")
	}
}

func TestEnvironmentOverridesDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MIHOMO_API_URL", "http://192.168.1.10:9090")
	t.Setenv("MIHOMO_API_SECRET", "from-env")

	cli, err := ParseCLI(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cli.API.URL != "http://192.168.1.10:9090" {
		t.Fatalf("unexpected url: %q", cli.API.URL)
	}
	if cli.API.Secret != "from-env" {
		t.Fatalf("unexpected secret: %q", cli.API.Secret)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	api := APIConfig{URL: "http://127.0.0.1:9090", Secret: "top-secret"}
	if err := Save(api); err != nil {
		t.Fatal(err)
	}
	path, ok := ConfigPath()
	if !ok {
		t.Fatal("expected a configuration path")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the secret file must not be readable by others, got %o", mode)
	}

	loaded := Load()
	if loaded != api {
		t.Fatalf("round trip mismatch: %+v != %+v", loaded, api)
	}
}

func TestSaveUsesTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := Save(APIConfig{URL: "http://127.0.0.1:9090"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "mihomo-tui", "config")); err != nil {
		t.Fatalf("expected the configuration below the home directory: %v", err)
	}
}
