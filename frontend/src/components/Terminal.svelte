<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';
  import { t } from '../i18n';
  import { showToast } from '../stores';
  import Icon from '../lib/components/Icon.svelte';

  let terminalContainer: HTMLDivElement | null = $state(null);
  let status = $state<'connected' | 'connecting' | 'disconnected'>('connecting');
  let isFullscreen = $state(false);
  let sessionLimitHit = $state(false);
  let currentCols = $state(80);
  let currentRows = $state(24);

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

    const bgDeep = styles.getPropertyValue('--bg-deep').trim() || '#050d16';
    const fgPrimary = styles.getPropertyValue('--fg-primary').trim() || '#d9e7f4';
    const accent = styles.getPropertyValue('--accent').trim() || '#29c2f0';
    const danger = styles.getPropertyValue('--danger').trim() || '#f4707f';
    const success = styles.getPropertyValue('--success').trim() || '#46d18a';
    const warning = styles.getPropertyValue('--warning').trim() || '#f0b450';

    return {
      background: bgDeep,
      foreground: fgPrimary,
      cursor: accent,
      cursorAccent: bgDeep,
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
              term?.writeln(`\r\n\x1b[33m[${$t('console.terminal_process_terminated')}]\x1b[0m`);
              status = 'disconnected';
              return;
            }
          } catch (_) {
            // Raw text
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

      if (cols > 0 && rows > 0) {
        currentCols = cols;
        currentRows = rows;
        if (cols !== lastCols || rows !== lastRows) {
          lastCols = cols;
          lastRows = rows;
          if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'resize', cols, rows }));
          }
        }
      }
    } catch (e) {
      // Ignored
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
    }, 60);
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
      fontFamily: "'JetBrains Mono', ui-monospace, Menlo, Monaco, monospace",
      fontSize: 13,
      lineHeight: 1.25,
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

    setTimeout(() => {
      handleResize();
      connect();
    }, 60);
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

<div class="terminal-card" class:fullscreen={isFullscreen}>
  <!-- Top Toolbar Header -->
  <div class="terminal-header">
    <div class="header-left">
      <div class="terminal-title">
        <Icon name="console" size={16} />
        <span class="title-text">root@xkeen</span>
        <span class="shell-badge">sh</span>
      </div>

      <div class="status-pill {status}">
        <span class="dot"></span>
        <span class="status-name">
          {#if status === 'connected'}
            {$t('console.status_connected')}
          {:else if status === 'connecting'}
            {$t('console.status_connecting')}
          {:else}
            {$t('console.status_disconnected')}
          {/if}
        </span>
      </div>

      {#if sessionLimitHit}
        <span class="badge badge-danger">
          {$t('console.terminal_session_limit')}
        </span>
      {/if}
    </div>

    <div class="header-right">
      <button
        class="btn btn-sm btn-primary"
        onclick={runXKeen}
        title={$t('console.terminal_run_xkeen')}
        disabled={status !== 'connected'}
      >
        <Icon name="play" size={12} />
        <span>{$t('console.terminal_run_xkeen')}</span>
      </button>

      <button
        class="btn btn-sm btn-secondary"
        onclick={clearTerminal}
        title={$t('console.terminal_clear')}
      >
        <Icon name="trash" size={12} />
        <span>{$t('console.terminal_clear')}</span>
      </button>

      <button
        class="btn btn-sm btn-secondary"
        onclick={connect}
        title={$t('console.terminal_reconnect')}
      >
        <Icon name="refresh" size={12} />
        <span>{$t('console.terminal_reconnect')}</span>
      </button>

      <button
        class="btn btn-sm btn-secondary"
        onclick={toggleFullscreen}
        title={isFullscreen
          ? $t('console.terminal_exit_fullscreen')
          : $t('console.terminal_fullscreen')}
      >
        {#if isFullscreen}
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3"
            />
          </svg>
          <span>{$t('console.terminal_exit_fullscreen')}</span>
        {:else}
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"
            />
          </svg>
          <span>{$t('console.terminal_fullscreen')}</span>
        {/if}
      </button>
    </div>
  </div>

  <!-- Terminal Canvas Viewport -->
  <div class="terminal-screen">
    <div class="xterm-mount" bind:this={terminalContainer}></div>
  </div>

  <!-- Status Bar Footer -->
  <div class="terminal-footer">
    <div class="footer-hints">
      <span class="hint">
        <kbd class="kbd">Ctrl+C</kbd>
        <span class="hint-text">{$t('console.terminal_hint_ctrl_c')}</span>
      </span>
      <span class="hint">
        <kbd class="kbd">Ctrl+L</kbd>
        <span class="hint-text">{$t('console.terminal_hint_ctrl_l')}</span>
      </span>
      <span class="hint">
        <kbd class="kbd">Tab</kbd>
        <span class="hint-text">{$t('console.terminal_hint_tab')}</span>
      </span>
    </div>

    <div class="footer-meta">
      {#if isFullscreen}
        <span class="hint">
          <kbd class="kbd">Esc</kbd>
          <span class="hint-text">{$t('console.terminal_exit_fullscreen')}</span>
        </span>
      {/if}
      <span class="geo-badge">{currentCols} × {currentRows}</span>
    </div>
  </div>
</div>

<style>
  .terminal-card {
    display: flex;
    flex-direction: column;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    height: min(680px, calc(100vh - 220px));
    min-height: 480px;
    box-shadow: var(--shadow-md);
    transition: all var(--transition-fast);
  }

  .terminal-card.fullscreen {
    position: fixed;
    inset: 0;
    width: 100vw;
    height: 100vh;
    z-index: 9999;
    border-radius: 0;
    border: none;
    box-shadow: none;
    min-height: 100vh;
  }

  .terminal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
    gap: 12px;
    flex-wrap: wrap;
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .terminal-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
  }

  .shell-badge {
    display: inline-block;
    padding: 1px 6px;
    font-size: 11px;
    font-family: var(--font-family-mono);
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    border-radius: var(--radius-sm);
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 2px 8px;
    border-radius: 12px;
    font-size: 11.5px;
    font-weight: 500;
  }

  .status-pill .dot {
    width: 6px;
    height: 6px;
    min-width: 6px;
    min-height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .status-pill.connected {
    background: rgba(70, 209, 138, 0.12);
    color: var(--success);
    border: 1px solid rgba(70, 209, 138, 0.25);
  }

  .status-pill.connected .dot {
    background: var(--success);
    box-shadow: 0 0 6px rgba(70, 209, 138, 0.7);
  }

  .status-pill.connecting {
    background: rgba(240, 180, 80, 0.12);
    color: var(--warning);
    border: 1px solid rgba(240, 180, 80, 0.25);
  }

  .status-pill.connecting .dot {
    background: var(--warning);
    box-shadow: 0 0 6px rgba(240, 180, 80, 0.7);
    animation: blink 1.2s infinite;
  }

  .status-pill.disconnected {
    background: rgba(244, 112, 127, 0.12);
    color: var(--danger);
    border: 1px solid rgba(244, 112, 127, 0.25);
  }

  .status-pill.disconnected .dot {
    background: var(--danger);
  }

  @keyframes blink {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .header-right :global(.btn) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }

  .terminal-screen {
    flex: 1;
    position: relative;
    background: #050d16;
    padding: 10px 14px;
    overflow: hidden;
    min-height: 0;
  }

  .xterm-mount {
    width: 100%;
    height: 100%;
  }

  /* Deep xterm customization */
  :global(.xterm) {
    height: 100%;
    padding: 0;
  }

  :global(.xterm-screen) {
    padding: 0;
  }

  :global(.xterm-viewport) {
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  :global(.xterm-viewport::-webkit-scrollbar) {
    width: 6px;
  }

  :global(.xterm-viewport::-webkit-scrollbar-track) {
    background: transparent;
  }

  :global(.xterm-viewport::-webkit-scrollbar-thumb) {
    background: var(--border);
    border-radius: var(--radius-sm);
  }

  :global(.xterm-viewport::-webkit-scrollbar-thumb:hover) {
    background: var(--border-strong);
  }

  .terminal-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 16px;
    background: var(--bg-elevated);
    border-top: 1px solid var(--border);
    font-size: 11.5px;
    color: var(--fg-dim);
    gap: 12px;
  }

  .footer-hints,
  .footer-meta {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .hint {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .kbd {
    display: inline-block;
    padding: 1px 5px;
    background: var(--bg-card);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    font-family: var(--font-family-mono);
    font-size: 10.5px;
    color: var(--fg-primary);
    box-shadow: 0 1px 0 rgba(0, 0, 0, 0.2);
  }

  .hint-text {
    font-size: 11px;
  }

  .geo-badge {
    font-family: var(--font-family-mono);
    font-size: 11px;
    color: var(--fg-dim);
    background: var(--bg-card);
    padding: 2px 7px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  @media (max-width: 768px) {
    .terminal-card {
      height: calc(100vh - 160px);
    }
    .header-right :global(.btn span) {
      display: none;
    }
    .footer-hints {
      gap: 8px;
    }
  }
</style>
