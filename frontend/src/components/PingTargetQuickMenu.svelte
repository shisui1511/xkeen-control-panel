<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../i18n';
  import { pingTargetStore, setPingTargetConfig, type PingPreset } from '../lib/pingTargetStore';

  let isOpen = $state(false);

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

  // Контейнер держит и кнопку, и выпадающее меню: клик вне него закрывает меню.
  const dismissOnOutside: Attachment<HTMLElement> = (container) => {
    function handlePointerDown(e: PointerEvent) {
      const target = e.target as Node | null;
      if (isOpen && !container.contains(target)) {
        isOpen = false;
      }
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape' && isOpen) {
        isOpen = false;
      }
    }

    window.addEventListener('pointerdown', handlePointerDown, true);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('pointerdown', handlePointerDown, true);
      window.removeEventListener('keydown', handleKeyDown);
    };
  };
</script>

<div class="quick-ping-menu-container" {@attach dismissOnOutside}>
  <button
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
    <div class="quick-ping-dropdown" role="menu">
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
          {#each [2000, 5000, 8000, 10000] as to (to)}
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
    padding: var(--spacing-1-5) var(--spacing-2);
    color: var(--fg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-card);
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      border-color var(--transition-fast);
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
      0 0 0 1px var(--surface-tint);
    padding: var(--spacing-2) 0;
    animation: menuSlide var(--duration-fast) var(--ease-out) forwards;
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
    font-size: var(--font-size-xs);
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--fg-dim);
    padding: var(--spacing-1) var(--spacing-3) var(--spacing-1-5);
  }

  .dropdown-section {
    padding: var(--spacing-0-5) var(--spacing-1-5);
  }

  .section-title {
    font-size: var(--font-size-xs);
    color: var(--fg-faint);
    padding: var(--spacing-0-5) var(--spacing-1-5) var(--spacing-1);
    font-weight: 600;
  }

  .dropdown-item {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--spacing-1-5) var(--spacing-2);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: var(--font-size-xs);
    cursor: pointer;
    text-align: left;
    transition: background var(--transition-fast);
  }

  .dropdown-item:hover {
    background: var(--hover);
  }

  .dropdown-item.selected {
    color: var(--accent);
    font-weight: 600;
    background: color-mix(in srgb, var(--accent) 8%, transparent);
  }

  .check-mark {
    font-size: var(--font-size-xs);
    color: var(--accent);
  }

  .dropdown-divider {
    height: 1px;
    background: var(--border);
    margin: var(--spacing-1-5) 0;
  }

  .timeout-chips {
    display: flex;
    gap: var(--spacing-1);
    padding: var(--spacing-0-5) var(--spacing-1);
  }

  .timeout-chip {
    flex: 1;
    padding: var(--spacing-1) 0;
    font-size: var(--font-size-xs);
    font-family: var(--font-family-mono);
    text-align: center;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-secondary);
    cursor: pointer;
    transition:
      color var(--transition-fast),
      background var(--transition-fast),
      border-color var(--transition-fast);
  }

  .timeout-chip:hover {
    border-color: var(--border-strong);
    color: var(--fg-primary);
  }

  .timeout-chip.selected {
    background: var(--accent);
    color: var(--btn-primary-text);
    border-color: var(--accent);
    font-weight: 700;
  }
</style>
