<script lang="ts">
  import { t } from '../../../../i18n';
  import {
    coresErrorText,
    type CoreKernel,
    type ForceProxyView,
    type KeyIssue,
    type SectionView
  } from '../../../../lib/configCores';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import {
    KERNEL_DEFAULTS,
    commitSection,
    fieldTestId,
    getPath,
    intField,
    issuesByField,
    setPath
  } from '../sectionForm';

  // Форма «Политики» Xray: для уровня 0 Handshake, ConnIdle, UplinkOnly, DownlinkOnly —
  // целые секунды. Счётчики policy.system закреплены (PinnedValues): их панель пишет сама.
  // Остаток секции — «Дополнительно». Значение с ошибкой в черновик не пишется: вместо него
  // уходит прежнее. Ключа нет в объекте — поле пустое с подсказкой умолчания, очистка поля
  // снимает ключ. Чужая секция (readonly) показывает действующие значения и ничего не пишет.
  let {
    kernel,
    section,
    readonly
  }: {
    kernel: CoreKernel;
    section: SectionView;
    readonly: boolean;
    forceProxy?: ForceProxyView | undefined;
    routingTaken?: boolean;
  } = $props();

  const ID = 'policy';
  const uid = $props.id();
  const DEFAULTS = KERNEL_DEFAULTS['xray.policy'] ?? {};
  const MAX_SECONDS = 86400;

  interface Field {
    path: string;
    label: string;
  }
  const FIELDS: Field[] = [
    { path: 'levels.0.handshake', label: 'cfg.sec.f.handshake' },
    { path: 'levels.0.connIdle', label: 'cfg.sec.f.conn_idle' },
    { path: 'levels.0.uplinkOnly', label: 'cfg.sec.f.uplink_only' },
    { path: 'levels.0.downlinkOnly', label: 'cfg.sec.f.downlink_only' }
  ];
  const FIELD_KEYS = FIELDS.map((f) => f.path);

  function textsOf(form: unknown): Record<string, string> {
    const out: Record<string, string> = {};
    for (const f of FIELDS) {
      const value = getPath(form, f.path);
      out[f.path] = value === undefined || value === null ? '' : String(value);
    }
    return out;
  }

  // Тексты полей выводятся из вида секции; ввод их переопределяет, новый вид сбрасывает
  let texts = $derived(textsOf(section.form));
  let advanced = $derived(section.advanced);
  let issues = $state<KeyIssue[]>([]);

  // Ошибка проверки по пути поля; пустая строка — значение верное
  const errors = $derived.by(() => {
    const out: Record<string, string> = {};
    for (const f of FIELDS) out[f.path] = intField(texts[f.path], 0, MAX_SECONDS);
    return out;
  });

  const byField = $derived(issuesByField(issues));
  const advIssues = $derived(issues.filter((i) => !FIELD_KEYS.includes(i.key)));

  function problem(path: string): string {
    if (errors[path] !== '') return errors[path];
    const issue = byField.get(path);
    return issue ? coresErrorText($t, issue.code, { key: issue.key }) : '';
  }

  /** Форма для записи: значение с ошибкой заменяется прежним, пустое снимает ключ. */
  function buildForm(): unknown {
    let out: Record<string, unknown> = {};
    for (const f of FIELDS) {
      let value: unknown;
      if (errors[f.path] !== '') {
        value = getPath(section.form, f.path);
      } else {
        const text = texts[f.path].trim();
        value = text === '' ? undefined : Number(text);
      }
      out = setPath(out, f.path, value);
    }
    return out;
  }

  async function commit(): Promise<void> {
    if (readonly) return;
    issues = await commitSection(kernel, ID, buildForm(), advanced);
  }

  /** Значение с ошибкой в черновик не уходит: поле остаётся в ошибке до исправления. */
  async function onField(path: string): Promise<void> {
    if (errors[path] !== '') return;
    await commit();
  }

  async function saveAdvanced(text: string): Promise<void> {
    advanced = text;
    await commit();
  }
</script>

<div class="xf" data-testid="section-form-xray-policy">
  <fieldset class="xf-group">
    <legend class="xf-legend">{$t('cfg.sec.f.level0')}</legend>
    {#each FIELDS as f (f.path)}
      {@const bad = problem(f.path)}
      {@const dflt = DEFAULTS[f.path]}
      <div class="xf-row">
        <label class="xf-lbl" for="{uid}-{f.path}">{$t(f.label)}</label>
        <div class="xf-ctl">
          <input
            id="{uid}-{f.path}"
            class="input"
            class:input-error={bad !== ''}
            type="number"
            inputmode="numeric"
            step="1"
            autocomplete="off"
            {readonly}
            aria-readonly={readonly ? 'true' : undefined}
            aria-invalid={bad !== '' ? 'true' : 'false'}
            aria-describedby={bad !== ''
              ? `${uid}-${f.path}-err`
              : texts[f.path] === '' && dflt !== undefined
                ? `${uid}-${f.path}-hint`
                : undefined}
            data-testid={fieldTestId(kernel, ID, f.path)}
            value={texts[f.path]}
            oninput={(e) => (texts = { ...texts, [f.path]: e.currentTarget.value })}
            onchange={() => onField(f.path)}
          />
          {#if bad !== ''}
            <span class="error-text" id="{uid}-{f.path}-err" role="alert">{bad}</span>
          {:else if texts[f.path] === '' && dflt !== undefined}
            <span class="xf-hint" id="{uid}-{f.path}-hint">
              {$t('cfg.sec.default_hint', { value: dflt })}
            </span>
          {/if}
        </div>
      </div>
    {/each}
  </fieldset>

  <PinnedValues {kernel} id={ID} items={section.pinned} />

  <AdvancedBlock
    {kernel}
    id={ID}
    bind:text={advanced}
    {readonly}
    issues={advIssues}
    oncommit={saveAdvanced}
  />
</div>

<style>
  .xf {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .xf-group {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }

  .xf-legend {
    padding: 0;
    margin-bottom: var(--spacing-3);
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .xf-row {
    display: grid;
    grid-template-columns: minmax(0, 1.1fr) minmax(0, 2fr);
    gap: var(--spacing-2) var(--spacing-4);
    align-items: start;
    min-width: 0;
  }

  .xf-lbl {
    padding-top: var(--spacing-2);
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .xf-ctl {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .xf-ctl :global(.input) {
    min-width: 0;
  }

  .xf-ctl :global(input[readonly]) {
    color: var(--fg-secondary);
  }

  .xf-hint {
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  @media (max-width: 768px) {
    .xf-row {
      grid-template-columns: minmax(0, 1fr);
    }

    .xf-lbl {
      padding-top: 0;
    }
  }
</style>
