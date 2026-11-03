<script lang="ts">
  import { t } from '../../i18n';
  import type { RuleProviderProbe, RuleProviderURLCheck } from '../../lib/api';
  import Icon from '../../lib/components/Icon.svelte';

  interface Props {
    /** Имя провайдера: суффикс data-testid. */
    name: string;
    /** Результат диагностики ссылки (сервер ответил). */
    result?: RuleProviderURLCheck;
    /** Текст сбоя самого запроса проверки, когда результата нет. */
    failure?: string;
    /** Прокси из профиля; по умолчанию берётся из результата. */
    proxy?: string;
  }

  let { name, result, failure = '', proxy }: Props = $props();

  const viaName = $derived((proxy ?? result?.proxy ?? '').trim());
  const hasProxy = $derived(viaName !== '' && viaName.toUpperCase() !== 'DIRECT');

  const via = $derived<RuleProviderProbe | undefined>(hasProxy ? result?.via_proxy : undefined);
  // Прямая проба; без разбора по пробам (старая форма ответа) берутся общие поля результата.
  const direct = $derived.by<RuleProviderProbe | undefined>(() => {
    if (!result) return undefined;
    if (result.direct) return result.direct;
    if (result.via_proxy) return undefined;
    return { ok: result.ok, status_code: result.status_code, error: result.error };
  });

  const directOnly = $derived(hasProxy && !!result?.direct_only);

  type Verdict = 'ok' | 'node' | 'both' | null;
  const verdict = $derived.by<Verdict>(() => {
    if (!via) return null;
    if (via.ok) return 'ok';
    return direct?.ok ? 'node' : 'both';
  });

  function directText(p: RuleProviderProbe): string {
    return p.ok
      ? $t('rules.url_direct_ok', { code: String(p.status_code ?? '') })
      : $t('rules.url_direct_fail', { reason: p.error ?? '' });
  }

  const PROBE_CODES = ['proxy_not_found', 'core_503', 'core_504', 'core_status', 'core_timeout'];

  function viaReason(p: RuleProviderProbe): string {
    if (p.error_code && PROBE_CODES.includes(p.error_code)) {
      return $t(`rules.probe_${p.error_code}`, { status: String(p.error_status ?? '') });
    }
    return p.error ?? '';
  }

  function viaText(p: RuleProviderProbe): string {
    return p.ok
      ? $t('rules.url_via_proxy_ok', { proxy: viaName, ms: String(p.delay_ms ?? 0) })
      : $t('rules.url_via_proxy_fail', { proxy: viaName, reason: viaReason(p) });
  }
</script>

<div class="url-result" role="status" aria-live="polite" data-testid="provider-url-result-{name}">
  {#if failure}
    <p class="row row-fail" data-testid="provider-url-failed-{name}">
      <Icon name="warning" size={14} />
      <span>{$t('rules.url_check_failed', { error: failure })}</span>
    </p>
  {:else}
    {#if via}
      <p
        class="row"
        class:row-ok={via.ok}
        class:row-fail={!via.ok}
        data-testid="provider-url-proxy-{name}"
      >
        <Icon name={via.ok ? 'check' : 'warning'} size={14} />
        <span>{viaText(via)}</span>
      </p>
      {#if via.ok}
        <p class="caption">{$t('rules.url_via_proxy_nocode')}</p>
      {/if}
    {/if}
    {#if direct}
      <p
        class="row"
        class:row-ok={direct.ok}
        class:row-fail={!direct.ok}
        data-testid="provider-url-direct-{name}"
      >
        <Icon name={direct.ok ? 'check' : 'warning'} size={14} />
        <span>{directText(direct)}</span>
      </p>
    {/if}
    {#if verdict}
      <p class="verdict" data-testid="provider-url-verdict-{name}">
        {#if verdict === 'ok'}
          {$t('rules.url_verdict_ok')}
        {:else if verdict === 'node'}
          {$t('rules.url_verdict_node', { proxy: viaName })}
        {:else}
          {$t('rules.url_verdict_both')}
        {/if}
      </p>
    {/if}
    {#if directOnly}
      <p class="caption" data-testid="provider-url-direct-only-{name}">
        {$t('rules.url_direct_only', { proxy: viaName })}
      </p>
    {/if}
  {/if}
</div>

<style>
  .url-result {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 16px;
    background: var(--bg-elevated);
    border-radius: var(--radius-md);
    min-width: 0;
  }

  .row {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .row :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .row-ok :global(svg) {
    color: var(--success);
  }

  .row-fail :global(svg) {
    color: var(--warning);
  }

  .verdict {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .caption {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }
</style>
