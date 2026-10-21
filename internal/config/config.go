package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultAPIURL is the default mihomo external controller address.
const DefaultAPIURL = "http://127.0.0.1:9090"

// APIConfig holds the connection settings of the controller.
type APIConfig struct {
	URL    string
	Secret string
}

// ServerProtocol selects the API a monitored instance speaks.
type ServerProtocol string

// The supported protocols of a monitored instance.
const (
	// ProtocolSingBox talks to the gRPC-Web API of sing-box.
	ProtocolSingBox ServerProtocol = "sing-box"
	// ProtocolClash talks to the RESTful API of mihomo.
	ProtocolClash ServerProtocol = "clash"
)

// ServerEntry is one monitored instance.
type ServerEntry struct {
	Name     string
	BaseURL  string
	Protocol ServerProtocol
}

// CLIConfig is the fully resolved startup configuration.
type CLIConfig struct {
	API         APIConfig
	ServersPath string
	AutoConnect bool
	ShowHelp    bool
}

// DefaultAPIConfig returns the built-in defaults.
func DefaultAPIConfig() APIConfig {
	return APIConfig{URL: DefaultAPIURL}
}

// ParseCLI resolves the startup configuration.
//
// The precedence is defaults, configuration file, environment variables and
// finally command line flags.
func ParseCLI(args []string) (CLIConfig, error) {
	api := Load()
	if value, ok := os.LookupEnv("MIHOMO_API_URL"); ok {
		api.URL = value
	}
	if value, ok := os.LookupEnv("MIHOMO_API_SECRET"); ok {
		api.Secret = value
	}

	var showHelp bool
	var noConnect bool
	var serversPath string
	flags := flag.NewFlagSet("mihomo-tui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&showHelp, "help", false, "show help")
	flags.BoolVar(&showHelp, "h", false, "show help")
	flags.BoolVar(&noConnect, "no-connect", false, "start without connecting")
	flags.StringVar(&api.URL, "url", api.URL, "mihomo external controller URL")
	flags.StringVar(&api.URL, "u", api.URL, "mihomo external controller URL")
	flags.StringVar(&api.Secret, "secret", api.Secret, "controller bearer secret")
	flags.StringVar(&api.Secret, "s", api.Secret, "controller bearer secret")
	flags.StringVar(&serversPath, "servers", serversPath, "path to the server list file")

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

// PrintHelp writes the usage text to stdout.
func PrintHelp() {
	fmt.Print(`mihomo-tui - terminal dashboard for the mihomo kernel

Usage:
  mihomo-tui [options]

Options:
  -u, --url URL       external controller URL (default: http://127.0.0.1:9090)
  -s, --secret VALUE  controller bearer secret
      --servers PATH  server list file for the Servers page (optional;
                      every line holds a name, an address and an optional
                      protocol, sing-box or clash)
      --no-connect    start without connecting automatically
  -h, --help          show this help

Environment:
  MIHOMO_API_URL     overrides the saved controller URL
  MIHOMO_API_SECRET  overrides the saved controller secret

Keys:
  1-6 switch pages, r refresh, ? help, q or Ctrl-C quit
`)
}

// ConfigPath returns the path of the configuration file.
func ConfigPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".config", "mihomo-tui", "config"), true
}

// Load reads the saved configuration, falling back to the defaults.
func Load() APIConfig {
	api := DefaultAPIConfig()
	path, ok := ConfigPath()
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

// Save writes the configuration file with owner only permissions.
func Save(api APIConfig) error {
	path, ok := ConfigPath()
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

// LoadServerEntries reads the server list file.
//
// Every line holds a display name, an address and an optional protocol.
// Lines starting with "#" and empty lines are ignored. A missing scheme is
// completed with "http://". The protocol defaults to sing-box, the other
// accepted value is "clash".
func LoadServerEntries(path string) []ServerEntry {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []ServerEntry
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
		protocol := ProtocolSingBox
		if len(fields) >= 3 {
			switch strings.ToLower(fields[2]) {
			case string(ProtocolClash):
				protocol = ProtocolClash
			case string(ProtocolSingBox):
				protocol = ProtocolSingBox
			}
		}
		entries = append(entries, ServerEntry{Name: fields[0], BaseURL: address, Protocol: protocol})
	}
	return entries
}
