<script lang="ts">
  import { onDestroy } from 'svelte';
  import { t } from '../../../i18n';
  import {
    FORM_KEYS,
    coresErrorText,
    parseRaw,
    type CoreKernel,
    type KeyIssue,
    type RawResult
  } from '../../../lib/configCores';

  // Сырой блок «Дополнительно» секции ядра (D-07): остаток значения секции без полей формы
  // и без закреплённых — JSON у Xray, YAML у Mihomo. Разбор делает сервер (parseRaw) через
  // 400 мс после ввода; в черновик текст уходит по уходу фокуса и только при верном
  // синтаксисе без отклонённых ключей. У «Прочих ключей» блок раскрыт всегда, без заголовка.
  // Чужая секция (readonly) показывает действующие значения и ничего не шлёт.
  let {
    kernel,
    id,
    text = $bindable(''),
    readonly = false,
    open = false,
    issues = [],
    oncommit
  }: {
    kernel: CoreKernel;
    id: string;
    text?: string;
    readonly?: boolean;
    open?: boolean;
    /** Проблемы ключей, которые вернула запись (commitSection): показываются вместе с разбором. */
    issues?: KeyIssue[];
    /** Текст верен: вызывается по уходу фокуса, если он изменился. */
    oncommit?: (text: string) => void;
  } = $props();

  const uid = $props.id();
  const DEBOUNCE_MS = 400;

  // Секция «Прочие ключи» раскрыта всегда
  const always = $derived(id === 'other');
  const label = $derived($t(kernel === 'xray' ? 'cfg.sec.advanced_json' : 'cfg.sec.advanced_yaml'));

  // Текст в поле: производный от текста секции, ввод его переопределяет, новый текст секции
  // сбрасывает к нему. Перечитывание вида с тем же текстом набранное не трогает: производная
  // `base` остаётся прежней строкой, пока текст секции не изменился, и `draft` на неё
  // не реагирует. Без этого шага поздний ответ перечитывания, пришедший посреди ввода,
  // подменял набранное прежним текстом секции.
  const base = $derived(text);
  let draft = $derived(base);

  // Последний ответ разбора и текст, к которому он относится
  let result = $state<RawResult | null>(null);
  let parsedText = $state<string | null>(null);
  let failure = $state('');
  let timer: ReturnType<typeof setTimeout> | undefined;
  // Счётчик запросов: ответ устаревшего запроса отбрасывается
  let seq = 0;

  async function check(value: string): Promise<boolean> {
    const n = ++seq;
    let res: RawResult | null = null;
    let fail = '';
    try {
      res = await parseRaw(kernel, id, value);
    } catch (err) {
      const e = err as { code?: string; params?: Record<string, string> } | null;
      fail = coresErrorText($t, e?.code, e?.params);
    }
    if (n !== seq) return false;
    result = res;
    failure = fail;
    parsedText = value;
    return true;
  }

  function onInput(): void {
    if (readonly) return;
    clearTimeout(timer);
    seq++;
    parsedText = null;
    failure = '';
    const value = draft;
    timer = setTimeout(() => void check(value), DEBOUNCE_MS);
  }

  async function onChange(): Promise<void> {
    if (readonly) return;
    clearTimeout(timer);
    const value = draft;
    if (parsedText !== value) {
      if (!(await check(value))) return;
    }
    const clean =
      failure === '' && result !== null && !result.error && result.rejected.length === 0;
    if (clean && value !== text) oncommit?.(value);
  }

  onDestroy(() => {
    clearTimeout(timer);
    seq++;
  });

  // Первые сегменты путей форм ядра: ключи, которые правятся в своей секции
  const SECTION_KEYS = $derived.by(() => {
    const list: string[] = [];
    for (const [name, paths] of Object.entries(FORM_KEYS)) {
      if (!name.startsWith(`${kernel}.`)) continue;
      list.push(name.slice(kernel.length + 1));
      for (const p of paths) {
        const head = p.split('.')[0].replace(/\[\]$/, '');
        if (head !== '') list.push(head);
      }
    }
    return list;
  });
  const ROUTING_KEYS = [
    'routing',
    'outbounds',
    'observatory',
    'burstObservatory',
    'rules',
    'rule-providers',
    'proxies',
    'proxy-groups',
    'proxy-providers'
  ];

  function hintOf(key: string): string {
    if (key === 'profile') return $t('cfg.sec.raw_hint.profile');
    if (ROUTING_KEYS.includes(key)) return $t('cfg.sec.raw_hint.routing');
    if (SECTION_KEYS.includes(key)) return $t('cfg.sec.raw_hint.section');
    return '';
  }

  function issueLine(issue: KeyIssue): string {
    switch (issue.code) {
      case 'raw_reserved':
        return $t('cfg.sec.raw_reserved', { key: issue.key, hint: hintOf(issue.key) }).trim();
      case 'controller_no_secret':
        return $t('cfg.sec.controller_secret');
      default:
        return coresErrorText($t, issue.code, { key: issue.key });
    }
  }

  // Разбор относится к тексту в поле: пока идёт ввод, прежние ошибки не показываются
  const current = $derived(parsedText === draft);
  const lines = $derived.by(() => {
    const out: string[] = [];
    if (current) {
      if (failure !== '') out.push(failure);
      if (result?.error) {
        out.push(
          $t('cfg.sec.raw_syntax', { line: result.error.line, detail: result.error.detail })
        );
      }
      for (const issue of result?.rejected ?? []) out.push(issueLine(issue));
    }
    for (const issue of issues) out.push(issueLine(issue));
    return out.filter((line, i) => out.indexOf(line) === i);
  });
  const hasError = $derived(lines.length > 0 && !readonly);
</script>

{#snippet editor()}
  <label class="ab-sr" for="{uid}-text">{label}</label>
  <textarea
    id="{uid}-text"
    class="input ab-text"
    class:input-error={hasError}
    rows="8"
    spellcheck="false"
    autocomplete="off"
    autocapitalize="off"
    {readonly}
    aria-readonly={readonly ? 'true' : undefined}
    aria-invalid={hasError ? 'true' : 'false'}
    aria-describedby={hasError ? `${uid}-error` : undefined}
    data-testid="section-advanced-{kernel}-{id}"
    bind:value={draft}
    oninput={onInput}
    onchange={onChange}></textarea>
  {#if hasError}
    <div
      class="error-text ab-error"
      id="{uid}-error"
      role="alert"
      data-testid="section-raw-error-{kernel}-{id}"
    >
      {#each lines as line (line)}
        <span class="ab-line">{line}</span>
      {/each}
    </div>
  {/if}
{/snippet}

{#if always}
  <div class="ab ab-always" data-testid="section-advanced-block-{kernel}-{id}">
    {@render editor()}
  </div>
{:else}
  <details class="ab" {open} data-testid="section-advanced-block-{kernel}-{id}">
    <summary class="ab-summary">{label}</summary>
    <div class="ab-body">
      {@render editor()}
    </div>
  </details>
{/if}

<style>
  .ab {
    min-width: 0;
  }

  .ab-summary {
    display: flex;
    align-items: center;
    min-height: 32px;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .ab-body {
    min-width: 0;
    padding-top: var(--spacing-2);
  }

  .ab-sr {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
  }

  .ab-text {
    display: block;
    width: 100%;
    min-width: 0;
    height: auto;
    min-height: 0;
    padding: var(--spacing-2) var(--spacing-3);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    line-height: 1.5;
    resize: vertical;
    overflow-y: auto;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
    scrollbar-width: thin;
  }

  .ab-error {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ab-line {
    display: block;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .ab-summary {
      min-height: 44px;
    }
  }
</style>
