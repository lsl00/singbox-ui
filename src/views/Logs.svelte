<script lang="ts">
  import { onMount } from "svelte";
  import { connected } from "../lib/stores";
  import type { LogsInfo } from "../lib/types";
  import { getLogs, clearLogs } from "../lib/api";
  import { LOG_LEVELS } from "../lib/types";

  let pollTimer: ReturnType<typeof setInterval> | undefined;
  let entries = $state<{ id: number; level: number; message: string }[]>([]);
  let seq = 0;
  let minLevel = $state(2); // default show Error and above
  let autoScroll = $state(true);
  let logMode = $state<"200" | "infinite">("200");
  let container: HTMLDivElement | undefined = $state(undefined);

  const levelColors = [
    "var(--purple)", // Panic
    "var(--red)", // Fatal
    "var(--red)", // Error
    "var(--yellow)", // Warn
    "var(--text)", // Info
    "var(--text-secondary)", // Debug
    "var(--text-secondary)", // Trace
  ];

  function applyLogs(batches: LogsInfo[]) {
    let next = entries;
    for (const batch of batches) {
      if (batch.reset) {
        next = [];
        seq = 0;
      }
      for (const msg of batch.messages) {
        next.push({ id: seq++, level: msg.level, message: msg.message });
      }
    }
    if (logMode === "200" && next.length > 200) {
      next = next.slice(next.length - 200);
    }
    entries = next;
  }

  onMount(() => {
    const refresh = async () => {
      try {
        const batches = await getLogs();
        applyLogs(batches);
      } catch {
        // ignore
      }
    };
    void refresh();
    pollTimer = setInterval(refresh, 2000);
    return () => {
      if (pollTimer) clearInterval(pollTimer);
    };
  });

  async function handleClear() {
    try {
      await clearLogs();
      entries = [];
      seq = 0;
    } catch (e) {
      console.error("Failed to clear logs:", e);
    }
  }

  function levelName(level: number): string {
    return LOG_LEVELS[level] ?? "Unknown";
  }

  $effect(() => {
    if (logMode === "200" && entries.length > 200) {
      entries = entries.slice(entries.length - 200);
    }
  });

  let filtered = $derived(entries.filter((e) => e.level <= minLevel));

  $effect(() => {
    if (container && autoScroll && filtered.length > 0) {
      container.scrollTop = container.scrollHeight;
    }
  });
</script>

{#if !$connected}
  <div class="placeholder">
    <h2>Not Connected</h2>
    <p>Connect to the sing-box API first.</p>
  </div>
{:else}
  <div class="logs">
    <header class="page-header">
      <div>
        <h1>Logs</h1>
        <p class="subtitle">{filtered.length} entries shown</p>
      </div>
      <div class="controls">
        <label class="level-filter">
          Min Level:
          <select bind:value={minLevel}>
            {#each LOG_LEVELS as name, i}
              <option value={i}>{name}</option>
            {/each}
          </select>
        </label>
        <label class="level-filter">
          History:
          <select bind:value={logMode}>
            <option value="200">Keep 200</option>
            <option value="infinite">Infinite</option>
          </select>
        </label>
        <label class="autoscroll">
          <input type="checkbox" bind:checked={autoScroll} />
          Auto-scroll
        </label>
        <button class="clear-btn" onclick={handleClear}>Clear</button>
      </div>
    </header>

    <div class="log-list" bind:this={container}>
      {#each filtered as entry (entry.id)}
        <div class="log-entry">
          <span class="level" style="color: {levelColors[entry.level]}">
            [{levelName(entry.level)}]
          </span>
          <span class="message mono">{entry.message}</span>
        </div>
      {/each}

      {#if filtered.length === 0}
        <div class="empty">No log entries.</div>
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

  .logs {
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
    gap: 16px;
    flex-wrap: wrap;
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

  .controls {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 13px;
    color: var(--text-secondary);
  }

  .level-filter {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .level-filter select {
    padding: 5px 8px;
    font-size: 12px;
  }

  .autoscroll {
    display: flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
  }

  .clear-btn {
    padding: 6px 14px;
    border-radius: 6px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 13px;
    transition: all 0.1s;
  }

  .clear-btn:hover {
    border-color: var(--red);
    color: var(--red);
  }

  .log-list {
    flex: 1;
    overflow-y: auto;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 10px;
    font-size: 12px;
  }

  .log-entry {
    display: flex;
    gap: 10px;
    padding: 3px 0;
    border-bottom: 1px solid rgba(128, 128, 128, 0.06);
  }

  .level {
    flex-shrink: 0;
    font-weight: 600;
  }

  .message {
    white-space: pre-wrap;
    word-break: break-all;
    flex: 1;
  }

  .empty {
    text-align: center;
    padding: 40px;
    color: var(--text-secondary);
  }
</style>
