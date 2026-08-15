<script lang="ts">
  import { onMount } from "svelte";
  import { groups, connected } from "../lib/stores";
  import type { GroupInfo, ClashModeInfo } from "../lib/types";
  import {
    getGroups,
    selectOutbound,
    urlTest,
    setGroupExpand,
    getClashModeStatus,
    setClashMode,
  } from "../lib/api";
  import { formatDelay } from "../lib/format";

  let pollTimer: ReturnType<typeof setInterval> | undefined;
  let testing = $state(false);
  let testingTag = $state("");
  let clashMode = $state<ClashModeInfo | null>(null);

  onMount(() => {
    const refresh = async () => {
      try {
        const gs = await getGroups();
        groups.set(gs);
        const mode = await getClashModeStatus().catch(() => null);
        if (mode) clashMode = mode;
      } catch {
        // ignore
      }
    };
    void refresh();
    pollTimer = setInterval(refresh, 3000);
    return () => {
      if (pollTimer) clearInterval(pollTimer);
    };
  });

  async function handleSelect(group: GroupInfo, outboundTag: string) {
    try {
      await selectOutbound(group.tag, outboundTag);
      const gs = await getGroups();
      groups.set(gs);
    } catch (e) {
      console.error("Failed to select outbound:", e);
    }
  }

  async function handleMode(mode: string) {
    try {
      await setClashMode(mode);
      if (clashMode) clashMode = { ...clashMode, current_mode: mode };
    } catch (e) {
      console.error("Failed to set clash mode:", e);
    }
  }

  async function handleUrlTest(outboundTag: string) {
    testing = true;
    testingTag = outboundTag;
    try {
      await urlTest(outboundTag);
      await new Promise((r) => setTimeout(r, 1500));
      const gs = await getGroups();
      groups.set(gs);
    } catch (e) {
      console.error("Failed to URL test:", e);
    } finally {
      testing = false;
      testingTag = "";
    }
  }

  async function handleExpand(group: GroupInfo) {
    try {
      await setGroupExpand(group.tag, !group.is_expand);
      const gs = await getGroups();
      groups.set(gs);
    } catch (e) {
      console.error("Failed to toggle expand:", e);
    }
  }
</script>

{#if !$connected}
  <div class="placeholder">
    <h2>Not Connected</h2>
    <p>Connect to the sing-box API first.</p>
  </div>
{:else}
  <div class="groups">
    <header class="page-header">
      <h1>Proxy Groups</h1>
      <span class="count">{$groups.length} group(s)</span>
    </header>

    {#if clashMode && clashMode.mode_list.length > 0}
      <div class="clash-mode">
        <span class="clash-label">Clash Mode</span>
        <div class="clash-options">
          {#each clashMode.mode_list as mode}
            <button
              class="mode-btn"
              class:active={mode === clashMode.current_mode}
              onclick={() => handleMode(mode)}
            >
              {mode}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    {#if $groups.length === 0}
      <div class="empty">No proxy groups configured.</div>
    {/if}

    {#each $groups as group (group.tag)}
      <div class="group-card">
        <div class="group-header">
          <div class="group-title">
            <span class="group-name">{group.tag}</span>
            <span class="group-type">{group.type}</span>
          </div>
          <button
            class="expand-btn"
            onclick={() => handleExpand(group)}
            title={group.is_expand ? "Collapse" : "Expand"}
          >
            {group.is_expand ? "▾" : "▸"}
          </button>
        </div>

        {#if group.selectable}
          <div class="selected-row">
            <span class="selected-label">Selected:</span>
            <span class="selected-value">{group.selected || "none"}</span>
          </div>
        {/if}

        {#if group.is_expand || true}
          <div class="items">
            {#each group.items as item (item.tag)}
              <div
                class="item"
                class:selected={item.tag === group.selected}
                onclick={() => group.selectable && handleSelect(group, item.tag)}
                role="button"
                tabindex="0"
                onkeydown={(e) => {
                  if (e.key === "Enter" && group.selectable)
                    handleSelect(group, item.tag);
                }}
              >
                <span class="item-radio"></span>
                <span class="item-name">{item.tag}</span>
                <span class="item-type">{item.type}</span>
                {#if item.url_test_time > 0}
                  <span
                    class="item-delay mono"
                    class:good={item.url_test_delay > 0 && item.url_test_delay < 200}
                    class:warn={item.url_test_delay >= 200 && item.url_test_delay < 500}
                    class:bad={item.url_test_delay >= 500 || item.url_test_delay === 0}
                  >
                    {formatDelay(item.url_test_delay)}
                  </span>
                {/if}
                <button
                  class="test-btn"
                  onclick={(e) => {
                    e.stopPropagation();
                    handleUrlTest(item.tag);
                  }}
                  disabled={testing}
                >
                  {testing && testingTag === item.tag ? "..." : "Test"}
                </button>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
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

  .groups {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .page-header h1 {
    font-size: 22px;
    font-weight: 600;
  }

  .count {
    color: var(--text-secondary);
    font-size: 13px;
  }

  .empty {
    color: var(--text-secondary);
    text-align: center;
    padding: 40px;
  }

  .clash-mode {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 14px 16px;
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .clash-label {
    font-size: 13px;
    color: var(--text-secondary);
    flex-shrink: 0;
  }

  .clash-options {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .mode-btn {
    padding: 5px 14px;
    border-radius: 16px;
    border: 1px solid var(--border);
    font-size: 12px;
    color: var(--text-secondary);
    transition: all 0.1s;
  }

  .mode-btn:hover {
    border-color: var(--accent);
    color: var(--text);
  }

  .mode-btn.active {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .group-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
  }

  .group-title {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .group-name {
    font-weight: 600;
    font-size: 15px;
  }

  .group-type {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
    background: var(--bg-hover);
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.4px;
  }

  .expand-btn {
    color: var(--text-secondary);
    font-size: 14px;
    padding: 4px 8px;
    border-radius: 4px;
  }

  .expand-btn:hover {
    background: var(--bg-hover);
  }

  .selected-row {
    padding: 10px 16px;
    background: var(--bg-hover);
    font-size: 13px;
    display: flex;
    gap: 8px;
  }

  .selected-label {
    color: var(--text-secondary);
  }

  .selected-value {
    font-weight: 500;
    color: var(--accent);
  }

  .items {
    padding: 6px;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-radius: 6px;
    cursor: pointer;
    transition: background 0.1s;
  }

  .item:hover {
    background: var(--bg-hover);
  }

  .item.selected {
    background: var(--bg-hover);
  }

  .item-radio {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 2px solid var(--border);
    flex-shrink: 0;
  }

  .item.selected .item-radio {
    border-color: var(--accent);
    background: var(--accent);
    box-shadow: inset 0 0 0 2px var(--bg-card);
  }

  .item-name {
    flex: 1;
    font-weight: 500;
  }

  .item-type {
    font-size: 11px;
    color: var(--text-secondary);
    text-transform: uppercase;
  }

  .item-delay {
    font-size: 13px;
    min-width: 60px;
    text-align: right;
  }

  .item-delay.good {
    color: var(--green);
  }

  .item-delay.warn {
    color: var(--yellow);
  }

  .item-delay.bad {
    color: var(--red);
  }

  .test-btn {
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    transition: all 0.1s;
  }

  .test-btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }

  .test-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
