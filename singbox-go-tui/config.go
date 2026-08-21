package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultAPIURL = "http://localhost:9000"

type APIConfig struct {
	URL    string
	Secret string
}

type ServerEntry struct {
	Name    string
	BaseURL string
}

type CLIConfig struct {
	API         APIConfig
	ServersPath string
	AutoConnect bool
	ShowHelp    bool
}

func defaultAPIConfig() APIConfig {
	return APIConfig{URL: defaultAPIURL}
}

func parseCLI(args []string) (CLIConfig, error) {
	api := loadSavedConfig()
	if value, ok := os.LookupEnv("SINGBOX_API_URL"); ok {
		api.URL = value
	}
	if value, ok := os.LookupEnv("SINGBOX_API_SECRET"); ok {
		api.Secret = value
	}

	var showHelp bool
	var noConnect bool
	var serversPath string
	flags := flag.NewFlagSet("singbox-go-tui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&showHelp, "help", false, "show help")
	flags.BoolVar(&showHelp, "h", false, "show help")
	flags.BoolVar(&noConnect, "no-connect", false, "start without connecting")
	flags.StringVar(&api.URL, "url", api.URL, "sing-box API URL")
	flags.StringVar(&api.URL, "u", api.URL, "sing-box API URL")
	flags.StringVar(&api.Secret, "secret", api.Secret, "API bearer secret")
	flags.StringVar(&api.Secret, "s", api.Secret, "API bearer secret")
	flags.StringVar(&serversPath, "servers", serversPath, "path to server list config")

	if err := flags.Parse(args); err != nil {
		return CLIConfig{}, err
	}
	if flags.NArg() != 0 {
		return CLIConfig{}, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	if showHelp {
		return CLIConfig{ShowHelp: true}, nil
	}
	if strings.TrimSpace(api.URL) == "" {
		return CLIConfig{}, errors.New("API URL must not be empty")
	}
	api.URL = strings.TrimRight(strings.TrimSpace(api.URL), "/")

	return CLIConfig{API: api, ServersPath: serversPath, AutoConnect: !noConnect}, nil
}

func printHelp() {
	fmt.Print(`singbox-go-tui

Usage:
  singbox-go-tui [options]

Options:
  -u, --url URL       sing-box API URL (default: http://localhost:9000)
  -s, --secret VALUE  API bearer secret
      --servers PATH  server list config for the Servers page (optional;
                      without it the Servers page stays empty)
      --no-connect    start without connecting automatically
  -h, --help          show this help

Environment:
  SINGBOX_API_URL     overrides the saved API URL
  SINGBOX_API_SECRET  overrides the saved API secret

Keys:
  1-6 switch pages, r refresh, ? help, q or Ctrl-C quit
`)
}

func configPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".config", "singbox-go-tui", "config"), true
}

func loadServerEntries(path string) []ServerEntry {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	entries := make([]ServerEntry, 0)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		address := fields[1]
		if !strings.Contains(address, "://") {
			address = "http://" + address
		}
		entries = append(entries, ServerEntry{Name: fields[0], BaseURL: address})
	}
	return entries
}

func loadSavedConfig() APIConfig {
	api := defaultAPIConfig()
	path, ok := configPath()
	if !ok {
		return api
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return api
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "url":
			api.URL = strings.TrimSpace(parts[1])
		case "secret":
			api.Secret = parts[1]
		}
	}
	return api
}

func saveConfig(api APIConfig) error {
	path, ok := configPath()
	if !ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := fmt.Fprintf(tmp, "url=%s\nsecret=%s\n", api.URL, api.Secret); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
