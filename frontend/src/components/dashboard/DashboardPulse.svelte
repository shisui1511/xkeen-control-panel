<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { t, tp } from '../../i18n';
  import { trafficStream, formatTrafficSpeed, type TrafficState } from '../../lib/trafficStream';
  import Icon from '../../lib/components/Icon.svelte';

  // «Пульт» — первый экран дашборда на телефоне: одним взглядом ответ на
  // вопрос «всё работает?» (ядро, живая скорость, подключения) и одно главное
  // действие по ситуации. Замечания — строкой, которая ведёт к их списку ниже.
  let {
    kernel = '',
    kernelStatus = 'loading',
    xkeenInstalled = true,
    problemCount = 0,
    onShowProblems,
    onSwitchTab
  }: {
    /** Активное ядро: 'mihomo' | 'xray' | ''. */
    kernel?: string;
    /** Статус активного ядра из /api/service/status. */
    kernelStatus?: string;
    xkeenInstalled?: boolean;
    problemCount?: number;
    onShowProblems?: () => void;
    onSwitchTab: (tab: string) => void;
  } = $props();

  let traffic = $state<TrafficState>(trafficStream.getState());
  let unsub: (() => void) | null = null;
  onMount(() => {
    unsub = trafficStream.subscribe((s) => (traffic = s));
  });
  onDestroy(() => unsub?.());

  const kernelName = $derived(kernel === 'mihomo' ? 'Mihomo' : kernel === 'xray' ? 'Xray' : '');

  // setup — настройка не завершена (нет XKeen или ядра): не авария, акцентный тон как у
  // баннера «Следующий шаг» на десктопе
  type Tone = 'ok' | 'warn' | 'bad' | 'idle' | 'setup';
  const pulse = $derived.by((): { tone: Tone; title: string; sub: string } => {
    if (!xkeenInstalled) {
      return {
        tone: 'setup',
        title: $t('dash.pulse.no_xkeen'),
        sub: $t('dash.pulse.no_xkeen_sub')
      };
    }
    if (kernelStatus === 'loading') {
      return { tone: 'idle', title: $t('dash.pulse.checking'), sub: '' };
    }
    if (!kernelName || kernelStatus === 'not_installed') {
      return {
        tone: 'setup',
        title: $t('dash.pulse.no_kernel'),
        sub: $t('dash.pulse.no_kernel_sub')
      };
    }
    if (kernelStatus !== 'running') {
      return {
        tone: 'bad',
        title: $t('dash.pulse.stopped', { kernel: kernelName }),
        sub: $t('dash.pulse.stopped_sub')
      };
    }
    return {
      tone: problemCount > 0 ? 'warn' : 'ok',
      title: $t('dash.pulse.running', { kernel: kernelName }),
      sub: problemCount > 0 ? $t('dash.pulse.running_warn_sub') : $t('dash.pulse.running_sub')
    };
  });

  const running = $derived(pulse.tone === 'ok' || pulse.tone === 'warn');

  const action = $derived.by(() => {
    // Та же подпись, что у кнопки следующего шага на десктопе
    if (!xkeenInstalled) return { label: $t('dash.problems.xkeen_missing_cta'), tab: 'services' };
    if (pulse.tone === 'setup')
      return { label: $t('dash.problems.kernel_missing_cta'), tab: 'services' };
    if (!running) return { label: $t('dash.pulse.open_services'), tab: 'services' };
    return kernel === 'mihomo'
      ? { label: $t('dash.pulse.open_proxies'), tab: 'proxies' }
      : { label: $t('dash.pulse.open_editor'), tab: 'editor' };
  });
</script>

<section class="pulse pulse-{pulse.tone}" aria-label={$t('dash.pulse.aria')}>
  <div class="pulse-head">
    <span class="pulse-led" aria-hidden="true"></span>
    <div class="pulse-state">
      <h2 class="pulse-title">{pulse.title}</h2>
      {#if pulse.sub}<p class="pulse-sub">{pulse.sub}</p>{/if}
    </div>
  </div>

  {#if running}
    <dl class="pulse-metrics">
      <div>
        <dt>{$t('dash.pulse.down')}</dt>
        <dd>{formatTrafficSpeed(traffic.down)}</dd>
      </div>
      <div>
        <dt>{$t('dash.pulse.up')}</dt>
        <dd>{formatTrafficSpeed(traffic.up)}</dd>
      </div>
      <div>
        <dt>{$t('dash.pulse.connections')}</dt>
        <dd>{traffic.connections}</dd>
      </div>
    </dl>
  {/if}

  <button
    type="button"
    class="pulse-action"
    class:is-primary={!running}
    onclick={() => onSwitchTab(action.tab)}
  >
    {action.label}
    <Icon name="chevron-right" size={16} />
  </button>

  {#if problemCount > 0}
    <button type="button" class="pulse-problems" onclick={() => onShowProblems?.()}>
      <Icon name="warning" size={16} />
      <span>{$tp('dash.pulse.problems', problemCount)}</span>
      <Icon name="chevron-down" size={16} />
    </button>
  {/if}
</section>

<style>
  .pulse {
    display: none;
  }

  @media (max-width: 768px) {
    .pulse {
      --tone: var(--success);
      display: flex;
      flex-direction: column;
      gap: var(--spacing-3-5);
      margin-bottom: var(--spacing-4);
      padding: var(--spacing-4);
      border: 1px solid var(--border);
      border-radius: var(--radius-lg);
      background: var(--bg-card);
      box-shadow: var(--shadow-sm);
    }
  }

  .pulse-warn {
    --tone: var(--warning);
  }

  .pulse-bad {
    --tone: var(--danger);
  }

  .pulse-idle {
    --tone: var(--fg-dim);
  }

  .pulse-setup {
    --tone: var(--accent);
  }

  .pulse-head {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-3);
  }

  /* Светодиод состояния — тот же язык, что у нижней панели и капсулы в шапке */
  .pulse-led {
    flex-shrink: 0;
    width: 12px;
    height: 12px;
    margin-top: var(--spacing-1-5);
    border-radius: 50%;
    background: var(--tone);
    box-shadow: 0 0 0 4px color-mix(in srgb, var(--tone) 18%, transparent);
  }

  .pulse-state {
    min-width: 0;
  }

  .pulse-title {
    margin: 0;
    font-size: var(--font-size-xl);
    line-height: 1.25;
  }

  .pulse-sub {
    margin: var(--spacing-0-5) 0 0;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }

  .pulse-metrics {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: var(--spacing-3);
    margin: 0;
  }

  .pulse-metrics dt {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .pulse-metrics dd {
    margin: var(--spacing-0-5) 0 0;
    font-size: var(--font-size-lg);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .pulse-action {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-1);
    min-height: 44px;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-elevated);
    color: var(--accent-text);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }

  .pulse-action.is-primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--btn-primary-text);
  }

  .pulse-action:active,
  .pulse-problems:active {
    transform: scale(0.98);
  }

  .pulse-problems {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-height: 40px;
    margin: calc(-1 * var(--spacing-1)) calc(-1 * var(--spacing-1-5)) calc(-1 * var(--spacing-1-5));
    padding: 0 var(--spacing-1-5);
    border: 0;
    border-radius: var(--radius-md);
    background: none;
    color: var(--warning);
    font: inherit;
    font-size: var(--font-size-sm);
    font-weight: 600;
    text-align: left;
    cursor: pointer;
  }

  .pulse-problems span {
    flex: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .pulse-action:active,
    .pulse-problems:active {
      transform: none;
    }
  }
</style>
