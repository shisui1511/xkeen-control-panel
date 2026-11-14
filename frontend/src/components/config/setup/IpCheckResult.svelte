<script lang="ts" module>
  /** Ход и итог проверки IP: до первой проверки, идёт, через VPN, напрямую, не удалось. */
  export type IpPhase = 'idle' | 'loading' | 'proxied' | 'direct_ok' | 'direct_warn' | 'failed';
</script>

<script lang="ts">
  import { t, currentLang } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Badge from '../../Badge.svelte';
  import Spinner from '../../Spinner.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { fetchClientExitIP, type ClientExitIPInfo } from '../../../lib/clientIp';

  // Итог шага 7 «Проверка IP» (149 D-24, D-05, D-07, D-08): адрес выхода этого устройства
  // определяется из браузера и сравнивается с WAN роутера. Итог — замер в моменте: на
  // сервер панели он не отправляется и в прогресс не входит. Пока применение не завершено
  // и ядро не запущено (ready), во внешние сервисы браузер не обращается. При пресете
  // «выборочно» «напрямую» — пояснение, а не ошибка; при «всё через прокси» и «всё кроме
  // страны» — предупреждение с частыми причинами.

  interface Props {
    /** Шаги 5 и 6 выполнены: применение завершено, ядро запущено. */
    ready: boolean;
    /** Пресет маршрутизации шага 4 (пусто — свои правила или не выбран). */
    presetId: string;
    /** Ход и итог проверки для статуса строки шага. */
    phase?: IpPhase;
  }

  let { ready, presetId, phase = $bindable('idle') }: Props = $props();

  // Пресеты, при которых весь трафик устройства должен идти через прокси
  const PROXY_ALL_PRESETS = ['all_proxy', 'except_country'];

  let info = $state<ClientExitIPInfo | null>(null);

  const dataState = $derived(ready ? phase : 'locked');
  const hasResult = $derived(phase !== 'idle' && phase !== 'loading');

  async function run(): Promise<void> {
    if (!ready || phase === 'loading') return;
    phase = 'loading';
    let res: ClientExitIPInfo;
    try {
      res = await fetchClientExitIP(true, $currentLang);
    } catch {
      info = null;
      phase = 'failed';
      return;
    }
    info = res;
    if (res.status === 'error' || res.isProxied === undefined) phase = 'failed';
    else if (res.isProxied) phase = 'proxied';
    else if (PROXY_ALL_PRESETS.includes(presetId)) phase = 'direct_warn';
    else phase = 'direct_ok';
  }

  /** Проверяет внешний IP заново (без кэша). До выполнения шагов 5 и 6 ничего не делает. */
  export function check(): void {
    void run();
  }

  // Строка «Подпись: значение» с моноширинным значением: перевод с меткой на месте
  // значения делится на текст до и после неё
  const MARK = '\u0000';

  function split(key: string, name: string): [string, string] {
    const text = $t(key, { [name]: MARK });
    const i = text.indexOf(MARK);
    return i < 0 ? [text, ''] : [text.slice(0, i), text.slice(i + MARK.length)];
  }

  const rows = $derived.by(() => {
    if (!info) return [];
    const country = [info.flag, info.countryName || info.countryCode].filter(Boolean).join(' ');
    const out: Array<{ key: string; parts: [string, string]; value: string }> = [];
    if (info.ip)
      out.push({ key: 'ip', parts: split('cfg.setup.ip.your_ip', 'ip'), value: info.ip });
    if (country) {
      out.push({
        key: 'country',
        parts: split('cfg.setup.ip.country', 'country'),
        value: country
      });
    }
    if (info.routerWanIp) {
      out.push({
        key: 'router',
        parts: split('cfg.setup.ip.router_ip', 'ip'),
        value: info.routerWanIp
      });
    }
    return out;
  });
</script>

<div
  class="ip-result"
  data-testid="setup-ip-result"
  data-state={dataState}
  role="status"
  aria-live="polite"
>
  {#if dataState === 'locked'}
    <p class="ip-note">{$t('cfg.setup.ip.waiting')}</p>
  {:else if phase === 'loading'}
    <p class="ip-loading">
      <Spinner size={16} />
      <span>{$t('cfg.setup.ip.loading')}</span>
    </p>
  {:else if phase === 'proxied'}
    <div class="ip-verdict"><Badge tone="success">{$t('cfg.setup.ip.proxied')}</Badge></div>
  {:else if phase === 'direct_ok'}
    <div class="ip-verdict"><Badge tone="neutral">{$t('cfg.setup.ip.direct')}</Badge></div>
    <p class="ip-note">{$t('cfg.setup.ip.direct_ok')}</p>
  {:else if phase === 'direct_warn'}
    <div class="alert alert-warning ip-alert">
      <span class="ip-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
      <div class="ip-alert-body">
        <strong>{$t('cfg.setup.ip.direct_warn_title')}</strong>
        <ul class="ip-reasons">
          <li>{$t('cfg.setup.ip.reason_device')}</li>
          <li>{$t('cfg.setup.ip.reason_starting')}</li>
          <li>{$t('cfg.setup.ip.reason_cache')}</li>
        </ul>
      </div>
    </div>
  {:else if phase === 'failed'}
    <div class="alert alert-warning ip-alert">
      <span class="ip-icon" aria-hidden="true"><Icon name="warning" size={16} /></span>
      <div class="ip-alert-body">
        <strong>{$t('cfg.setup.ip.failed_title')}</strong>
        <span>{$t('cfg.setup.ip.failed_body')}</span>
      </div>
    </div>
  {/if}

  {#if dataState !== 'locked' && (phase === 'proxied' || phase === 'direct_ok' || phase === 'direct_warn') && rows.length > 0}
    <ul class="ip-rows">
      {#each rows as row (row.key)}
        <li>{row.parts[0]}<span class="ip-value">{row.value}</span>{row.parts[1]}</li>
      {/each}
    </ul>
  {/if}

  <div class="ip-actions">
    <Button
      variant="secondary"
      size="sm"
      data-testid="setup-ip-check"
      disabled={!ready}
      loading={ready && phase === 'loading'}
      onclick={check}
    >
      {hasResult ? $t('cfg.setup.ip.again') : $t('cfg.setup.ip.check')}
    </Button>
    {#if ready && phase === 'direct_warn'}
      <a class="btn btn-ghost btn-sm" href="#/logs" data-testid="setup-ip-diag-link">
        {$t('cfg.setup.ip.diag_link')}
      </a>
    {/if}
  </div>
</div>

<style>
  .ip-result {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .ip-note {
    margin: 0;
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ip-loading {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin: 0;
    min-width: 0;
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ip-verdict {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .ip-alert {
    margin-bottom: 0;
  }

  .ip-icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    height: 1lh;
  }

  .ip-alert-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    color: var(--fg-primary);
    font-size: var(--font-size-base);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ip-reasons {
    margin: 0;
    padding-left: var(--spacing-4);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .ip-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
    color: var(--fg-secondary);
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ip-value {
    font-family: var(--font-family-mono);
    color: var(--fg-primary);
  }

  .ip-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  @media (max-width: 480px) {
    .ip-actions :global(.btn) {
      width: 100%;
      min-height: 44px;
    }
  }
</style>
