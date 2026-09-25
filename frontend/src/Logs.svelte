<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t, currentLang, pluralize } from './i18n';
  import { showToast, showConfirm, capabilities } from './stores';
  import { apiFetch } from './lib/api';
  import { formatBytes } from './lib/format';
  import PageHeader from './PageHeader.svelte';
  import Button from './components/Button.svelte';
  import Select from './components/Select.svelte';
  import LiveIndicator from './components/LiveIndicator.svelte';
  import SegmentedControl from './components/SegmentedControl.svelte';
  import XrayDevicesPanel from './components/logs/XrayDevicesPanel.svelte';

  let { onSwitchTab }: { onSwitchTab?: (tab: string) => void } = $props();

  interface LogEntry {
    id: number;
    timestamp: string;
    source: string; // 'mihomo' | 'xray' | 'xkeen' | 'syslog' | 'xcp'
    level: string; // 'info' | 'warning' | 'error' | 'debug' | 'fatal'
    subsystem?: string;
    message: string;
    metadata?: Record<string, string>;
  }

  interface FlashHealthInfo {
    total_logs_bytes: number;
    free_space_bytes: number;
    total_space_bytes: number;
    is_under_pressure: boolean;
    emergency_actions: number;
  }

  const MAX_LOG_BUFFER = 1000;
  const ROW_HEIGHT = 28;
  const BUFFER_ROWS = 10;

  let destroyed = false;
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  let flashHealthInterval: ReturnType<typeof setInterval> | null = null;
  // Ids for entries without a server id; negative so they never collide.
  let logIdCounter = 0;

  let logs = $state<LogEntry[]>([]);
  let incomingBuffer: LogEntry[] = [];
  let rafId: number | null = null;

  let ws = $state<WebSocket | null>(null);
  let containerHeight = $state(0);
  let scrollTop = $state(0);

  let connected = $state(false);
  let paused = $state(false);
  let pausedNewCount = $state(0);
  let filter = $state('');
  let sourceFilter = $state('');
  // Stream of all sources or the Xray access log grouped by device.
  let logsView = $state<'stream' | 'xray-devices'>('stream');
  const viewItems = $derived([
    { value: 'stream', label: $t('xlog.view_stream') },
    { value: 'xray-devices', label: $t('xlog.view_devices') }
  ]);
  let levelFilter = $state('');
  let autoScroll = $state(true);
  let wordWrap = $state(false);
  let logContainer = $state<HTMLDivElement>();
  let availableSources = $state<string[]>([]);

  // Flash health and runtime level
  let flashHealth = $state<FlashHealthInfo | null>(null);
  let runtimeLevel = $state('info');
  let isUpdatingLevel = $state(false);

  // Xray restart logger state with 5s cooldown
  let restartingXrayLogger = $state(false);
  let restartLoggerCooldown = $state(0);
  let restartLoggerTimer: ReturnType<typeof setInterval> | null = null;

  async function handleRestartXrayLogger() {
    if (restartingXrayLogger || restartLoggerCooldown > 0) return;
    restartingXrayLogger = true;
    try {
      const res = await apiFetch('/api/xray/restart-logger', {
        method: 'POST'
      });
      if (res.ok) {
        showToast('success', $t('logs.restart_logger_success'));
        restartLoggerCooldown = 5;
        if (restartLoggerTimer) clearInterval(restartLoggerTimer);
        restartLoggerTimer = setInterval(() => {
          restartLoggerCooldown--;
          if (restartLoggerCooldown <= 0 && restartLoggerTimer) {
            clearInterval(restartLoggerTimer);
            restartLoggerTimer = null;
          }
        }, 1000);
      } else {
        const err = await res.json().catch(() => null);
        showToast('error', err?.error || $t('logs.restart_logger_error'));
      }
    } catch (e: any) {
      showToast('error', e?.message || $t('logs.restart_logger_error'));
    } finally {
      restartingXrayLogger = false;
    }
  }

  // Filter sources
  const SOURCE_TABS = [
    { id: '', label: 'logs.all_sources' },
    { id: 'mihomo', label: 'Mihomo' },
    { id: 'xray', label: 'Xray' },
    { id: 'xkeen', label: 'XKeen' },
    { id: 'syslog', label: 'logs.source_syslog' },
    { id: 'xcp', label: 'logs.source_xcp' },
    { id: 'errors', label: 'logs.errors_tab', isError: true }
  ];

  const filteredLogs = $derived.by(() => {
    let result = logs;

    // Source / Priority filter
    if (sourceFilter === 'errors') {
      result = result.filter((log) => log.level === 'error' || log.level === 'fatal');
    } else if (sourceFilter) {
      result = result.filter((log) => log.source.toLowerCase() === sourceFilter.toLowerCase());
    }

    // Severity level filter
    if (levelFilter) {
      result = result.filter((log) => log.level.toLowerCase() === levelFilter.toLowerCase());
    }

    // Search query: supports inversion '!term' and text search
    if (filter) {
      const q = filter.trim();
      if (q.startsWith('!') && q.length > 1) {
        const excludeTerm = q.substring(1).toLowerCase();
        result = result.filter((log) => {
          const haystack =
            `${log.timestamp} ${log.source} ${log.level} ${log.subsystem || ''} ${log.message}`.toLowerCase();
          return !haystack.includes(excludeTerm);
        });
      } else {
        const includeTerm = q.toLowerCase();
        result = result.filter((log) => {
          const haystack =
            `${log.timestamp} ${log.source} ${log.level} ${log.subsystem || ''} ${log.message}`.toLowerCase();
          return haystack.includes(includeTerm);
        });
      }
    }

    return result;
  });

  const totalItems = $derived(filteredLogs.length);
  const visibleCount = $derived(Math.ceil(containerHeight / ROW_HEIGHT));
  const startIndex = $derived(Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - BUFFER_ROWS));
  const endIndex = $derived(Math.min(totalItems, startIndex + visibleCount + 2 * BUFFER_ROWS));

  const visibleLogs = $derived.by(() => {
    if (wordWrap) {
      return filteredLogs.map((log) => ({ log, y: 0 }));
    }
    return filteredLogs.slice(startIndex, endIndex).map((log, idx) => ({
      log,
      y: (startIndex + idx) * ROW_HEIGHT
    }));
  });

  function handleScroll(e: Event) {
    const target = e.currentTarget as HTMLElement;
    scrollTop = target.scrollTop;
  }

  function updateSources() {
    const sources = new Set<string>();
    for (const log of logs) {
      if (log.source) sources.add(log.source);
    }
    availableSources = Array.from(sources).sort();
  }

  function scheduleBatchFlush() {
    if (rafId !== null) return;
    rafId = requestAnimationFrame(() => {
      rafId = null;
      if (incomingBuffer.length === 0) return;

      if (paused) {
        pausedNewCount += incomingBuffer.length;
        incomingBuffer = [];
        return;
      }

      // The stream can repeat entries already loaded from history; ids are
      // the {#each} keys, so duplicates must never reach the list.
      const known = new Set(logs.map((l) => l.id));
      const toAppend = incomingBuffer.filter((l) => !known.has(l.id) && known.add(l.id));
      incomingBuffer = [];
      logs = [...logs, ...toAppend].slice(-MAX_LOG_BUFFER);
      updateSources();

      if (autoScroll && logContainer) {
        setTimeout(() => {
          if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
        }, 0);
      }
    });
  }

  function parseFallbackLogLine(raw: string): LogEntry {
    logIdCounter -= 1;
    let text = raw.trim();
    let source = 'xkeen';
    let level = 'info';
    let subsystem = '';
    let timestamp = new Date().toTimeString().split(' ')[0];

    const bracketMatch = text.match(/^\[([^\]]+)\]\s*/);
    if (bracketMatch) {
      const tag = bracketMatch[1].toLowerCase();
      if (tag.includes('xray') || tag.includes('access') || tag.includes('error.log')) {
        source = 'xray';
      } else if (tag.includes('mihomo')) {
        source = 'mihomo';
      } else if (tag.includes('xkeen')) {
        source = 'xkeen';
      } else if (tag.includes('syslog') || tag.includes('messages')) {
        source = 'syslog';
      } else if (tag.includes('xcp')) {
        source = 'xcp';
      }
      text = text.substring(bracketMatch[0].length).trim();
    }

    const tsMatch = text.match(
      /^(\d{4}[-/]\d{2}[-/]\d{2}[T\s]|\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}\s*)?(\d{2}:\d{2}:\d{2})/
    );
    if (tsMatch) {
      timestamp = tsMatch[2];
      text = text.substring(tsMatch[0].length).trim();
    }

    const lower = text.toLowerCase();
    if (lower.includes('error') || lower.includes('fatal') || lower.includes('failed')) {
      level = 'error';
    } else if (lower.includes('warn')) {
      level = 'warning';
    } else if (lower.includes('debug')) {
      level = 'debug';
    }

    return {
      id: logIdCounter,
      timestamp,
      source,
      level,
      subsystem,
      message: text
    };
  }

  async function loadHistory() {
    try {
      const res = await apiFetch('/api/logs/history');
      if (res.ok) {
        const data = await res.json();
        if (data.entries && Array.isArray(data.entries) && data.entries.length > 0) {
          // Merge with entries the stream may have delivered meanwhile.
          const byId = new Map<number, any>();
          for (const l of [...data.entries, ...logs]) byId.set(l.id, l);
          logs = [...byId.values()].sort((a, b) => a.id - b.id).slice(-MAX_LOG_BUFFER);
          updateSources();
          if (autoScroll && logContainer) {
            setTimeout(() => {
              if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
            }, 50);
          }
        }
      }
    } catch (e) {
      console.error('Failed to load initial log history:', e);
    }
  }

  async function fetchFlashHealth() {
    try {
      const res = await apiFetch('/api/logs/flash-health');
      if (res.ok) {
        flashHealth = await res.json();
      }
    } catch {}
  }

  async function changeLogLevel(newLevel: string) {
    if (isUpdatingLevel) return;
    isUpdatingLevel = true;
    try {
      const res = await apiFetch('/api/logs/level', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: 'mihomo', level: newLevel })
      });
      if (res.ok) {
        runtimeLevel = newLevel;
        showToast('success', $t('logs.level_updated', { level: newLevel.toUpperCase() }));
      } else {
        const err = await res.json();
        showToast('error', err?.error || 'Failed to update level');
      }
    } catch (e: any) {
      showToast('error', e?.message || 'Failed to change log level');
    } finally {
      isUpdatingLevel = false;
    }
  }

  function connect() {
    if (destroyed) return;
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/logs/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      connected = true;
    };

    ws.onmessage = (event) => {
      if (document.hidden || destroyed) return;

      try {
        const parsed = JSON.parse(event.data);
        if (Array.isArray(parsed)) {
          for (const item of parsed) {
            logIdCounter -= 1;
            incomingBuffer.push({
              // Local ids are negative so they never collide with server ids.
              id: item.id || logIdCounter,
              timestamp: item.timestamp || new Date().toTimeString().split(' ')[0],
              source: item.source || 'sys',
              level: item.level || 'info',
              subsystem: item.subsystem || '',
              message: item.message || ''
            });
          }
          scheduleBatchFlush();
          return;
        } else if (parsed && typeof parsed === 'object') {
          logIdCounter -= 1;
          incomingBuffer.push({
            id: parsed.id || logIdCounter,
            timestamp: parsed.timestamp || new Date().toTimeString().split(' ')[0],
            source: parsed.source || 'sys',
            level: parsed.level || 'info',
            subsystem: parsed.subsystem || '',
            message: parsed.message || ''
          });
          scheduleBatchFlush();
          return;
        }
      } catch {
        // Fallback for plain text streams
        const entry = parseFallbackLogLine(event.data);
        incomingBuffer.push(entry);
        scheduleBatchFlush();
      }
    };

    ws.onerror = () => {
      connected = false;
    };

    ws.onclose = () => {
      connected = false;
      if (!paused && !destroyed) {
        if (reconnectTimeout) clearTimeout(reconnectTimeout);
        reconnectTimeout = setTimeout(connect, 3000);
      }
    };
  }

  function disconnect() {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    if (ws) {
      ws.close();
      ws = null;
    }
  }

  async function handleClearLogs() {
    const confirmed = await showConfirm($t('logs.clear_confirm'));
    if (!confirmed) return;
    try {
      await apiFetch('/api/logs/clear', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: 'all' })
      });
    } catch {}
    logs = [];
    incomingBuffer = [];
    pausedNewCount = 0;
    showToast('success', $t('logs.cleared_success'));
    fetchFlashHealth();
  }

  function togglePause() {
    paused = !paused;
    if (!paused) {
      pausedNewCount = 0;
      if (autoScroll && logContainer) {
        setTimeout(() => {
          if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
        }, 0);
      }
    }
  }

  function unpauseAndScroll() {
    paused = false;
    pausedNewCount = 0;
    if (logContainer) {
      setTimeout(() => {
        if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
      }, 0);
    }
  }

  function exportFiltered() {
    const textContent = filteredLogs
      .map(
        (l) =>
          `[${l.timestamp}] [${l.source}] [${l.level.toUpperCase()}] ${l.subsystem ? `[${l.subsystem}] ` : ''}${l.message}`
      )
      .join('\n');
    const blob = new Blob([textContent], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `xcp_logs_filtered_${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  function exportFull() {
    const a = document.createElement('a');
    a.href = '/api/logs/download';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  async function copyRow(entry: LogEntry) {
    try {
      const line = `[${entry.timestamp}] [${entry.source}] [${entry.level.toUpperCase()}] ${entry.subsystem ? `[${entry.subsystem}] ` : ''}${entry.message}`;
      await navigator.clipboard.writeText(line);
      showToast('success', $t('logs.copied'));
    } catch {
      showToast('error', 'Failed to copy');
    }
  }

  function highlightMatches(text: string, query: string): string {
    if (!query) return escapeHtml(text);
    const cleanQuery = query.startsWith('!') ? query.substring(1).trim() : query.trim();
    if (!cleanQuery) return escapeHtml(text);

    const safeQuery = cleanQuery.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const regex = new RegExp(`(${safeQuery})`, 'gi');
    const parts = text.split(regex);
    return parts
      .map((part) => {
        if (!part) return '';
        if (part.toLowerCase() === cleanQuery.toLowerCase()) {
          return `<mark class="log-mark">${escapeHtml(part)}</mark>`;
        }
        return escapeHtml(part);
      })
      .join('');
  }

  function escapeHtml(str: string): string {
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function handleVisibilityChange() {
    if (!document.hidden && (!ws || ws.readyState !== WebSocket.OPEN) && !paused && !destroyed) {
      connect();
      fetchFlashHealth();
    }
  }

  onMount(() => {
    loadHistory();
    fetchFlashHealth();
    flashHealthInterval = setInterval(fetchFlashHealth, 30000);
    connect();

    window.addEventListener('visibilitychange', handleVisibilityChange);
    const mainContent = document.querySelector('.main-content') as HTMLElement;
    if (mainContent) {
      mainContent.style.overflowY = 'hidden';
    }
  });

  onDestroy(() => {
    destroyed = true;
    if (rafId !== null) {
      cancelAnimationFrame(rafId);
      rafId = null;
    }
    if (flashHealthInterval) {
      clearInterval(flashHealthInterval);
      flashHealthInterval = null;
    }
    if (restartLoggerTimer) {
      clearInterval(restartLoggerTimer);
      restartLoggerTimer = null;
    }
    disconnect();
    window.removeEventListener('visibilitychange', handleVisibilityChange);
    const mainContent = document.querySelector('.main-content') as HTMLElement;
    if (mainContent) {
      mainContent.style.overflowY = '';
    }
  });
</script>

<div class="logs-page">
  <PageHeader
    title={$t('logs.h1')}
    subtitle={$t('logs.h1_sub')}
    breadcrumbs={[{ label: $t('nav.group_observability') }, { label: $t('nav.logs') }]}
    {onSwitchTab}
    hideHome={true}
  >
    <SegmentedControl items={viewItems} bind:value={logsView} ariaLabel={$t('xlog.view_label')} />
    <!-- Flash Health Badge -->
    {#if flashHealth}
      <div
        class="flash-health-badge"
        class:pressure={flashHealth.is_under_pressure}
        title={$t('logs.flash_health_desc')}
      >
        <svg
          class="flash-icon"
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          aria-hidden="true"
          ><line x1="22" y1="12" x2="2" y2="12" /><path
            d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"
          /><line x1="6" y1="16" x2="6.01" y2="16" /><line
            x1="10"
            y1="16"
            x2="10.01"
            y2="16"
          /></svg
        >
        <span class="flash-stat"
          >{$t('logs.flash_total_logs', {
            size: formatBytes(flashHealth.total_logs_bytes)
          })}</span
        >
        <span class="flash-divider">•</span>
        <span class="flash-stat"
          >{$t('logs.flash_free', { free: formatBytes(flashHealth.free_space_bytes) })}</span
        >
        {#if flashHealth.emergency_actions > 0}
          <span class="flash-alert-tag">⚡ {flashHealth.emergency_actions}</span>
        {/if}
      </div>
    {/if}

    {#if !connected}
      <LiveIndicator live={false} label={$t('logs.status_disconnected')} />
      <Button variant="primary" onclick={connect} title={$t('logs.connect')}>
        {$t('logs.connect')}
      </Button>
    {:else if paused}
      <LiveIndicator live={false} label={$t('logs.status_paused')} />
    {:else}
      <LiveIndicator live={true} label={$t('logs.status_connected')} />
    {/if}
  </PageHeader>

  {#if flashHealth && flashHealth.is_under_pressure}
    <div class="pressure-banner">
      <span class="pressure-icon">⚠️</span>
      <span>{$t('logs.flash_pressure_alert')}</span>
      <button class="btn btn-sm btn-secondary" onclick={handleClearLogs}>
        {$t('logs.clear')}
      </button>
    </div>
  {/if}

  {#if logsView === 'xray-devices'}
    <XrayDevicesPanel />
  {:else}
    <div class="logs-page-container">
      <!-- Unified Balanced Toolbar (LOGHUB-08) -->
      <div class="logs-toolbar">
        <!-- Left Controls: Stream Lifecycle -->
        <div class="tb-group tb-stream">
          <button
            class="btn btn-secondary btn-sm"
            onclick={togglePause}
            title={paused ? $t('logs.resume') : $t('logs.pause')}
          >
            {#if paused}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
                ><polygon points="5 3 19 12 5 21 5 3" /></svg
              >
              <span>{$t('logs.resume')}</span>
            {:else}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"
                ><rect x="6" y="5" width="4" height="14" rx="1" /><rect
                  x="14"
                  y="5"
                  width="4"
                  height="14"
                  rx="1"
                /></svg
              >
              <span>{$t('logs.pause')}</span>
            {/if}
          </button>

          <button
            class="btn btn-secondary btn-sm"
            onclick={handleClearLogs}
            title={$t('logs.clear')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><polyline points="3 6 5 6 21 6" /><path
                d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"
              /></svg
            >
            <span>{$t('logs.clear')}</span>
          </button>

          <button
            class="btn btn-secondary btn-sm"
            class:btn-active={autoScroll}
            onclick={() => (autoScroll = !autoScroll)}
            title={$t('logs.autoscroll')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><polyline points="7 13 12 18 17 13" /><polyline points="7 6 12 11 17 6" /></svg
            >
            <span>{$t('logs.autoscroll')}</span>
          </button>

          <button
            class="btn btn-secondary btn-sm"
            class:btn-active={wordWrap}
            onclick={() => (wordWrap = !wordWrap)}
            title={$t('logs.word_wrap')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><polyline points="9 10 4 15 9 20" /><path d="M20 4v7a4 4 0 0 1-4 4H4" /></svg
            >
            <span>{$t('logs.word_wrap')}</span>
          </button>

          <div class="export-split">
            <button
              class="btn btn-secondary btn-sm"
              onclick={exportFiltered}
              title={$t('logs.export_filtered')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                ><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline
                  points="7 10 12 15 17 10"
                /><line x1="12" y1="15" x2="12" y2="3" /></svg
              >
              <span>{$t('logs.export_filtered')}</span>
            </button>
            <button
              class="btn btn-secondary btn-sm btn-icon"
              onclick={exportFull}
              title={$t('logs.export_full')}
              aria-label={$t('logs.export_full')}
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"><path d="M6 9l6 6 6-6" /></svg
              >
            </button>
          </div>

          {#if $capabilities?.active_kernel === 'xray'}
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              onclick={handleRestartXrayLogger}
              disabled={restartingXrayLogger || restartLoggerCooldown > 0}
              title={$t('logs.restart_xray_logger_hint')}
              data-testid="restart-xray-logger-btn"
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <polyline points="23 4 23 10 17 10" />
                <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
              </svg>
              <span>
                {restartingXrayLogger
                  ? $t('logs.restarting_logger')
                  : restartLoggerCooldown > 0
                    ? `${$t('logs.restart_logger')} (${restartLoggerCooldown}s)`
                    : $t('logs.restart_logger')}
              </span>
            </button>
          {:else}
            <!-- Runtime Core Log-Level Switcher -->
            <div class="runtime-level-control" title={$t('logs.runtime_level')}>
              <span class="ctrl-label">Mihomo:</span>
              <Select
                class="runtime-select"
                value={runtimeLevel}
                disabled={isUpdatingLevel}
                onchange={(e) => changeLogLevel((e.target as HTMLSelectElement).value)}
                ariaLabel={$t('logs.runtime_level')}
              >
                <option value="silent">SILENT</option>
                <option value="error">ERROR</option>
                <option value="warning">WARN</option>
                <option value="info">INFO</option>
                <option value="debug">DEBUG</option>
              </Select>
            </div>
          {/if}
        </div>

        <!-- Right Controls: Search & Filtering -->
        <div class="tb-group tb-filters">
          <!-- Search Input with Inversion & Match Counter -->
          <div class="search-wrap">
            <svg
              class="search-icon"
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              ><circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" /></svg
            >
            <input
              type="text"
              class="search-input"
              placeholder={$t('logs.search_placeholder')}
              bind:value={filter}
            />
            {#if filter}
              <span class="match-badge">
                {filteredLogs.length}/{logs.length}
              </span>
              <button
                class="clear-search-btn"
                onclick={() => (filter = '')}
                title={$t('logs.clear_search')}
                aria-label={$t('logs.clear_search')}
              >
                ×
              </button>
            {/if}
          </div>

          <!-- Source Tabs -->
          <div class="source-pills" role="group" aria-label={$t('logs.source')}>
            {#each SOURCE_TABS as tab (tab.id)}
              <button
                type="button"
                class="source-pill"
                class:error-tab={tab.isError}
                class:active={sourceFilter === tab.id}
                onclick={() => (sourceFilter = sourceFilter === tab.id ? '' : tab.id)}
              >
                {tab.label.startsWith('logs.') ? $t(tab.label) : tab.label}
              </button>
            {/each}
          </div>

          <!-- Severity Level Dropdown -->
          <Select
            bind:value={levelFilter}
            class="level-select"
            wrapperClass="level-select-wrap"
            ariaLabel={$t('logs.level')}
          >
            <option value="">{$t('logs.all_levels')}</option>
            <option value="error">ERROR</option>
            <option value="warning">WARN</option>
            <option value="info">INFO</option>
            <option value="debug">DEBUG</option>
          </Select>
        </div>
      </div>

      <!-- Log Console Pane (Virtual Scroll + Fluid Layout) -->
      <div
        class="logs-console"
        class:wrap-mode={wordWrap}
        bind:this={logContainer}
        bind:clientHeight={containerHeight}
        onscroll={handleScroll}
      >
        {#if totalItems > 0}
          {#if !wordWrap}
            <div
              class="logs-spacer"
              style="height: {totalItems *
                ROW_HEIGHT}px; width: 100%; pointer-events: none; position: absolute; top: 0; left: 0;"
            ></div>
          {/if}

          <div class="lines-container" class:static-layout={wordWrap}>
            {#each visibleLogs as item (item.log.id)}
              <div
                class="log-row"
                class:row-error={item.log.level === 'error' || item.log.level === 'fatal'}
                class:row-warning={item.log.level === 'warning'}
                class:row-debug={item.log.level === 'debug'}
                style={!wordWrap
                  ? `position: absolute; top: 0; left: 0; right: 0; height: ${ROW_HEIGHT}px; transform: translateY(${item.y}px);`
                  : ''}
              >
                <span class="col-ts monospace">{item.log.timestamp}</span>
                <span class="col-src">
                  <span class="src-tag">{item.log.source || 'sys'}</span>
                </span>
                <span class="col-level">
                  {#if item.log.level === 'error' || item.log.level === 'fatal'}
                    <span class="lvl-badge lvl-error">ERR</span>
                  {:else if item.log.level === 'warning'}
                    <span class="lvl-badge lvl-warn">WRN</span>
                  {:else if item.log.level === 'debug'}
                    <span class="lvl-badge lvl-debug">DBG</span>
                  {:else}
                    <span class="lvl-badge lvl-info">INF</span>
                  {/if}
                </span>
                {#if item.log.subsystem}
                  <span class="col-subsystem">[{item.log.subsystem}]</span>
                {/if}
                <span class="col-msg">
                  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                  {@html highlightMatches(item.log.message, filter)}
                </span>

                <button
                  type="button"
                  class="copy-row-btn"
                  onclick={() => copyRow(item.log)}
                  title={$t('logs.copy_row')}
                  aria-label={$t('logs.copy_row')}
                >
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    ><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path
                      d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                    /></svg
                  >
                </button>
              </div>
            {/each}
          </div>
        {/if}

        {#if totalItems === 0}
          <div class="empty-state">
            <svg
              width="32"
              height="32"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              ><circle cx="12" cy="12" r="10" /><line x1="8" y1="12" x2="16" y2="12" /></svg
            >
            <div class="empty-title">
              {!connected
                ? $t('logs.disconnected_title')
                : filter || sourceFilter || levelFilter
                  ? $t('logs.no_filtered_logs')
                  : $t('logs.no_logs')}
            </div>
            <div class="empty-desc">
              {!connected
                ? $t('logs.disconnected_desc')
                : connected
                  ? $t('logs.waiting')
                  : $t('logs.connect_hint')}
            </div>
          </div>
        {/if}

        <!-- Floating Paused Notification Banner -->
        {#if paused && pausedNewCount > 0}
          <div class="floating-pause-banner">
            <span>{$t('logs.paused_notice', { count: String(pausedNewCount) })}</span>
            <button class="btn btn-sm btn-primary" onclick={unpauseAndScroll}>
              {$t('logs.scroll_to_bottom')}
            </button>
          </div>
        {/if}
      </div>

      <!-- Status Bar / Stats Footer -->
      <div class="logs-footer">
        <div class="footer-stat">
          {pluralize(
            logs.length,
            $t('logs.buffer_count_one', { count: String(logs.length) }),
            $t('logs.buffer_count_few', { count: String(logs.length) }),
            $t('logs.buffer_count_many', { count: String(logs.length) }),
            $currentLang
          )}
        </div>
        <div class="footer-stat">
          {pluralize(
            availableSources.length,
            $t('logs.active_sources_count_one', { count: String(availableSources.length) }),
            $t('logs.active_sources_count_few', { count: String(availableSources.length) }),
            $t('logs.active_sources_count_many', { count: String(availableSources.length) }),
            $currentLang
          )}
        </div>
        <div class="footer-stat footer-live">
          <LiveIndicator live={connected && !paused} label={$t('logs.realtime_label')} />
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .logs-page {
    display: flex;
    flex-direction: column;
    height: 100vh;
    box-sizing: border-box;
    padding: 24px 32px 16px;
    gap: 14px;
    background: var(--bg);
  }

  @media (max-width: 768px) {
    .logs-page {
      height: calc(100vh - 50px);
      padding: 16px;
      gap: 10px;
    }
  }

  .flash-health-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-card);
    border: 1px solid var(--border);
    font-size: 12px;
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
  }

  .flash-health-badge.pressure {
    border-color: var(--danger);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    color: var(--danger);
  }

  .flash-icon {
    flex-shrink: 0;
  }

  .flash-stat {
    font-weight: 600;
  }

  .flash-divider {
    color: var(--fg-dim);
    opacity: 0.5;
  }

  .flash-alert-tag {
    background: var(--danger);
    color: var(--btn-primary-text);
    font-size: 12px;
    font-weight: 700;
    padding: 1px 5px;
    border-radius: 4px;
  }

  .pressure-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    border: 1px solid var(--danger);
    border-radius: var(--radius-md);
    padding: 8px 14px;
    font-size: 13px;
    color: var(--danger);
    font-weight: 600;
  }

  .logs-page-container {
    display: flex;
    flex-direction: column;
    gap: 10px;
    flex: 1;
    min-height: 0;
  }

  /* Unified Toolbar */
  .logs-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 8px 12px;
  }

  .tb-group {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .btn-active {
    background: rgba(41, 194, 240, 0.15) !important;
    border-color: var(--accent) !important;
    color: var(--accent) !important;
  }

  .export-split {
    display: inline-flex;
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .export-split button:first-child {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }

  .export-split button:last-child {
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    border-left: 1px solid var(--border);
    padding: 0 6px;
  }

  .runtime-level-control {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }

  .ctrl-label {
    color: var(--fg-secondary);
    font-weight: 600;
  }

  /* Search Input */
  .search-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    color: var(--fg-dim);
    pointer-events: none;
  }

  .search-input {
    height: 30px;
    padding: 0 54px 0 28px;
    font-size: 12px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--fg-primary);
    width: 200px;
    transition: all 0.15s ease;
  }

  .search-input:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(41, 194, 240, 0.2);
    width: 240px;
  }

  .match-badge {
    position: absolute;
    right: 22px;
    font-size: 12px;
    color: var(--accent);
    font-weight: 700;
    font-family: var(--font-family-mono);
  }

  .clear-search-btn {
    position: absolute;
    right: 6px;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    font-size: 14px;
    padding: 0 4px;
    line-height: 1;
  }

  .clear-search-btn:hover {
    color: var(--fg-primary);
  }

  .tb-filters {
    min-width: 0;
    max-width: 100%;
  }

  .tb-filters :global(.level-select-wrap) {
    width: auto;
    min-width: 140px;
  }

  /* Source Pills */
  .source-pills {
    display: inline-flex;
    max-width: 100%;
    overflow-x: auto;
    scrollbar-width: none;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--bg-secondary);
    padding: 1px;
  }

  .source-pill {
    flex-shrink: 0;
    white-space: nowrap;
    padding: 3px 8px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-secondary);
    background: transparent;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    transition:
      background 0.15s,
      color 0.15s;
  }

  .source-pill:hover {
    color: var(--fg-primary);
    background: var(--bg-hover, rgba(255, 255, 255, 0.05));
  }

  .source-pill.active {
    background: var(--accent);
    color: var(--btn-primary-text);
    font-weight: 700;
  }

  .source-pill.error-tab.active {
    background: var(--danger);
    color: var(--btn-primary-text);
  }

  /* Log Console Pane */
  .logs-console {
    position: relative;
    flex: 1;
    min-height: 200px;
    background: var(--bg-terminal);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow-y: auto;
    overflow-x: hidden;
    font-family: var(--font-family-mono);
    font-size: 12px;
    line-height: 28px;
    color: var(--fg-terminal);
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }

  .logs-console.wrap-mode {
    overflow-x: hidden;
    line-height: 1.5;
  }

  .lines-container {
    width: 100%;
  }

  .lines-container.static-layout {
    display: flex;
    flex-direction: column;
  }

  .log-row {
    display: flex;
    align-items: center;
    padding: 0 12px;
    box-sizing: border-box;
    white-space: nowrap;
    border-bottom: 1px solid var(--border-light);
    transition: background 0.1s ease;
  }

  .wrap-mode .log-row {
    white-space: normal;
    padding: 6px 12px;
    align-items: flex-start;
  }

  .log-row:hover {
    background: var(--hover);
  }

  .log-row:hover .copy-row-btn {
    opacity: 1;
  }

  .row-error {
    background: color-mix(in srgb, var(--danger) 9%, transparent);
    color: var(--danger);
  }

  .row-error:hover {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
  }

  .row-warning {
    background: color-mix(in srgb, var(--warning) 8%, transparent);
    color: var(--warning);
  }

  .row-warning:hover {
    background: color-mix(in srgb, var(--warning) 14%, transparent);
  }

  .row-debug {
    color: var(--fg-dim);
  }

  .col-ts {
    flex-shrink: 0;
    width: 70px;
    color: var(--fg-dim);
    font-size: 12px;
  }

  .col-src {
    flex-shrink: 0;
    width: 75px;
    margin-right: 6px;
  }

  .src-tag {
    display: inline-block;
    padding: 1px 5px;
    font-size: 12px;
    font-weight: 600;
    border-radius: 3px;
    background: var(--surface-tint);
    color: var(--fg-dim);
    line-height: 18px;
    vertical-align: middle;
    max-width: 70px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-level {
    flex-shrink: 0;
    width: 40px;
    margin-right: 6px;
  }

  .lvl-badge {
    display: inline-block;
    line-height: 18px;
    vertical-align: middle;
    padding: 1px 4px;
    font-size: 12px;
    font-weight: 800;
    border-radius: 3px;
    letter-spacing: 0.5px;
  }

  .lvl-error {
    background: var(--danger);
    color: var(--btn-primary-text);
  }

  .lvl-warn {
    background: var(--warning);
    color: var(--btn-primary-text);
  }

  .lvl-info {
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--accent);
  }

  .lvl-debug {
    background: color-mix(in srgb, var(--fg-dim) 20%, transparent);
    color: var(--fg-dim);
  }

  .col-subsystem {
    flex-shrink: 0;
    color: var(--accent);
    font-size: 12px;
    font-weight: 700;
    margin-right: 8px;
  }

  .col-msg {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .wrap-mode .col-msg {
    overflow: visible;
    text-overflow: clip;
    word-break: break-word;
  }

  :global(.log-mark) {
    background: var(--warning);
    color: var(--btn-primary-text);
    padding: 0 2px;
    border-radius: 2px;
    font-weight: 700;
  }

  .copy-row-btn {
    opacity: 0;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    cursor: pointer;
    padding: 2px 4px;
    border-radius: 3px;
    transition:
      opacity 0.15s,
      color 0.15s;
    margin-left: 6px;
    flex-shrink: 0;
  }

  .copy-row-btn:hover {
    color: var(--fg-terminal);
    background: var(--surface-tint);
  }

  /* Empty State */
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 180px;
    color: var(--fg-dim);
    text-align: center;
    gap: 8px;
  }

  .empty-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg-terminal);
  }

  .empty-desc {
    font-size: 12px;
    color: var(--fg-terminal);
    opacity: 0.75;
  }

  /* Floating Pause Banner */
  .floating-pause-banner {
    position: absolute;
    bottom: 16px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--bg-terminal);
    border: 1px solid var(--accent);
    box-shadow: var(--shadow-md);
    border-radius: var(--radius-full);
    padding: 6px 14px;
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
    color: var(--fg-terminal);
    z-index: 10;
    animation: fadeIn 0.2s ease;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translate(-50%, 8px);
    }
    to {
      opacity: 1;
      transform: translate(-50%, 0);
    }
  }

  /* Status Bar / Footer */
  .logs-footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 16px;
    padding: 2px 4px;
    font-size: 12px;
    color: var(--fg-secondary);
  }

  .footer-stat {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .footer-live {
    font-weight: 600;
  }
</style>
