<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../i18n';
  import { pingTargetStore, setPingTargetConfig, type PingPreset } from '../lib/pingTargetStore';

  let isOpen = $state(false);
  let menuEl: HTMLElement | null = $state(null);
  let btnEl: HTMLElement | null = $state(null);

  let config = $derived($pingTargetStore);

  function toggleOpen() {
    isOpen = !isOpen;
  }

  function handlePresetSelect(preset: PingPreset) {
    setPingTargetConfig({ preset });
  }

  function handleTimeoutSelect(timeoutMs: number) {
    setPingTargetConfig({ timeoutMs });
  }

  function handlePointerDown(e: PointerEvent) {
    const target = e.target as Node | null;
    if (isOpen && menuEl && !menuEl.contains(target) && btnEl && !btnEl.contains(target)) {
      isOpen = false;
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isOpen) {
      isOpen = false;
    }
  }

  onMount(() => {
    window.addEventListener('pointerdown', handlePointerDown, true);
    window.addEventListener('keydown', handleKeyDown);
  });

  onDestroy(() => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('pointerdown', handlePointerDown, true);
      window.removeEventListener('keydown', handleKeyDown);
    }
  });
</script>

<div class="quick-ping-menu-container">
  <button
    bind:this={btnEl}
    type="button"
    class="btn btn-ghost btn-icon {isOpen ? 'active' : ''}"
    onclick={toggleOpen}
    title={$t('settings.ping_target_title')}
    aria-label={$t('settings.ping_target_title')}
    aria-haspopup="true"
    aria-expanded={isOpen}
  >
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <circle cx="12" cy="12" r="3"></circle>
      <path
        d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"
      ></path>
    </svg>
  </button>

  {#if isOpen}
    <div bind:this={menuEl} class="quick-ping-dropdown" role="menu">
      <div class="dropdown-header">
        {$t('settings.ping_target_title')}
      </div>

      <div class="dropdown-section">
        <div class="section-title">{$t('settings.ping_preset_label')}</div>
        <button
          type="button"
          class="dropdown-item {config.preset === 'google' ? 'selected' : ''}"
          onclick={() => handlePresetSelect('google')}
        >
          <span>{$t('settings.ping_preset_google')}</span>
          {#if config.preset === 'google'}<span class="check-mark">✓</span>{/if}
        </button>
        <button
          type="button"
          class="dropdown-item {config.preset === 'cloudflare' ? 'selected' : ''}"
          onclick={() => handlePresetSelect('cloudflare')}
        >
          <span>{$t('settings.ping_preset_cloudflare')}</span>
          {#if config.preset === 'cloudflare'}<span class="check-mark">✓</span>{/if}
        </button>
        <button
          type="button"
          class="dropdown-item {config.preset === 'yandex' ? 'selected' : ''}"
          onclick={() => handlePresetSelect('yandex')}
        >
          <span>{$t('settings.ping_preset_yandex')}</span>
          {#if config.preset === 'yandex'}<span class="check-mark">✓</span>{/if}
        </button>
        <button
          type="button"
          class="dropdown-item {config.preset === 'custom' ? 'selected' : ''}"
          onclick={() => handlePresetSelect('custom')}
        >
          <span>{$t('settings.ping_preset_custom')}</span>
          {#if config.preset === 'custom'}<span class="check-mark">✓</span>{/if}
        </button>
      </div>

      <div class="dropdown-divider"></div>

      <div class="dropdown-section">
        <div class="section-title">{$t('settings.ping_timeout_label')}</div>
        <div class="timeout-chips">
          {#each [2000, 5000, 8000, 10000] as to}
            <button
              type="button"
              class="timeout-chip {config.timeoutMs === to ? 'selected' : ''}"
              onclick={() => handleTimeoutSelect(to)}
            >
              {to / 1000}s
            </button>
          {/each}
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .quick-ping-menu-container {
    position: relative;
    display: inline-flex;
  }

  .btn-icon {
    padding: 6px 8px;
    color: var(--fg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-card);
    transition: all 0.15s ease;
  }

  .btn-icon:hover,
  .btn-icon.active {
    color: var(--fg-primary);
    border-color: var(--accent);
    background: var(--hover);
  }

  .quick-ping-dropdown {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 1050;
    width: 220px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow:
      0 10px 24px -4px rgba(0, 0, 0, 0.55),
      0 0 0 1px rgba(255, 255, 255, 0.05);
    padding: 8px 0;
    animation: menuSlide 0.14s ease-out forwards;
  }

  @keyframes menuSlide {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .dropdown-header {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--fg-dim);
    padding: 4px 12px 6px;
  }

  .dropdown-section {
    padding: 2px 6px;
  }

  .section-title {
    font-size: 10px;
    color: var(--fg-faint);
    text-transform: uppercase;
    padding: 2px 6px 4px;
    font-weight: 600;
  }

  .dropdown-item {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    cursor: pointer;
    text-align: left;
    transition: background 0.12s ease;
  }

  .dropdown-item:hover {
    background: var(--hover);
  }

  .dropdown-item.selected {
    color: var(--accent);
    font-weight: 600;
    background: rgba(41, 194, 240, 0.08);
  }

  .check-mark {
    font-size: 11px;
    color: var(--accent);
  }

  .dropdown-divider {
    height: 1px;
    background: var(--border);
    margin: 6px 0;
  }

  .timeout-chips {
    display: flex;
    gap: 4px;
    padding: 2px 4px;
  }

  .timeout-chip {
    flex: 1;
    padding: 4px 0;
    font-size: 11px;
    font-family: var(--font-family-mono);
    text-align: center;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-secondary);
    cursor: pointer;
    transition: all 0.12s ease;
  }

  .timeout-chip:hover {
    border-color: var(--border-strong);
    color: var(--fg-primary);
  }

  .timeout-chip.selected {
    background: var(--accent);
    color: #03182a;
    border-color: var(--accent);
    font-weight: 700;
  }
</style>
