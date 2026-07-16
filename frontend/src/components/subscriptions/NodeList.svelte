<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';

  interface Node {
    tag: string;
    name?: string;
    country?: string;
    flag?: string;
    active: boolean;
    use_case?: string;
    speed?: string;
    protocol?: string;
    transport?: string;
    security?: string;
    is_new?: boolean;
  }

  interface NodeHealth {
    alive: boolean;
    delay?: number;
    http_code?: number;
    tested?: boolean;
  }

  let {
    subId = '',
    enableXray = false,
    enableMihomo = false,
    source = 'xray',
    nodes = [],
    health = {},
    checkingNodes = {},
    onSetActiveNode,
    onCheckNodeHealth
  }: {
    subId: string;
    enableXray: boolean;
    enableMihomo: boolean;
    source: 'mihomo' | 'xray';
    nodes: Node[];
    health: Record<string, NodeHealth>;
    checkingNodes: Record<string, boolean>;
    onSetActiveNode: (subId: string, tag: string) => void;
    onCheckNodeHealth: (subId: string, tag: string) => void;
  } = $props();

  let flagsSupported = $state(true);

  function getCountryColorStyle(countryCode: string | undefined): string {
    if (!countryCode) return '';
    const code = countryCode.toUpperCase();
    const styles: Record<string, string> = {
      EU: 'background: linear-gradient(135deg, #0b3c98, #072561); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3); border-color: rgba(41, 194, 240, 0.3);',
      RU: 'background: linear-gradient(135deg, #1e88e5, #e53935); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3);',
      DE: 'background: linear-gradient(135deg, #ffb300, #ff3d00, #212121); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      NL: 'background: linear-gradient(135deg, #ff7043, #d84315); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3);',
      PL: 'background: linear-gradient(180deg, #ffffff 50%, #e91e63 50%); color: #333333; box-shadow: inset 0 0 4px rgba(0,0,0,0.15); border-color: rgba(255, 255, 255, 0.1);',
      FI: 'background: linear-gradient(135deg, #ffffff 40%, #0d47a1 40%); color: #0d47a1;',
      LT: 'background: linear-gradient(135deg, #4caf50, #ffeb3b, #f44336); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      EE: 'background: linear-gradient(135deg, #29b6f6, #212121, #ffffff); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      ES: 'background: linear-gradient(135deg, #e53935, #ffeb3b, #e53935); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);',
      US: 'background: linear-gradient(135deg, #0d47a1, #b71c1c); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.3);',
      AM: 'background: linear-gradient(135deg, #e53935, #0d47a1, #ffb300); color: #ffffff; text-shadow: 0 1px 2px rgba(0,0,0,0.4);'
    };
    return (
      styles[code] ??
      'background: linear-gradient(135deg, #424242, #212121); color: var(--fg-primary);'
    );
  }

  function latencyClass(h: NodeHealth | undefined): string {
    if (!h) return '';
    if (!h.alive) return 'latency-timeout';
    if (!h.delay) return '';
    if (h.delay < 150) return 'latency-fast';
    if (h.delay < 400) return 'latency-medium';
    return 'latency-slow';
  }

  function latencyLabel(h: NodeHealth | undefined): string {
    if (!h) return '';
    if (!h.alive) return 'timeout';
    if (!h.delay) return '';
    return `${h.delay} ms`;
  }

  onMount(() => {
    try {
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');
      if (ctx) {
        ctx.fillStyle = '#000';
        ctx.textBaseline = 'top';
        ctx.font = '32px Arial';
        ctx.fillText('🇺🇸', 0, 0);
        const widthFlag = ctx.measureText('🇺🇸').width;
        const widthLetters = ctx.measureText('US').width;
        flagsSupported = widthFlag > widthLetters;
      }
    } catch (e) {
      flagsSupported = false;
    }
  });
</script>

<div class="inline-nodes-list">
  {#each nodes as node}
    {@const h = health[node.tag]}
    {@const isNodeActive = node.active}
    {@const metaText =
      node.use_case || node.speed
        ? `${node.use_case || ''}${node.use_case && node.speed ? ' - ' : ''}${node.speed || ''}`
        : `${node.protocol || ''}${node.protocol && node.transport ? ' · ' + node.transport : ''}${node.security && node.security !== 'none' ? ' · ' + node.security : ''}`}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="sub-node-row"
      class:active={isNodeActive}
      onclick={() => {
        if (enableXray) {
          onSetActiveNode(subId, node.tag);
        }
      }}
    >
      {#if isNodeActive}
        <div class="sub-node-active-bar"></div>
      {/if}

      <!-- Flag Avatar -->
      <div
        class="sub-node-avatar-container"
        class:active={isNodeActive}
        style={(!flagsSupported || !node.flag) && node.country
          ? getCountryColorStyle(node.country)
          : ''}
      >
        {#if flagsSupported && node.flag}
          <span class="sub-node-flag">{node.flag}</span>
        {:else if node.country}
          <span class="sub-node-avatar-text">{node.country}</span>
        {:else}
          <span class="sub-node-flag-fallback">🌐</span>
        {/if}
      </div>

      <!-- Text Info -->
      <div class="sub-node-info">
        <div class="sub-node-name-row">
          <span class="sub-node-name">
            {node.name || $t('country.' + node.country) || node.tag}
            {#if node.is_new}
              <span class="sub-node-name-new"> [NEW]</span>
            {/if}
          </span>
        </div>
        <div class="sub-node-meta-row">
          {#if metaText}
            <span class="sub-node-chip-blue">{metaText}</span>
          {/if}
        </div>
      </div>

      <!-- Status / Ping right -->
      <div class="sub-node-status-container">
        <span class="sub-node-chip-gold">{enableMihomo ? 'YAML' : 'JSON'}</span>

        <button
          class="sub-node-ping-btn"
          onclick={(e) => {
            e.stopPropagation();
            onCheckNodeHealth(subId, node.tag);
          }}
          disabled={checkingNodes[node.tag]}
          title="Проверить пинг"
        >
          {#if checkingNodes[node.tag]}
            <span class="spinner-xs"></span>
          {:else if source === 'mihomo'}
            {#if h && h.tested}
              <span class="sub-node-ping-val {latencyClass(h)}">{latencyLabel(h)}</span>
              {#if h.alive}
                <div class="sub-node-status-icon success">
                  <svg
                    width="8"
                    height="8"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="4"
                  >
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                </div>
              {:else}
                <div class="sub-node-status-icon danger">
                  <svg
                    width="8"
                    height="8"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="4"
                  >
                    <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                </div>
              {/if}
            {:else}
              <span style="color: var(--fg-faint);">—</span>
            {/if}
          {:else}
            {#if h}
              <span class="sub-node-ping-val {latencyClass(h)}">{latencyLabel(h)}</span>
              {#if h.alive}
                <div class="sub-node-status-icon success">
                  <svg
                    width="8"
                    height="8"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="4"
                  >
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                </div>
              {:else}
                <div class="sub-node-status-icon danger">
                  <svg
                    width="8"
                    height="8"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="4"
                  >
                    <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                </div>
              {/if}
            {:else}
              <div class="sub-node-status-icon default-ok">
                <svg
                  width="8"
                  height="8"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="4"
                >
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </div>
            {/if}
          {/if}
        </button>
      </div>
    </div>
  {/each}
</div>

<style>
  .inline-nodes-list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-md, 6px);
    overflow: hidden;
    background: var(--bg-card);
  }

  .sub-node-row {
    position: relative;
    display: flex;
    align-items: center;
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .sub-node-row:last-child {
    border-bottom: none;
  }

  .sub-node-row:hover {
    background: rgba(255, 255, 255, 0.02);
  }

  .sub-node-row.active {
    background: rgba(25, 118, 210, 0.05);
  }

  .sub-node-active-bar {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--accent);
  }

  .sub-node-avatar-container {
    width: 32px;
    height: 32px;
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.05);
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 12px;
    flex-shrink: 0;
    font-size: 16px;
    transition: all var(--transition-fast);
    color: var(--fg-secondary);
  }

  .sub-node-avatar-container.active {
    background: var(--accent);
    border-color: var(--accent);
    color: white;
  }

  .sub-node-avatar-text {
    font-size: 11px;
    font-weight: 800;
    text-transform: uppercase;
    color: inherit;
    letter-spacing: 0.02em;
  }

  .sub-node-avatar-container.active .sub-node-avatar-text {
    color: white;
  }

  .sub-node-flag-fallback {
    font-size: 14px;
  }

  .sub-node-info {
    flex: 1;
    min-width: 0;
  }

  .sub-node-name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 1px;
  }

  .sub-node-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sub-node-name-new {
    color: #f59e0b;
    font-weight: 700;
    font-size: 11px;
    letter-spacing: 0.02em;
  }

  .sub-node-chip-blue {
    background: rgba(41, 194, 240, 0.08);
    border: 1px solid rgba(41, 194, 240, 0.2);
    color: #7dd3fc;
    padding: 2px 10px;
    border-radius: 12px;
    font-size: 11px;
    font-weight: 500;
    display: inline-flex;
    align-items: center;
    margin-top: 3px;
    max-width: 100%;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sub-node-row.active .sub-node-chip-blue {
    background: rgba(255, 255, 255, 0.12);
    border-color: rgba(255, 255, 255, 0.25);
    color: #fff;
  }

  .sub-node-chip-gold {
    background: rgba(245, 158, 11, 0.07);
    border: 1px solid rgba(245, 158, 11, 0.2);
    color: #f59e0b;
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 9.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    display: inline-block;
    text-transform: uppercase;
    flex-shrink: 0;
  }
  .sub-node-row.active .sub-node-chip-gold {
    background: rgba(255, 255, 255, 0.15);
    border-color: rgba(255, 255, 255, 0.3);
    color: #fff;
  }

  .sub-node-meta-row {
    display: flex;
    align-items: center;
  }

  .sub-node-status-container {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
    margin-left: 12px;
  }

  .sub-node-ping-btn {
    background: none;
    border: none;
    padding: 0;
    margin: 0;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    transition: opacity var(--transition-fast);
  }

  .sub-node-ping-btn:hover {
    opacity: 0.8;
  }

  .sub-node-ping-btn:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  .sub-node-ping-val {
    font-size: 10.5px;
    font-family: var(--font-family-mono);
    color: var(--fg-dim);
  }

  .sub-node-status-icon {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .sub-node-status-icon.success {
    background: rgba(34, 197, 94, 0.15);
    border: 1px solid rgba(34, 197, 94, 0.3);
    color: #22c55e;
  }

  .sub-node-status-icon.danger {
    background: rgba(239, 68, 68, 0.15);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: var(--danger);
  }

  .sub-node-status-icon.default-ok {
    background: rgba(34, 197, 94, 0.15);
    border: 1px solid rgba(34, 197, 94, 0.3);
    color: #22c55e;
  }

  .latency-fast {
    color: #22c55e;
  }
  .latency-medium {
    color: #f59e0b;
  }
  .latency-slow {
    color: var(--danger);
  }
  .latency-timeout {
    color: var(--fg-faint);
  }
</style>
