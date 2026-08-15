<script lang="ts">
  import { onMount } from "svelte";
  import { connected } from "../lib/stores";
  import type { ConnectionInfo, ConnectionEventsInfo } from "../lib/types";
  import { getConnections, closeConnection, closeAllConnections } from "../lib/api";
  import { formatRate, formatDuration } from "../lib/format";

  interface ConnRow {
    connection: ConnectionInfo;
    uplinkRate: number;
    downlinkRate: number;
    closedAt: number | null;
  }

  let pollTimer: ReturnType<typeof setInterval> | undefined;
  let rows = $state<Record<string, ConnRow>>({});
  let closing = $state(false);

  function applyEvents(batches: ConnectionEventsInfo[]) {
    for (const batch of batches) {
      if (batch.reset) {
        rows = {};
      }
      for (const event of batch.events) {
        if (event.type === 0 && event.connection) {
          // NEW
          rows[event.id] = {
            connection: event.connection,
            uplinkRate: 0,
            downlinkRate: 0,
            closedAt: null,
          };
        } else if (event.type === 1) {
          // UPDATE
          const row = rows[event.id];
          if (row) {
            rows[event.id] = {
              ...row,
              uplinkRate: event.uplink_delta,
              downlinkRate: event.downlink_delta,
              connection: event.connection ?? row.connection,
            };
          }
        } else if (event.type === 2) {
          // CLOSED
          const row = rows[event.id];
          if (event.connection) {
            rows[event.id] = {
              connection: event.connection,
              uplinkRate: 0,
              downlinkRate: 0,
              closedAt: event.closed_at || Date.now(),
            };
          } else if (row) {
            rows[event.id] = {
              ...row,
              uplinkRate: 0,
              downlinkRate: 0,
              closedAt: event.closed_at || Date.now(),
            };
          }
        }
      }
    }
  }

  onMount(() => {
    const refresh = async () => {
      try {
        const batches = await getConnections();
        applyEvents(batches);
      } catch {
        // ignore
      }
    };
    void refresh();
    pollTimer = setInterval(refresh, 2500);
    return () => {
      if (pollTimer) clearInterval(pollTimer);
    };
  });

  async function handleClose(id: string) {
    try {
      await closeConnection(id);
      const row = rows[id];
      if (row) {
        rows[id] = { ...row, closedAt: Date.now(), uplinkRate: 0, downlinkRate: 0 };
      }
    } catch (e) {
      console.error("Failed to close connection:", e);
    }
  }

  async function handleCloseAll() {
    closing = true;
    try {
      await closeAllConnections();
      for (const id of Object.keys(rows)) {
        rows[id] = { ...rows[id], closedAt: Date.now(), uplinkRate: 0, downlinkRate: 0 };
      }
    } catch (e) {
      console.error("Failed to close all connections:", e);
    } finally {
      closing = false;
    }
  }

  let activeCount = $derived(
    Object.values(rows).filter((r) => r.closedAt === null).length,
  );
  let totalUplink = $derived(
    Object.values(rows).reduce((s, r) => s + r.uplinkRate, 0),
  );
  let totalDownlink = $derived(
    Object.values(rows).reduce((s, r) => s + r.downlinkRate, 0),
  );
</script>

{#if !$connected}
  <div class="placeholder">
    <h2>Not Connected</h2>
    <p>Connect to the sing-box API first.</p>
  </div>
{:else}
  <div class="connections">
    <header class="page-header">
      <div>
        <h1>Connections</h1>
        <p class="subtitle">
          {activeCount} active · ↑ {formatRate(totalUplink)} · ↓ {formatRate(totalDownlink)}
        </p>
      </div>
      <button class="close-all" onclick={handleCloseAll} disabled={closing}>
        {closing ? "Closing..." : "Close All"}
      </button>
    </header>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Destination</th>
            <th>Protocol</th>
            <th>Chain</th>
            <th>Rule</th>
            <th class="num">Uplink</th>
            <th class="num">Downlink</th>
            <th class="num">Duration</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each Object.values(rows) as row (row.connection.id)}
            <tr class:closed={row.closedAt !== null}>
              <td class="dest">
                <div class="dest-main">
                  {row.connection.domain || row.connection.destination || "-"}
                </div>
                <div class="dest-sub mono">
                  {row.connection.source} → {row.connection.destination}
                </div>
              </td>
              <td>
                <span class="badge">{row.connection.protocol || "-"}</span>
              </td>
              <td class="chain mono">
                {row.connection.chain_list?.length
                  ? row.connection.chain_list.join(" → ")
                  : row.connection.outbound || "-"}
              </td>
              <td class="mono">{row.connection.rule || "-"}</td>
              <td class="num mono up">{formatRate(row.uplinkRate)}</td>
              <td class="num mono down">{formatRate(row.downlinkRate)}</td>
              <td class="num mono">
                {row.closedAt !== null
                  ? formatDuration(row.closedAt - row.connection.created_at)
                  : formatDuration(Date.now() - row.connection.created_at)}
              </td>
              <td class="actions">
                {#if row.closedAt === null}
                  <button class="close-btn" onclick={() => handleClose(row.connection.id)}>
                    ✕
                  </button>
                {:else}
                  <span class="closed-label">Closed</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>

      {#if Object.keys(rows).length === 0}
        <div class="empty">No connections.</div>
      {/if}
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
    gap: 8px;
    color: var(--text-secondary);
  }

  .placeholder h2 {
    color: var(--text);
  }

  .connections {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    height: 100%;
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

  .close-all {
    padding: 8px 16px;
    border-radius: 6px;
    background: var(--red);
    color: #fff;
    font-size: 13px;
    transition: opacity 0.1s;
  }

  .close-all:hover:not(:disabled) {
    opacity: 0.85;
  }

  .close-all:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .table-wrap {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: auto;
    flex: 1;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  thead th {
    text-align: left;
    padding: 12px 14px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.4px;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border);
    position: sticky;
    top: 0;
    background: var(--bg-card);
    white-space: nowrap;
  }

  tbody td {
    padding: 10px 14px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
  }

  tbody tr:last-child td {
    border-bottom: none;
  }

  tr.closed {
    opacity: 0.5;
  }

  .num {
    text-align: right;
  }

  .dest-main {
    font-weight: 500;
  }

  .dest-sub {
    font-size: 11px;
    color: var(--text-secondary);
    margin-top: 2px;
  }

  .badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
    background: var(--bg-hover);
    color: var(--text-secondary);
    text-transform: uppercase;
  }

  .up {
    color: var(--green);
  }

  .down {
    color: var(--blue);
  }

  .chain {
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .actions {
    text-align: right;
  }

  .close-btn {
    padding: 4px 8px;
    border-radius: 4px;
    color: var(--text-secondary);
    font-size: 12px;
  }

  .close-btn:hover {
    background: var(--bg-hover);
    color: var(--red);
  }

  .closed-label {
    font-size: 11px;
    color: var(--text-secondary);
  }

  .empty {
    text-align: center;
    padding: 40px;
    color: var(--text-secondary);
  }
</style>
