<script lang="ts">
  import { onMount } from "svelte";
  import { connected, version, resetAll } from "../lib/stores";
  import type { ApiConfig } from "../lib/types";
  import { connect, disconnect } from "../lib/api";

  let url = $state("http://localhost:9000");
  let secret = $state("");
  let connecting = $state(false);
  let error = $state("");
  let theme = $state<"light" | "dark">("dark");

  onMount(() => {
    const saved = localStorage.getItem("singbox-dashboard.config");
    if (saved) {
      try {
        const config: ApiConfig = JSON.parse(saved);
        url = config.url;
        secret = config.secret;
      } catch {
        // ignore
      }
    }

    const savedTheme = localStorage.getItem("singbox-dashboard.theme");
    if (savedTheme === "light" || savedTheme === "dark") {
      theme = savedTheme;
    } else {
      theme = window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
    }
    applyTheme();
  });

  function applyTheme() {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("singbox-dashboard.theme", theme);
  }

  function toggleTheme() {
    theme = theme === "dark" ? "light" : "dark";
    applyTheme();
  }

  async function handleConnect() {
    error = "";
    connecting = true;
    const config: ApiConfig = { url: url.trim(), secret };
    try {
      const v = await connect(config);
      localStorage.setItem("singbox-dashboard.config", JSON.stringify(config));
      version.set(v);
      connected.set(true);
    } catch (e) {
      error = String(e);
    } finally {
      connecting = false;
    }
  }

  async function handleDisconnect() {
    try {
      await disconnect();
    } catch (e) {
      console.error("Failed to disconnect:", e);
    }
    resetAll();
  }
</script>

<div class="settings">
  <header class="page-header">
    <h1>Settings</h1>
  </header>

  <section class="section">
    <h2>Connection</h2>
    <div class="form">
      <label class="field">
        <span class="field-label">API URL</span>
        <input type="text" bind:value={url} placeholder="http://localhost:9000" />
      </label>
      <label class="field">
        <span class="field-label">Secret (optional)</span>
        <input type="password" bind:value={secret} placeholder="Bearer secret" />
      </label>
      {#if error}
        <div class="error">Connection failed: {error}</div>
      {/if}
      <div class="actions">
        {#if !$connected}
          <button class="connect-btn" onclick={handleConnect} disabled={connecting}>
            {connecting ? "Connecting..." : "Connect"}
          </button>
        {:else}
          <span class="connected-badge">
            <span class="dot"></span>
            Connected (v{$version?.version})
          </span>
          <button class="disconnect-btn" onclick={handleDisconnect}>Disconnect</button>
        {/if}
      </div>
    </div>
  </section>

  <section class="section">
    <h2>Appearance</h2>
    <div class="form">
      <label class="field-row">
        <span class="field-label">Theme</span>
        <button class="toggle-btn" onclick={toggleTheme}>
          {theme === "dark" ? "🌙 Dark" : "☀️ Light"}
        </button>
      </label>
    </div>
  </section>

  <section class="section">
    <h2>About</h2>
    <p class="about-text">
      sing-box Dashboard — a desktop client for the sing-box 1.14 API, built with
      Tauri and Svelte.
    </p>
  </section>
</div>

<style>
  .settings {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
    max-width: 640px;
  }

  .page-header h1 {
    font-size: 22px;
    font-weight: 600;
  }

  .section {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 18px;
  }

  .section h2 {
    font-size: 15px;
    font-weight: 600;
    margin-bottom: 14px;
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field-label {
    font-size: 12px;
    color: var(--text-secondary);
  }

  .field-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .error {
    padding: 10px 12px;
    border-radius: 6px;
    background: rgba(243, 139, 168, 0.1);
    color: var(--red);
    font-size: 13px;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .connect-btn {
    padding: 9px 20px;
    border-radius: 6px;
    background: var(--accent);
    color: #fff;
    font-size: 14px;
    font-weight: 500;
    transition: opacity 0.1s;
  }

  .connect-btn:hover:not(:disabled) {
    opacity: 0.85;
  }

  .connect-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .connected-badge {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--green);
    font-size: 13px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--green);
  }

  .disconnect-btn {
    padding: 8px 16px;
    border-radius: 6px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 13px;
  }

  .disconnect-btn:hover {
    border-color: var(--red);
    color: var(--red);
  }

  .toggle-btn {
    padding: 8px 16px;
    border-radius: 6px;
    border: 1px solid var(--border);
    font-size: 13px;
  }

  .toggle-btn:hover {
    border-color: var(--accent);
  }

  .about-text {
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
</style>
