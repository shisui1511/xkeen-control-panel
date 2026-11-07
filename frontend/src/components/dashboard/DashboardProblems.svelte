<script lang="ts">
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import { capabilities, activeKernelName } from '../../stores';
  import { kernelLabel } from '../../lib/kernelState';
  import { formatBytes } from '../../lib/format';
  import type { NextStep } from '../../lib/nextStep';

  // Замечания дашборда: сторожевой таймер, сбой ядра, место на диске, сертификат, ошибка
  // конфигурации, API Mihomo и следующий шаг настройки. Каждое — строка с тоном, текстом и
  // одним действием. Нет ни одного — блока нет. Класс .dash-alerts — цель прокрутки «пульта».

  interface WatchdogStatus {
    state: string;
    last_disarm_error: string;
  }

  interface Props {
    invalidConfig: boolean;
    diskFree: number | null;
    sslCertDays: number | null;
    watchdogStatus: WatchdogStatus | null;
    isWatchdogIncident: boolean;
    isResettingWatchdog: boolean;
    /** Статус службы не обновился: подпись «данные могли устареть» у сторожевого таймера. */
    statusStale: boolean;
    isKernelCrashed: boolean;
    isDiskLow: boolean;
    isSSLExpiring: boolean;
    nextStep: NextStep | null;
    nextStepKernelName: string;
    /** Шаг уже назван «пультом» (телефон): строка скрыта там и не дублируется. */
    pulseCoversStep: boolean;
    onResetWatchdog: () => void;
    onRestartXkeen: () => void;
    onMigrateMihomo: () => void;
    onSwitchTab: (tab: string) => void;
  }

  let {
    invalidConfig,
    diskFree,
    sslCertDays,
    watchdogStatus,
    isWatchdogIncident,
    isResettingWatchdog,
    statusStale,
    isKernelCrashed,
    isDiskLow,
    isSSLExpiring,
    nextStep,
    nextStepKernelName,
    pulseCoversStep,
    onResetWatchdog,
    onRestartXkeen,
    onMigrateMihomo,
    onSwitchTab
  }: Props = $props();

  function openConstructor() {
    window.location.hash = '#/constructor';
  }

  const mihomoApiDown = $derived(
    $capabilities !== null &&
      !$capabilities?.mihomo?.api_reachable &&
      $capabilities?.mihomo?.process_running === true
  );
  const mihomoInsecure = $derived(
    $capabilities !== null && $capabilities?.mihomo?.is_insecure_lan === true
  );

  interface Problem {
    key: string;
    tone: 'error' | 'warning' | 'step';
    title: string;
    desc: string;
    /** Подпись «Следующий шаг» над заголовком. */
    step?: boolean;
    cta?: { label: string; primary?: boolean; run: () => void };
    testid?: string;
    dataStep?: string;
    inPulse?: boolean;
  }

  // Следующий шаг настройки — одна строка из пяти возможных
  const stepProblem = $derived.by((): Problem | null => {
    const services = () => onSwitchTab('services');
    switch (nextStep) {
      case 'install_xkeen':
        return {
          key: 'step',
          tone: 'step',
          title: $t('dash.problems.xkeen_missing_title'),
          desc: $t('dash.problems.xkeen_missing_desc'),
          cta: { label: $t('dash.problems.xkeen_missing_cta'), primary: true, run: services },
          testid: 'problem-xkeen-missing',
          inPulse: pulseCoversStep
        };
      case 'finish_xkeen_setup':
        return {
          key: 'step',
          tone: 'step',
          title: $t('dash.problems.xkeen_incomplete_title'),
          desc: $t('dash.problems.xkeen_incomplete_desc'),
          cta: { label: $t('dash.problems.xkeen_incomplete_cta'), primary: true, run: services }
        };
      case 'install_kernel':
        return {
          key: 'step',
          tone: 'step',
          title: $t('dash.problems.kernel_missing_title'),
          desc: $t('dash.problems.kernel_missing_desc'),
          cta: { label: $t('dash.problems.kernel_missing_cta'), primary: true, run: services },
          inPulse: pulseCoversStep
        };
      case 'configure':
        return {
          key: 'step',
          tone: 'step',
          title: $t('dash.problems.configure_title'),
          desc: $t('dash.problems.configure_desc', { kernel: nextStepKernelName }),
          cta: { label: $t('dash.problems.configure_cta'), primary: true, run: openConstructor }
        };
      case 'start':
        return {
          key: 'step',
          tone: 'step',
          title: $t('dash.problems.start_title'),
          desc: $t('dash.problems.start_desc'),
          cta: { label: $t('dash.problems.start_cta'), primary: true, run: services },
          inPulse: pulseCoversStep
        };
      default:
        return null;
    }
  });

  // Порядок строк: сначала то, что сломалось, затем следующий шаг, затем предупреждения
  const problems = $derived.by((): Problem[] => {
    const list: Problem[] = [];
    if (isKernelCrashed) {
      list.push({
        key: 'crash',
        tone: 'error',
        title: $t('dash.problems.kernel_crash_title'),
        desc: $t('dash.problems.kernel_crash_desc', { kernel: kernelLabel($activeKernelName) }),
        cta: { label: $t('dash.problems.kernel_crash_cta'), run: onRestartXkeen }
      });
    }
    if (isDiskLow && diskFree !== null) {
      list.push({
        key: 'disk',
        tone: 'error',
        title: $t('dash.problems.disk_low_title'),
        desc: $t('dash.problems.disk_low_desc', { free: formatBytes(diskFree) }),
        cta: { label: $t('dash.problems.disk_low_cta'), run: () => onSwitchTab('settings') }
      });
    }
    if (isSSLExpiring && sslCertDays !== null) {
      list.push({
        key: 'ssl',
        tone: 'warning',
        title: $t('dash.problems.ssl_expire_title'),
        desc: $t('dash.problems.ssl_expire_desc', { days: sslCertDays })
      });
    }
    if (invalidConfig) {
      list.push({
        key: 'config',
        tone: 'error',
        title: $t('dash.problems.invalid_config_title'),
        desc: $t('dash.problems.invalid_config_desc'),
        cta: { label: $t('dash.problems.invalid_config_cta'), run: () => onSwitchTab('editor') }
      });
    }
    if (mihomoApiDown) {
      list.push({
        key: 'api',
        tone: 'warning',
        title: $t('dash.problems.mihomo_api_title'),
        desc: $t('dash.problems.mihomo_api_desc'),
        cta: { label: $t('dash.problems.mihomo_api_cta'), run: openConstructor }
      });
    }
    if (stepProblem) {
      list.push({
        ...stepProblem,
        step: true,
        dataStep: nextStep ?? undefined,
        testid: stepProblem.testid ?? 'problem-next-step'
      });
    }
    if (mihomoInsecure) {
      list.push({
        key: 'insecure',
        tone: 'warning',
        title: $t('dash.problems.mihomo_insecure_title'),
        desc: $t('dash.problems.mihomo_insecure_desc'),
        cta: { label: $t('mihomo.migrate_btn'), run: onMigrateMihomo }
      });
    }
    return list;
  });

  const watchdogShown = $derived(isWatchdogIncident && watchdogStatus !== null);
  const watchdogDegraded = $derived(watchdogStatus?.state === 'degraded');
</script>

{#if watchdogShown || problems.length > 0}
  <section class="dash-alerts" aria-label={$t('dash.problems_panel')}>
    <div class="problems-list">
      {#if watchdogShown && watchdogStatus}
        <div class="problem-item" class:alert-error={watchdogDegraded}>
          <div class="problem-content">
            <span class="problem-icon"><Icon name="warning" size={16} /></span>
            <div>
              <strong class="problem-title">
                {$t(
                  watchdogDegraded
                    ? 'watchdog.banner_degraded_title'
                    : 'watchdog.banner_disarmed_title'
                )}
              </strong>
              <div class="problem-desc">
                {$t(
                  watchdogDegraded
                    ? 'watchdog.banner_degraded_desc'
                    : 'watchdog.banner_disarmed_desc'
                )}
                {#if statusStale}
                  <span class="watchdog-stale-desc">({$t('watchdog.stale_note')})</span>
                {/if}
              </div>
              {#if watchdogStatus.last_disarm_error}
                <div class="watchdog-error-detail">{watchdogStatus.last_disarm_error}</div>
              {/if}
            </div>
          </div>
          <Button variant="secondary" loading={isResettingWatchdog} onclick={onResetWatchdog}>
            {$t(watchdogDegraded ? 'watchdog.cta_retry' : 'watchdog.cta_reset')}
          </Button>
        </div>
      {/if}

      {#each problems as p (p.key)}
        <div
          class="problem-item"
          class:alert-error={p.tone === 'error'}
          class:alert-step={p.tone === 'step'}
          class:in-pulse={p.inPulse}
          data-testid={p.testid}
          data-step={p.dataStep}
        >
          <div class="problem-content">
            <span class="problem-icon"
              ><Icon name={p.tone === 'step' ? 'arrow-right' : 'warning'} size={16} /></span
            >
            <div>
              {#if p.step}
                <span class="problem-step-label">{$t('dash.next_step_label')}</span>
              {/if}
              <strong class="problem-title">{p.title}</strong>
              <div class="problem-desc">{p.desc}</div>
            </div>
          </div>
          {#if p.cta}
            <Button variant={p.cta.primary ? 'primary' : 'secondary'} onclick={p.cta.run}>
              {p.cta.label}
            </Button>
          {/if}
        </div>
      {/each}
    </div>
  </section>
{/if}

<style>
  .dash-alerts {
    margin-bottom: var(--spacing-6);
  }

  /* Строка замечания: полоса и значок тона, текст на нейтральных токенах, действие справа
     (на узкой ширине уходит под текст). Тон задаёт модификатор .alert-error. */
  .problems-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3, 12px);
  }

  .problem-item {
    --tone: var(--warning);
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--spacing-3, 12px) var(--spacing-4, 16px);
    padding: var(--spacing-4, 16px) var(--spacing-5, 20px);
    border: 1px solid color-mix(in srgb, var(--tone) 28%, var(--border));
    border-left: 3px solid var(--tone);
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--tone) 7%, var(--bg-card));
    color: var(--fg-primary);
  }

  .problem-item.alert-error {
    --tone: var(--danger);
  }

  /* Следующий шаг настройки — не авария: акцентный тон и стрелка вместо красного треугольника */
  .problem-item.alert-step {
    --tone: var(--accent);
  }

  .problem-content {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-3, 12px);
    flex: 1 1 320px;
    min-width: 0;
  }

  .problem-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--tone) 16%, transparent);
    color: var(--tone);
  }

  .problem-content > div {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-0-5);
    min-width: 0;
    max-width: 68ch;
  }

  .problem-step-label {
    display: block;
    font-size: var(--font-size-xs, 12px);
    font-weight: 600;
    line-height: 1.3;
    color: var(--tone);
  }

  .problem-title {
    font-size: var(--font-size-lg, 16px);
    font-weight: 600;
    line-height: 1.35;
    color: var(--fg-primary);
  }

  .problem-desc {
    font-size: var(--font-size-base, 14px);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .problem-item > :global(.btn) {
    flex-shrink: 0;
  }

  .watchdog-stale-desc {
    display: inline-block;
    margin-left: var(--spacing-1, 4px);
    color: var(--fg-dim);
    font-size: var(--font-size-xs, 12px);
  }

  .watchdog-error-detail {
    font-family: var(--font-family-mono, monospace);
    font-size: var(--font-size-xs, 12px);
    color: var(--fg-secondary);
    margin-top: var(--spacing-2, 8px);
    word-break: break-word;
    white-space: pre-wrap;
  }

  @media (max-width: 768px) {
    .problem-item {
      padding: var(--spacing-3, 12px) var(--spacing-4, 16px);
    }

    /* Телефон: замечание — компактная строка под «пультом», а не крупная
       карточка на пол-экрана; описание сокращено до трёх строк */
    .problem-icon {
      width: 28px;
      height: 28px;
    }

    .problem-title {
      font-size: var(--font-size-base, 14px);
    }

    .problem-desc {
      font-size: var(--font-size-sm);
      display: -webkit-box;
      -webkit-box-orient: vertical;
      -webkit-line-clamp: 3;
      line-clamp: 3;
      overflow: hidden;
    }

    .problems-list {
      gap: var(--spacing-2, 8px);
    }

    .problem-item.in-pulse {
      display: none;
    }

    .dash-alerts:not(:has(.problem-item:not(.in-pulse))) {
      display: none;
    }

    .dash-alerts {
      scroll-margin-top: calc(var(--mhdr-h) + env(safe-area-inset-top) + 12px);
    }

    .problem-item > :global(.btn) {
      width: 100%;
    }
  }
</style>
