<script lang="ts">
  import { t } from '../i18n';
  import { isSidebarOpen, isConflict, isMihomo, mihomoApiAvailable, navCaps } from '../stores';
  import { showMihomoNavFor } from '../lib/navCaps';
  import Icon from '../lib/components/Icon.svelte';

  // Нижняя панель разделов на телефоне (≤768 px): четыре частых раздела под большим
  // пальцем и «Ещё» — открывает то же выдвижное меню, что и кнопка в шапке.
  // Активный раздел подсвечен «светодиодом» над иконкой — как порт на корпусе роутера.
  let {
    currentTab = 'dashboard',
    onOpenMenu = () => {}
  }: {
    currentTab?: string;
    onOpenMenu?: () => void;
  } = $props();

  // Второй пункт зависит от ядра: при Mihomo — «Прокси», иначе — «Редактор» (Xray
  // правится файлами). Пока срез неизвестен (null), показываем нейтральный «Редактор».
  const showMihomoNav = $derived($isConflict ? false : showMihomoNavFor($navCaps) === true);
  const showApiBadge = $derived($isMihomo && !$mihomoApiAvailable);

  const items = $derived([
    { tab: 'dashboard', icon: 'dashboard', label: $t('nav.dashboard') },
    showMihomoNav
      ? { tab: 'proxies', icon: 'proxies', label: $t('nav.proxies'), badge: showApiBadge }
      : { tab: 'editor', icon: 'editor', label: $t('nav.editor') },
    { tab: 'logs', icon: 'logs', label: $t('nav.logs') },
    { tab: 'services', icon: 'services', label: $t('nav.services') }
  ]);

  // Страница, которой нет в панели, — значит, она в меню: «Ещё» горит, чтобы было видно,
  // откуда пришли.
  const inMenu = $derived(!items.some((i) => i.tab === currentTab));
</script>

<nav class="bottom-dock" aria-label={$t('nav.dock_aria')}>
  {#each items as item (item.tab)}
    <a
      href="#/{item.tab}"
      class="dock-item"
      aria-current={currentTab === item.tab ? 'page' : undefined}
      onclick={() => isSidebarOpen.set(false)}
    >
      <span class="dock-led" aria-hidden="true"></span>
      <span class="dock-icon">
        <Icon name={item.icon} size={20} />
        {#if item.badge}
          <span class="dock-badge" role="img" aria-label={$t('dash.quickstart.sidebar_badge_aria')}
          ></span>
        {/if}
      </span>
      <span class="dock-label">{item.label}</span>
    </a>
  {/each}
  <button
    type="button"
    class="dock-item"
    class:is-lit={inMenu}
    aria-haspopup="dialog"
    aria-expanded={$isSidebarOpen}
    onclick={onOpenMenu}
  >
    <span class="dock-led" aria-hidden="true"></span>
    <span class="dock-icon">
      <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <circle cx="4" cy="10" r="1.8" />
        <circle cx="10" cy="10" r="1.8" />
        <circle cx="16" cy="10" r="1.8" />
      </svg>
    </span>
    <span class="dock-label">{$t('nav.more')}</span>
  </button>
</nav>

<style>
  .bottom-dock {
    display: none;
  }

  @media (max-width: 768px) {
    .bottom-dock {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 90;
      display: grid;
      grid-template-columns: repeat(5, 1fr);
      height: calc(var(--dock-h) + env(safe-area-inset-bottom));
      padding: 0 max(4px, env(safe-area-inset-left)) env(safe-area-inset-bottom)
        max(4px, env(safe-area-inset-right));
      /* Тёмная во всех темах, как шапка: светлые чернила задаются здесь, а не
         токенами текста, которые в «Тумане» тёмные */
      background: var(--bg-deep);
      border-top: 1px solid var(--on-deep-line);
      color: var(--on-deep-dim);
    }

    /* Тёмное стекло, как у шапки: список, уходящий под панель, просвечивает */
    @supports (backdrop-filter: blur(1px)) or (-webkit-backdrop-filter: blur(1px)) {
      .bottom-dock {
        background: color-mix(in srgb, var(--bg-deep) 88%, transparent);
        -webkit-backdrop-filter: blur(18px) saturate(1.6);
        backdrop-filter: blur(18px) saturate(1.6);
      }
    }
  }

  /* Телефон боком: панель ниже (--dock-h 44 px), подпись встаёт рядом со значком */
  @media (max-width: 768px) and (max-height: 480px) {
    .bottom-dock .dock-item {
      flex-direction: row;
      gap: 6px;
      padding: 0 4px;
    }
  }

  .dock-item {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 3px;
    min-width: 0;
    padding: 6px 2px 4px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-decoration: none;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
    transition: color var(--transition-fast);
  }

  .dock-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -4px;
    border-radius: var(--radius-md);
  }

  /* Светодиод: в покое — тусклая метка порта, у активного раздела — горит акцентом */
  .dock-led {
    position: absolute;
    top: -1px;
    left: 50%;
    width: 12px;
    height: 3px;
    border-radius: 0 0 3px 3px;
    background: var(--on-deep-led);
    transform: translateX(-50%);
    transition:
      width var(--duration-move) var(--ease-out),
      background-color var(--transition-fast),
      box-shadow var(--transition-fast);
  }

  .dock-icon {
    position: relative;
    display: grid;
    place-items: center;
    width: 28px;
    height: 24px;
    transition: transform var(--transition-fast);
  }

  .dock-label {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-2xs);
    font-weight: 600;
    line-height: 1.2;
  }

  .dock-item[aria-current='page'],
  .dock-item.is-lit {
    color: var(--on-deep);
  }

  .dock-item[aria-current='page'] .dock-led,
  .dock-item.is-lit .dock-led {
    width: 32px;
    background: var(--accent);
    box-shadow:
      0 0 8px var(--accent),
      0 0 18px rgba(var(--accent-rgb), 0.45);
  }

  .dock-item:active .dock-icon {
    transform: scale(0.9);
  }

  .dock-badge {
    position: absolute;
    top: 0;
    right: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--warning);
    box-shadow: 0 0 0 2px var(--bg-deep);
  }

  @media (prefers-reduced-motion: reduce) {
    .dock-led,
    .dock-icon {
      transition: none;
    }
  }
</style>
