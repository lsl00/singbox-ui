<script lang="ts">
  import { onMount } from "svelte";
  import { listen } from "@tauri-apps/api/event";
  import {
    connected,
    version,
    serviceStatus,
    status,
    statusHistory,
    startedAt,
    appendStatus,
  } from "../lib/stores";
  import type { StatusInfo, ServiceStatusInfo } from "../lib/types";
  import { getStatus, getServiceStatus, getStartedAt } from "../lib/api";
  import { formatRate, formatBytes, formatTime } from "../lib/format";
  import { SERVICE_STATUS } from "../lib/types";

  let unlisten: (() => void) | undefined;
  let pollTimer: ReturnType<typeof setInterval> | undefined;

  onMount(() => {
    const setup = async () => {
      unlisten = await listen<StatusInfo>("status-update", (event) => {
        appendStatus(event.payload);
      });

      const refreshMeta = async () => {
        try {
          const [svc, started] = await Promise.all([
            getServiceStatus().catch(() => null),
            getStartedAt().catch(() => 0),
          ]);
          if (svc) serviceStatus.set(svc);
          if (started) startedAt.set(started);
        } catch {
          // ignore
        }
      };

      try {
        const s = await getStatus();
        appendStatus(s);
      } catch {
        // ignore
      }

      void refreshMeta();
      pollTimer = setInterval(refreshMeta, 15000);
    };

    void setup();

    return () => {
      unlisten?.();
      if (pollTimer) clearInterval(pollTimer);
    };
  });

  function statusLabel(s: ServiceStatusInfo | null): string {
    if (!s) return "Unknown";
    return SERVICE_STATUS[s.status] ?? "Unknown";
  }

  function statusColor(s: ServiceStatusInfo | null): string {
    if (!s) return "var(--red)";
    switch (s.status) {
      case 2:
        return "var(--green)";
      case 1:
      case 3:
        return "var(--yellow)";
      case 4:
        return "var(--red)";
      default:
        return "var(--text-secondary)";
    }
  }

  function drawChart(uplink: number[], downlink: number[], width: number, height: number) {
    const all = [...uplink, ...downlink].filter((v) => v >= 0);
    const max = all.length ? Math.max(...all, 1) : 1;
    const n = Math.max(uplink.length, downlink.length);
    if (n === 0) return { up: "", down: "", max: 0 };
    const step = width / (n - 1 || 1);

    function path(data: number[]) {
      if (data.length < 2) return "";
      const points = data
        .map((v, i) => {
          const x = i * step;
          const y = height - (v / max) * (height - 4) - 2;
          return `${x.toFixed(1)},${y.toFixed(1)}`;
        })
        .join(" ");
      return `M ${points}`;
    }

    return { up: path(uplink), down: path(downlink), max };
  }

  let chart = $derived(
    $connected
      ? drawChart($statusHistory.uplink, $statusHistory.downlink, 600, 180)
      : null,
  );
</script>

{#if !$connected}
  <div class="placeholder">
    <div class="placeholder-icon">⏻</div>
    <h2>Not Connected</h2>
    <p>Connect to the sing-box API to view the dashboard.</p>
    <p class="hint">Go to Settings to configure the connection.</p>
  </div>
{:else}
  <div class="overview">
    <header class="page-header">
      <div>
        <h1>Overview</h1>
        <p class="subtitle">
          sing-box v{$version?.version ?? "?"} · started
          {formatTime($startedAt)}
        </p>
      </div>
      <div class="service-badge">
        <span class="dot" style="background: {statusColor($serviceStatus)}"></span>
        {statusLabel($serviceStatus)}
      </div>
    </header>

    <div class="cards">
      <div class="card accent-up">
        <span class="card-label">Uplink Rate</span>
        <span class="card-value mono">{formatRate($status?.uplink ?? 0)}</span>
      </div>
      <div class="card accent-down">
        <span class="card-label">Downlink Rate</span>
        <span class="card-value mono">{formatRate($status?.downlink ?? 0)}</span>
      </div>
      <div class="card">
        <span class="card-label">Total Uplink</span>
        <span class="card-value mono">{formatBytes($status?.uplink_total ?? 0)}</span>
      </div>
      <div class="card">
        <span class="card-label">Total Downlink</span>
        <span class="card-value mono">{formatBytes($status?.downlink_total ?? 0)}</span>
      </div>
    </div>

    <div class="chart-card">
      <div class="chart-header">
        <h3>Bandwidth</h3>
        <div class="legend">
          <span><i class="legend-up"></i> Uplink</span>
          <span><i class="legend-down"></i> Downlink</span>
          <span class="chart-max mono">Peak: {formatRate(chart?.max ?? 0)}</span>
        </div>
      </div>
      {#if chart && ($statusHistory.uplink.length > 1 || $statusHistory.downlink.length > 1)}
        <svg viewBox="0 0 600 180" preserveAspectRatio="none">
          {#if chart.down}
            <path d={chart.down} fill="none" stroke="var(--blue)" stroke-width="2" />
          {/if}
          {#if chart.up}
            <path d={chart.up} fill="none" stroke="var(--green)" stroke-width="2" />
          {/if}
        </svg>
      {:else}
        <div class="chart-empty">Waiting for data...</div>
      {/if}
    </div>

    <div class="stats-grid">
      <div class="stat">
        <span class="stat-label">Memory</span>
        <span class="stat-value mono">{formatBytes($status?.memory ?? 0)}</span>
      </div>
      <div class="stat">
        <span class="stat-label">Goroutines</span>
        <span class="stat-value mono">{$status?.goroutines ?? 0}</span>
      </div>
      <div class="stat">
        <span class="stat-label">Inbound Connections</span>
        <span class="stat-value mono">{$status?.connections_in ?? 0}</span>
      </div>
      <div class="stat">
        <span class="stat-label">Outbound Connections</span>
        <span class="stat-value mono">{$status?.connections_out ?? 0}</span>
      </div>
    </div>
  </div>
{/if}

<style>
  .placeholder {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--text-secondary);
  }

  .placeholder-icon {
    font-size: 48px;
    opacity: 0.4;
  }

  .placeholder h2 {
    color: var(--text);
    font-size: 20px;
  }

  .hint {
    font-size: 12px;
    opacity: 0.7;
  }

  .overview {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
  }

  .page-header h1 {
    font-size: 22px;
    font-weight: 600;
  }

  .subtitle {
    color: var(--text-secondary);
    font-size: 13px;
    margin-top: 4px;
  }

  .service-badge {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 20px;
    font-size: 13px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  .cards {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 14px;
  }

  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .card-label {
    font-size: 12px;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.4px;
  }

  .card-value {
    font-size: 24px;
    font-weight: 600;
  }

  .accent-up .card-value {
    color: var(--green);
  }

  .accent-down .card-value {
    color: var(--blue);
  }

  .chart-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px;
  }

  .chart-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }

  .chart-header h3 {
    font-size: 15px;
    font-weight: 600;
  }

  .legend {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 12px;
    color: var(--text-secondary);
  }

  .legend i {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    margin-right: 4px;
  }

  .legend-up {
    background: var(--green);
  }

  .legend-down {
    background: var(--blue);
  }

  .chart-max {
    margin-left: 8px;
  }

  svg {
    width: 100%;
    height: 180px;
    display: block;
  }

  .chart-empty {
    height: 180px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-secondary);
    font-size: 13px;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 14px;
  }

  .stat {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .stat-label {
    font-size: 12px;
    color: var(--text-secondary);
  }

  .stat-value {
    font-size: 18px;
    font-weight: 600;
  }
</style>
