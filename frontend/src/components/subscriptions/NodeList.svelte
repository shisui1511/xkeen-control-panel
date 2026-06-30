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
  }

  let {
    subId = '',
    enableXray = false,
    enableMihomo = false,
    nodes = [],
    health = {},
    checkingNodes = {},
    onSetActiveNode,
    onCheckNodeHealth
  }: {
    subId: string;
    enableXray: boolean;
    enableMihomo: boolean;
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
          {:else if h}
            <span class="sub-node-ping-val {latencyClass(h)}"
              >{latencyLabel(h)}</span
            >
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
                  <line x1="18" y1="6" x2="6" y2="18" /><line
                    x1="6"
                    y1="6"
                    x2="18"
                    y2="18"
                  />
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
        </button>
      </div>
    </div>
  {/each}
</div>
