<script lang="ts">
  import { onMount } from "svelte";
  import { listen } from "@tauri-apps/api/event";
  import Overview from "./views/Overview.svelte";
  import Groups from "./views/Groups.svelte";
  import Connections from "./views/Connections.svelte";
  import Logs from "./views/Logs.svelte";
  import Settings from "./views/Settings.svelte";
  import { connected, version } from "./lib/stores";
  import type { ApiConfig } from "./lib/types";
  import { connect } from "./lib/api";

  type View = "overview" | "groups" | "connections" | "logs" | "settings";

  interface ConnectionState {
    connected: boolean;
    error: string | null;
  }

  let currentView: View = "overview";
  let sidebarOpen = true;

  const navItems: { id: View; label: string; icon: string }[] = [
    { id: "overview", label: "Overview", icon: "◉" },
    { id: "groups", label: "Proxy Groups", icon: "⇄" },
    { id: "connections", label: "Connections", icon: "≋" },
    { id: "logs", label: "Logs", icon: "≡" },
    { id: "settings", label: "Settings", icon: "⚙" },
  ];

  onMount(() => {
    void listen<ConnectionState>("connection-state", (event) => {
      if (!event.payload.connected) {
        connected.set(false);
      } else {
        connected.set(true);
      }
    });

    const saved = localStorage.getItem("singbox-dashboard.config");
    if (saved) {
      try {
        const config: ApiConfig = JSON.parse(saved);
        void doConnect(config);
      } catch {
        // ignore
      }
    } else {
      void doConnect({ url: "http://localhost:9000", secret: "" });
    }
  });

  async function doConnect(config: ApiConfig) {
    try {
      const v = await connect(config);
      version.set(v);
      connected.set(true);
    } catch (e) {
      console.error("Failed to connect:", e);
    }
  }
</script>

<div class="app">
  <aside class:open={sidebarOpen}>
    <div class="sidebar-header">
      <span class="logo">sing-box</span>
      <span class="subtitle">Dashboard</span>
    </div>
    <nav>
      {#each navItems as item (item.id)}
        <button
          class="nav-item"
          class:active={currentView === item.id}
          onclick={() => (currentView = item.id)}
        >
          <span class="nav-icon">{item.icon}</span>
          <span>{item.label}</span>
        </button>
      {/each}
    </nav>
    <div class="sidebar-footer">
      {#if $connected}
        <span class="status-dot connected"></span>
        <span class="version">v{$version?.version ?? "?"}</span>
      {:else}
        <span class="status-dot disconnected"></span>
        <span class="version">Not connected</span>
      {/if}
    </div>
  </aside>

  <main>
    {#if currentView === "overview"}
      <Overview />
    {:else if currentView === "groups"}
      <Groups />
    {:else if currentView === "connections"}
      <Connections />
    {:else if currentView === "logs"}
      <Logs />
    {:else if currentView === "settings"}
      <Settings />
    {/if}
  </main>
</div>

<style>
  .app {
    display: flex;
    height: 100%;
    width: 100%;
  }

  aside {
    width: var(--sidebar-width);
    background: var(--bg-sidebar);
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
    transition: margin-left 0.2s;
  }

  .sidebar-header {
    padding: 20px 16px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .logo {
    font-size: 18px;
    font-weight: 700;
    color: var(--text-sidebar-active);
    letter-spacing: -0.3px;
  }

  .subtitle {
    font-size: 12px;
    color: var(--text-sidebar);
    opacity: 0.7;
  }

  nav {
    flex: 1;
    padding: 12px 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-radius: 6px;
    color: var(--text-sidebar);
    font-size: 13px;
    transition: background 0.15s, color 0.15s;
    text-align: left;
  }

  .nav-item:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-sidebar-active);
  }

  .nav-item.active {
    background: rgba(255, 255, 255, 0.1);
    color: var(--text-sidebar-active);
  }

  .nav-icon {
    width: 18px;
    text-align: center;
    opacity: 0.8;
  }

  .sidebar-footer {
    padding: 14px 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--text-sidebar);
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    display: inline-block;
  }

  .status-dot.connected {
    background: var(--green);
  }

  .status-dot.disconnected {
    background: var(--red);
  }

  main {
    flex: 1;
    overflow-y: auto;
    background: var(--bg);
  }
</style>
