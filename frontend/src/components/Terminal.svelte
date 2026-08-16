<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';
  import { t } from '../i18n';
  import { showToast } from '../stores';

  let terminalContainer: HTMLDivElement | null = $state(null);
  let status = $state<'connected' | 'connecting' | 'disconnected'>('connecting');
  let isFullscreen = $state(false);
  let sessionLimitHit = $state(false);

  let term: Terminal | null = null;
  let fitAddon: FitAddon | null = null;
  let ws: WebSocket | null = null;
  let resizeObserver: ResizeObserver | null = null;
  let themeObserver: MutationObserver | null = null;
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  let lastCols = 0;
  let lastRows = 0;

  function getTerminalTheme() {
    if (typeof window === 'undefined') return {};
    const styles = getComputedStyle(document.documentElement);

    const bgCard = styles.getPropertyValue('--bg-card').trim() || '#102a44';
    const fgPrimary = styles.getPropertyValue('--fg-primary').trim() || '#d9e7f4';
    const accent = styles.getPropertyValue('--accent').trim() || '#29c2f0';
    const border = styles.getPropertyValue('--border').trim() || '#1c3e5c';
    const danger = styles.getPropertyValue('--danger').trim() || '#f4707f';
    const success = styles.getPropertyValue('--success').trim() || '#46d18a';
    const warning = styles.getPropertyValue('--warning').trim() || '#f0b450';

    return {
      background: bgCard,
      foreground: fgPrimary,
      cursor: accent,
      cursorAccent: bgCard,
      selectionBackground: 'rgba(41, 194, 240, 0.3)',
      black: '#0c2237',
      red: danger,
      green: success,
      yellow: warning,
      blue: accent,
      magenta: '#c084fc',
      cyan: '#38bdf8',
      white: fgPrimary,
      brightBlack: '#3e5774',
      brightRed: '#fb7185',
      brightGreen: '#4ade80',
      brightYellow: '#fde047',
      brightBlue: '#60a5fa',
      brightMagenta: '#e879f9',
      brightCyan: '#67e8f9',
      brightWhite: '#ffffff'
    };
  }

  function updateTheme() {
    if (term) {
      term.options.theme = getTerminalTheme();
    }
  }

  function connect() {
    if (ws) {
      try {
        ws.close();
      } catch (_) {}
      ws = null;
    }

    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }

    sessionLimitHit = false;
    status = 'connecting';

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const cols = term?.cols || 80;
    const rows = term?.rows || 24;
    const wsUrl = `${protocol}//${window.location.host}/api/terminal/ws?cols=${cols}&rows=${rows}`;

    try {
      ws = new WebSocket(wsUrl);
      ws.binaryType = 'arraybuffer';

      ws.onopen = () => {
        status = 'connected';
        handleResize();
        term?.focus();
      };

      ws.onmessage = (event) => {
        if (typeof event.data === 'string') {
          try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'error') {
              if (msg.message && msg.message.includes('Maximum active terminal sessions')) {
                sessionLimitHit = true;
                showToast('error', $t('console.terminal_session_limit'));
              }
              term?.writeln(`\r\n\x1b[31m[ERROR] ${msg.message}\x1b[0m`);
              return;
            } else if (msg.type === 'exit') {
              term?.writeln(`\r\n\x1b[33m[Process completed]\x1b[0m`);
              status = 'disconnected';
              return;
            }
          } catch (_) {
            // Not a JSON control message, write raw string
          }
          term?.write(event.data);
        } else if (event.data instanceof ArrayBuffer) {
          const uint8 = new Uint8Array(event.data);
          term?.write(uint8);
        }
      };

      ws.onerror = () => {
        status = 'disconnected';
      };

      ws.onclose = (event) => {
        status = 'disconnected';
        if (event.code === 1008 || event.reason.includes('max sessions')) {
          sessionLimitHit = true;
        }
      };
    } catch (err) {
      status = 'disconnected';
      console.error('Failed to create WebSocket:', err);
    }
  }

  function handleResize() {
    if (!term || !fitAddon || !terminalContainer) return;
    try {
      fitAddon.fit();
      const cols = term.cols;
      const rows = term.rows;

      if (cols > 0 && rows > 0 && (cols !== lastCols || rows !== lastRows)) {
        lastCols = cols;
        lastRows = rows;
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'resize', cols, rows }));
        }
      }
    } catch (e) {
      // Container might be hidden or detaching
    }
  }

  function onDebouncedResize() {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      handleResize();
    }, 100);
  }

  function runXKeen() {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'stdin', data: 'xkeen\n' }));
      term?.focus();
    } else {
      showToast('warning', $t('console.status_disconnected'));
    }
  }

  function clearTerminal() {
    term?.clear();
    term?.focus();
  }

  function toggleFullscreen() {
    isFullscreen = !isFullscreen;
    setTimeout(() => {
      handleResize();
      term?.focus();
    }, 50);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isFullscreen) {
      toggleFullscreen();
    }
  }

  onMount(() => {
    if (!terminalContainer) return;

    term = new Terminal({
      cursorBlink: true,
      cursorStyle: 'block',
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Courier New', monospace",
      fontSize: 13,
      lineHeight: 1.2,
      scrollback: 2000,
      theme: getTerminalTheme(),
      allowProposedApi: true
    });

    fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalContainer);

    term.onData((data) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'stdin', data }));
      }
    });

    term.onBinary((data) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        const buffer = new Uint8Array(data.length);
        for (let i = 0; i < data.length; i++) {
          buffer[i] = data.charCodeAt(i) & 255;
        }
        ws.send(buffer);
      }
    });

    resizeObserver = new ResizeObserver(() => {
      onDebouncedResize();
    });
    resizeObserver.observe(terminalContainer);

    themeObserver = new MutationObserver(() => {
      updateTheme();
    });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme', 'class']
    });

    window.addEventListener('keydown', handleKeydown);

    // Initial connection after layout settles
    setTimeout(() => {
      handleResize();
      connect();
    }, 50);
  });

  onDestroy(() => {
    if (debounceTimer) clearTimeout(debounceTimer);
    if (reconnectTimeout) clearTimeout(reconnectTimeout);
    if (typeof window !== 'undefined') {
      window.removeEventListener('keydown', handleKeydown);
    }
    if (resizeObserver) {
      resizeObserver.disconnect();
      resizeObserver = null;
    }
    if (themeObserver) {
      themeObserver.disconnect();
      themeObserver = null;
    }
    if (ws) {
      try {
        ws.close();
      } catch (_) {}
      ws = null;
    }
    if (fitAddon) {
      fitAddon.dispose();
      fitAddon = null;
    }
    if (term) {
      term.dispose();
      term = null;
    }
  });
</script>

<div class="terminal-wrapper" class:fullscreen={isFullscreen}>
  <!-- Terminal Header Toolbar -->
  <div class="terminal-toolbar">
    <div class="terminal-toolbar-left">
      <div class="terminal-traffic-lights">
        <span class="dot red"></span>
        <span class="dot yellow"></span>
        <span class="dot green"></span>
      </div>

      <div class="terminal-status">
        {#if status === 'connected'}
          <span class="status-indicator connected"></span>
          <span class="status-label">{$t('console.status_connected')}</span>
        {:else if status === 'connecting'}
          <span class="status-indicator connecting"></span>
          <span class="status-label">{$t('console.status_connecting')}</span>
        {:else}
          <span class="status-indicator disconnected"></span>
          <span class="status-label">{$t('console.status_disconnected')}</span>
        {/if}
      </div>

      {#if sessionLimitHit}
        <span class="limit-badge">{$t('console.terminal_session_limit')}</span>
      {/if}
    </div>

    <div class="terminal-toolbar-right">
      <button
        class="terminal-btn accent"
        onclick={runXKeen}
        title={$t('console.terminal_run_xkeen')}
        disabled={status !== 'connected'}
      >
        <svg
          class="btn-icon"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <polygon points="5 3 19 12 5 21 5 3"></polygon>
        </svg>
        <span>{$t('console.terminal_run_xkeen')}</span>
      </button>

      <button class="terminal-btn" onclick={clearTerminal} title={$t('console.terminal_clear')}>
        <svg
          class="btn-icon"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"
          ></path>
        </svg>
        <span>{$t('console.terminal_clear')}</span>
      </button>

      <button class="terminal-btn" onclick={connect} title={$t('console.terminal_reconnect')}>
        <svg
          class="btn-icon"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M23 4v6h-6M1 20v-6h6"></path>
          <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
        </svg>
        <span>{$t('console.terminal_reconnect')}</span>
      </button>

      <button
        class="terminal-btn"
        onclick={toggleFullscreen}
        title={isFullscreen
          ? $t('console.terminal_exit_fullscreen')
          : $t('console.terminal_fullscreen')}
      >
        {#if isFullscreen}
          <svg
            class="btn-icon"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3"
            ></path>
          </svg>
        {:else}
          <svg
            class="btn-icon"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"
            ></path>
          </svg>
        {/if}
      </button>
    </div>
  </div>

  <!-- xterm.js mount element -->
  <div class="terminal-body">
    <div class="terminal-container" bind:this={terminalContainer}></div>
  </div>

  <!-- Terminal Footer with Key Hints -->
  <div class="terminal-footer">
    <div class="key-hints">
      <span class="hint-item">
        <kbd class="key-tag">Ctrl+C</kbd>
        <span class="hint-label">{$t('console.terminal_hint_ctrl_c')}</span>
      </span>
      <span class="hint-item">
        <kbd class="key-tag">Ctrl+L</kbd>
        <span class="hint-label">{$t('console.terminal_hint_ctrl_l')}</span>
      </span>
      <span class="hint-item">
        <kbd class="key-tag">Tab</kbd>
        <span class="hint-label">{$t('console.terminal_hint_tab')}</span>
      </span>
    </div>
    {#if isFullscreen}
      <div class="fullscreen-exit-hint">
        <kbd class="key-tag">Esc</kbd>
        <span class="hint-label">{$t('console.terminal_exit_fullscreen')}</span>
      </div>
    {/if}
  </div>
</div>

<style>
  .terminal-wrapper {
    display: flex;
    flex-direction: column;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    height: 600px;
    box-shadow: var(--shadow-md);
    transition: all var(--transition-fast);
  }

  .terminal-wrapper.fullscreen {
    position: fixed;
    top: 0;
    left: 0;
    width: 100vw;
    height: 100vh;
    z-index: 9999;
    border-radius: 0;
    border: none;
    box-shadow: none;
  }

  .terminal-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.625rem 1rem;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
    user-select: none;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .terminal-toolbar-left {
    display: flex;
    align-items: center;
    gap: 0.875rem;
  }

  .terminal-traffic-lights {
    display: flex;
    align-items: center;
    gap: 0.375rem;
  }

  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    opacity: 0.85;
  }

  .dot.red {
    background: #f4707f;
  }
  .dot.yellow {
    background: #f0b450;
  }
  .dot.green {
    background: #46d18a;
  }

  .terminal-status {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: var(--font-size-xs);
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .status-indicator {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }

  .status-indicator.connected {
    background: var(--success);
    box-shadow: 0 0 6px rgba(70, 209, 138, 0.6);
  }

  .status-indicator.connecting {
    background: var(--warning);
    box-shadow: 0 0 6px rgba(240, 180, 80, 0.6);
    animation: pulse 1.5s infinite;
  }

  .status-indicator.disconnected {
    background: var(--danger);
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
  }

  .limit-badge {
    font-size: var(--font-size-xs);
    background: rgba(244, 112, 127, 0.15);
    color: var(--danger);
    padding: 0.125rem 0.5rem;
    border-radius: var(--radius-sm);
    border: 1px solid rgba(244, 112, 127, 0.3);
  }

  .terminal-toolbar-right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .terminal-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.375rem 0.625rem;
    background: var(--bg-card);
    color: var(--fg-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font-size: var(--font-size-xs);
    font-weight: 500;
    cursor: pointer;
    transition: all var(--transition-fast);
  }

  .terminal-btn:hover:not(:disabled) {
    background: var(--hover);
    border-color: var(--accent);
    color: var(--accent);
  }

  .terminal-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .terminal-btn.accent {
    background: var(--accent-soft);
    border-color: var(--accent-line);
    color: var(--accent);
  }

  .terminal-btn.accent:hover:not(:disabled) {
    background: var(--accent);
    color: var(--btn-primary-text);
  }

  .btn-icon {
    width: 14px;
    height: 14px;
  }

  .terminal-body {
    flex: 1;
    position: relative;
    padding: 0.5rem;
    background: var(--bg-card);
    overflow: hidden;
  }

  .terminal-container {
    width: 100%;
    height: 100%;
  }

  /* Deep styles for xterm viewport and canvas */
  :global(.xterm) {
    height: 100%;
    padding: 0.25rem;
  }

  :global(.xterm-viewport) {
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  :global(.xterm-viewport::-webkit-scrollbar) {
    width: 6px;
  }

  :global(.xterm-viewport::-webkit-scrollbar-thumb) {
    background: var(--scrollbar-thumb);
    border-radius: var(--radius-sm);
  }

  :global(.xterm-viewport::-webkit-scrollbar-thumb:hover) {
    background: var(--scrollbar-thumb-hover);
  }

  .terminal-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.375rem 1rem;
    background: var(--bg-elevated);
    border-top: 1px solid var(--border);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .key-hints {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .hint-item,
  .fullscreen-exit-hint {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
  }

  .key-tag {
    display: inline-block;
    padding: 0.1rem 0.35rem;
    background: var(--bg-card);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    font-family: var(--font-family-mono);
    font-size: 0.7rem;
    color: var(--fg-primary);
  }

  .hint-label {
    font-size: var(--font-size-xs);
  }
</style>
