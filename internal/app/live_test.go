package app

import (
	"context"
	"errors"
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

// TestLiveStreams checks the connection and the log stream of a real
// sing-box instance.
//
// Both streams are event driven, so an idle instance stays silent until
// the deadline.
func TestLiveStreams(t *testing.T) {
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
	entries := config.LoadServerEntries(path)
	if len(entries) == 0 {
		t.Skipf("no instance list at %s", path)
	}

	client, err := singbox.New(entries[0].BaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	connStream, err := client.OpenConnectionStream(ctx, time.Second)
	if err != nil {
		t.Fatal("open connections:", err)
	}
	defer connStream.Close()
	for batch := 0; batch < 3; batch++ {
		events, err := connStream.Next(10 * time.Second)
		if err != nil {
			if errors.Is(err, singbox.ErrTimeout) {
				fmt.Println("  the instance is idle, no connection event")
				break
			}
			t.Fatal("read connections:", err)
		}
		fmt.Printf("  connections batch %d: reset=%v events=%d\n", batch, events.Reset, len(events.Events))
		for _, ev := range events.Events {
			destination := ""
			if ev.Connection != nil {
				destination = ev.Connection.Destination
			}
			fmt.Printf("    type=%d id=%.8s up=%d down=%d dst=%s\n",
				ev.Type, ev.ID, ev.UplinkDelta, ev.DownlinkDelta, destination)
		}
	}

	logStream, err := client.OpenLogStream(ctx)
	if err != nil {
		t.Fatal("open logs:", err)
	}
	defer logStream.Close()
	log, err := logStream.Next(10 * time.Second)
	if err != nil {
		if errors.Is(err, singbox.ErrTimeout) {
			fmt.Println("  the instance is idle, no log record")
			return
		}
		t.Fatal("read logs:", err)
	}
	fmt.Printf("  logs: reset=%v messages=%d\n", log.Reset, len(log.Messages))
	for _, entry := range log.Messages {
		fmt.Printf("    level=%d(%s) %s\n", entry.Level, singbox.LogLevelName(entry.Level), entry.Message)
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
