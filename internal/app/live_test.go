package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mihomo-tui/internal/config"
	"mihomo-tui/internal/singbox"
)

// TestLiveInstances checks the monitored instances against real sing-box
// instances. It is skipped unless MIHOMO_TUI_LIVE is set.
//
// The instance list is read from MIHOMO_TUI_SERVERS or from the servers file
// in the home directory.
func TestLiveInstances(t *testing.T) {
	if os.Getenv("MIHOMO_TUI_LIVE") == "" {
		t.Skip("set MIHOMO_TUI_LIVE=1 to run the live check")
	}
	path := os.Getenv("MIHOMO_TUI_SERVERS")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home directory")
		}
		path = filepath.Join(home, "servers")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no instance list at %s: %v", path, err)
	}

	for _, entry := range config.LoadServerEntries(path) {
		address := entry.BaseURL
		client, err := singbox.New(address, "")
		if err != nil {
			t.Errorf("%s: %v", address, err)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		version, err := client.GetVersion(ctx)
		cancel()
		if err != nil {
			t.Errorf("%s: version failed: %v", address, err)
			continue
		}
		streamCtx, streamCancel := context.WithTimeout(context.Background(), 20*time.Second)
		stream, err := client.OpenStatusStream(streamCtx, time.Second)
		if err != nil {
			t.Errorf("%s: stream failed: %v", address, err)
			streamCancel()
			continue
		}
		var prevUp, prevDown int64
		for i := 0; i < 8; i++ {
			status, err := stream.Next(8 * time.Second)
			if err != nil {
				t.Errorf("%s: sample failed: %v", address, err)
				break
			}
			// sing-box computes the rate as the difference of the
			// cumulative counters, so the two have to agree.
			if i > 0 {
				if got := status.UplinkTotal - prevUp; got != status.Uplink {
					t.Errorf("%s: uplink %d does not match the counter delta %d",
						address, status.Uplink, got)
				}
				if got := status.DownlinkTotal - prevDown; got != status.Downlink {
					t.Errorf("%s: downlink %d does not match the counter delta %d",
						address, status.Downlink, got)
				}
			}
			prevUp, prevDown = status.UplinkTotal, status.DownlinkTotal
			fmt.Printf("%-6s %-30s v%-12s mem=%-11s goroutines=%-4d conns=%-4d up=%-12s down=%-12s upTotal=%-12s downTotal=%-12s\n",
				entry.Name, address, version.Version,
				formatBytes(int64(status.Memory)), status.Goroutines, status.Connections(),
				formatBytes(status.UplinkRate()), formatBytes(status.DownlinkRate()),
				formatBytes(status.UplinkTotal), formatBytes(status.DownlinkTotal))
		}
		stream.Close()
		streamCancel()
	}
}

func formatBytes(value int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	scaled := float64(value)
	unit := 0
	for scaled >= 1024 && unit < len(units)-1 {
		scaled /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", scaled, units[unit])
}
