<script module lang="ts">
  import type { DnsCheckResult } from '../../../lib/configDns';

  // Ввод и последний ручной результат живут в модуле: карточка перемонтируется при
  // переключении ядра, а поле и результат сбрасываться не должны.
  const saved: { name: string | null; result: DnsCheckResult | null } = {
    name: null,
    result: null
  };
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    checkDns,
    dnsClockOffset,
    dnsRequestErrorText,
    type DnsCheckGroup,
    type DnsState,
    type DnsView
  } from '../../../lib/configDns';

  // Проверка резолва (D-03, D-29, D-30): запрос идёт на 127.0.0.1:53 роутера, путь ответа
  // (группа, прошивка, локальная зона) определяет сервер. Пустое имя — имя по умолчанию.
  let { view }: { view: DnsView } = $props();

  const uid = $props.id();
  const inputId = `${uid}-name`;
  const hintId = `${uid}-hint`;

  const MAX_ANSWERS = 4;
  // Проверка имеет смысл, только пока порт 53 ведёт ядро или перехват ждёт ядро
  const CHECKABLE: ReadonlySet<DnsState> = new Set(['window', 'on', 'waiting']);

  // Начальное значение — имя по умолчанию сервера; обновления вида ввод не сбрасывают
  let name = $state(saved.name ?? untrack(() => view.default_name));
  let own = $state<DnsCheckResult | null>(saved.result);
  let checking = $state(false);
  // Транспортный сбой относится к состоянию перехвата, при котором он случился: смена
  // состояния его снимает
  let transport = $state<{ state: DnsState; text: string } | null>(null);
  const transportError = $derived(
    transport !== null && transport.state === view.state ? transport.text : ''
  );
  let inputEl: HTMLInputElement | null = null;
  const keepInput: Attachment<HTMLInputElement> = (el) => {
    inputEl = el;
    return () => {
      inputEl = null;
    };
  };

  const available = $derived(CHECKABLE.has(view.state));
  const disabled = $derived(!available || checking);

  // Показывается более свежий из двух: свой ручной результат или последняя проверка сервера
  const shown = $derived.by<DnsCheckResult | null>(() => {
    const last = view.last_check ?? null;
    if (!own) return last;
    if (!last) return own;
    return last.at_ms > own.at_ms ? last : own;
  });
  const grouped = $derived(shown !== null && (shown.source === 'auto' || shown.groups.length > 1));

  /** Фокус в поле имени (кнопка «Проверить резолв» в блоке сбоя). */
  export function focus(): void {
    inputEl?.focus();
  }

  function onInput(e: Event & { currentTarget: HTMLInputElement }): void {
    name = e.currentTarget.value;
    saved.name = name;
  }

  async function run(): Promise<void> {
    if (disabled) return;
    checking = true;
    try {
      const res = await checkDns(name.trim());
      own = res;
      saved.result = res;
      transport = null;
    } catch (err) {
      transport = {
        state: view.state,
        text: $t('cfg.dns.check_transport', { error: dnsRequestErrorText(err) })
      };
    } finally {
      checking = false;
    }
  }

  function clock(serverMs: number): string {
    const d = new Date(serverMs - $dnsClockOffset);
    return [d.getHours(), d.getMinutes(), d.getSeconds()]
      .map((n) => String(n).padStart(2, '0'))
      .join(':');
  }

  function groupTitle(g: DnsCheckGroup): string {
    switch (g.label) {
      case 'external':
        return $t('cfg.dns.check_external');
      case 'local':
        return $t('cfg.dns.check_local');
      default:
        return g.name;
    }
  }

  function pathText(g: DnsCheckGroup): string {
    switch (g.path) {
      case 'group':
        return g.group
          ? $t('cfg.dns.path_group', { group: g.group })
          : $t('cfg.dns.path_group_unnamed');
      case 'firmware':
        return $t('cfg.dns.path_firmware');
      case 'zone':
        return g.zone ? $t('cfg.dns.path_zone', { zone: g.zone }) : $t('cfg.dns.path_zone_unnamed');
      default:
        return '';
    }
  }

  function outcomeText(g: DnsCheckGroup): string {
    return g.ok
      ? $t('cfg.dns.check_ok')
      : $t('cfg.dns.check_fail', { reason: g.reason || $t('cfg.dns.no_reason') });
  }
</script>

<div class="dns-check-block" data-testid="dns-check">
  <label class="form-label" for={inputId}>{$t('cfg.dns.check_name')}</label>
  <div class="dns-check-row">
    <input
      id={inputId}
      {@attach keepInput}
      class="input dns-check-input"
      type="text"
      value={name}
      placeholder={view.default_name}
      spellcheck="false"
      autocomplete="off"
      autocapitalize="off"
      data-testid="dns-check-name"
      oninput={onInput}
      onkeydown={(e) => {
        if (e.key === 'Enter') void run();
      }}
    />
    <Button
      variant="secondary"
      {disabled}
      loading={checking}
      ariaDescribedby={available ? undefined : hintId}
      data-testid="dns-check-btn"
      onclick={() => void run()}
    >
      {$t('cfg.dns.check_btn')}
    </Button>
  </div>
  {#if !available}
    <p class="dns-check-caption" id={hintId} data-testid="dns-check-off-hint">
      {$t('cfg.dns.check_off_hint')}
    </p>
  {/if}
  <!-- Зона «Проверяем…» всегда в дереве доступности: без текста она визуально скрыта
       (sr-only), а не display: none — иначе скринридер пропускает появление текста -->
  <p
    class={['dns-check-caption', { 'sr-only': !checking }]}
    aria-live="polite"
    data-testid="dns-check-running"
  >
    {checking ? $t('cfg.dns.check_running') : ''}
  </p>

  <!-- Зона сбоя стоит постоянно (пустая — sr-only), меняется только содержимое -->
  <p
    class={['dns-check-transport', { 'sr-only': transportError === '' }]}
    role="status"
    data-testid="dns-check-transport"
  >
    {#if transportError}
      <Icon name="warning" size={14} class="icon-first-line" />
      <span>{transportError}</span>
    {/if}
  </p>

  {#if shown}
    <div
      class="dns-check-result"
      role="status"
      aria-live="polite"
      data-testid="dns-check-result"
      data-ok={shown.ok}
      data-source={shown.source}
    >
      {#each shown.groups as g, i (i)}
        {@const answers = g.answers.slice(0, MAX_ANSWERS)}
        {@const more = Math.max(0, g.answers.length - MAX_ANSWERS)}
        {@const path = pathText(g)}
        <div class="dns-check-group" data-testid="dns-check-group-{g.label}" data-ok={g.ok}>
          {#if grouped}
            <p class="dns-check-group-title">{groupTitle(g)}</p>
          {/if}
          <p class={['dns-check-outcome', { 'dns-check-outcome-fail': !g.ok }]}>
            <Icon name={g.ok ? 'check' : 'warning'} size={14} class="icon-first-line" />
            <span data-testid="dns-check-outcome">{outcomeText(g)}</span>
          </p>
          <dl class="dns-check-lines">
            <div class="dns-check-line">
              <dt>{$t('cfg.dns.res_name')}</dt>
              <dd class="dns-check-mono">{g.name}</dd>
            </div>
            {#if answers.length > 0}
              <div class="dns-check-line">
                <dt>{$t('cfg.dns.res_answer')}</dt>
                <dd>
                  <span class="dns-check-mono">{answers.join(', ')}</span>
                  {#if more > 0}
                    <span class="dns-check-more">{$t('cfg.dns.and_more', { n: more })}</span>
                  {/if}
                </dd>
              </div>
            {/if}
            {#if path}
              <div class="dns-check-line">
                <dt>{$t('cfg.dns.res_path')}</dt>
                <dd class="dns-check-mono" data-testid="dns-check-path" data-path={g.path}>
                  {path}
                </dd>
              </div>
            {/if}
            <div class="dns-check-line">
              <dt>{$t('cfg.dns.res_latency')}</dt>
              <dd>{g.latency_ms} {$t('app.ms')}</dd>
            </div>
          </dl>
        </div>
      {/each}
      <p class="dns-check-caption" data-testid="dns-check-when">
        {$t(shown.source === 'auto' ? 'cfg.dns.checked_auto' : 'cfg.dns.checked_manual', {
          time: clock(shown.at_ms)
        })}
      </p>
    </div>
  {/if}
</div>

<style>
  .dns-check-block {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-check-block .form-label {
    margin: 0;
  }

  .dns-check-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dns-check-input {
    flex: 1 1 auto;
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
  }

  .dns-check-row :global(.btn) {
    flex-shrink: 0;
  }

  .dns-check-caption {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .dns-check-transport,
  .dns-check-outcome {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-1);
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .dns-check-outcome {
    font-size: var(--font-size-sm);
    font-weight: 600;
    line-height: 1.4;
  }

  /* Транспортный сбой — подпись-сообщение: Caption 12/400 в --warning */
  .dns-check-transport {
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    color: var(--warning);
  }

  .dns-check-outcome {
    color: var(--success);
  }

  .dns-check-outcome-fail {
    color: var(--warning);
  }

  .dns-check-result {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-4);
    background: var(--bg-elevated);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-md);
    min-width: 0;
  }

  .dns-check-group {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .dns-check-group-title {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg-primary);
  }

  .dns-check-lines {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    margin: 0;
    min-width: 0;
  }

  .dns-check-line {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--spacing-2);
    min-width: 0;
  }

  .dns-check-line dt {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg-secondary);
  }

  .dns-check-line dd {
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .dns-check-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .dns-check-more {
    margin-left: var(--spacing-1);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dns-check-input {
      height: 44px;
      min-height: 44px;
    }

    .dns-check-row :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .dns-check-row {
      flex-direction: column;
      align-items: stretch;
    }

    .dns-check-row :global(.btn) {
      width: 100%;
    }
  }
</style>
