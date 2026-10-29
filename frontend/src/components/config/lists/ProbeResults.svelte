<script lang="ts">
  import { t } from '../../../i18n';
  import { formatBytes } from '../../../lib/format';
  import Icon from '../../../lib/components/Icon.svelte';
  import type { ProbeCheck, ProbeResult } from '../../../lib/configGeodata';

  // Результат проверки источника: строки «иконка + текст» по кодам сервера. Цвет не единственный
  // носитель смысла: у каждого уровня своя иконка (check, warning, close). Тексты берутся из
  // словаря по коду, значения подставляются как есть, разметки в них нет.
  let {
    result,
    ghProxy = ''
  }: {
    /** Результат проверки; null — проверки ещё не было, список пуст. */
    result: ProbeResult | null;
    /** gh_proxy из настроек XKeen: пустой — подсказка про прокси, если адрес не ответил. */
    ghProxy?: string;
  } = $props();

  const SIZE_PARAMS = ['size', 'need', 'free'];

  function params(c: ProbeCheck): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(c.params ?? {})) {
      out[k] = SIZE_PARAMS.includes(k) && /^\d+$/.test(v) ? formatBytes(Number(v)) : v;
    }
    return out;
  }

  function text(c: ProbeCheck): string {
    const key = 'cfg.geo.chk_' + c.code;
    const s = $t(key, params(c));
    return s === key ? c.code : s;
  }

  function routeText(route: string): string {
    const key = 'cfg.geo.route_' + route;
    const s = $t(key);
    return s === key ? route : s;
  }

  const ICONS: Record<string, string> = { ok: 'check', warn: 'warning', block: 'close' };

  const checks = $derived(result?.checks ?? []);
  // Подсказка про блок «Загрузка»: GitHub недоступен этим путём или адрес не ответил без прокси
  const showGhHint = $derived(
    checks.some(
      (c) => c.code === 'gh_unreachable' || (c.code === 'reach_fail' && ghProxy.trim() === '')
    )
  );
</script>

<div class="pr-results" role="status" data-testid="geo-add-results">
  {#if result !== null}
    <ul class="pr-list">
      {#each checks as c, i (i)}
        <li class="pr-item pr-{c.level}" data-code={c.code} data-level={c.level}>
          <span class="pr-icon icon-first-line"
            ><Icon name={ICONS[c.level] ?? 'check'} size={16} /></span
          >
          <span class="pr-text">{text(c)}</span>
        </li>
      {/each}
    </ul>
    {#if result.route !== ''}
      <p class="pr-note" data-testid="geo-add-route">
        {$t('cfg.geo.check_route', { route: routeText(result.route) })}
      </p>
    {/if}
    {#if showGhHint}
      <p class="pr-note" data-testid="geo-add-gh-hint">{$t('cfg.geo.chk_gh_hint')}</p>
    {/if}
  {/if}
</div>

<style>
  .pr-results {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .pr-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .pr-item {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
  }

  .pr-icon {
    display: inline-flex;
  }

  .pr-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pr-ok .pr-icon {
    color: var(--success);
  }

  .pr-warn .pr-icon {
    color: var(--warning);
  }

  .pr-block .pr-icon {
    color: var(--danger);
  }

  .pr-note {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }
</style>
