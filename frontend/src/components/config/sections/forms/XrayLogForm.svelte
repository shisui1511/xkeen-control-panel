<script lang="ts">
  import Toggle from '../../../Toggle.svelte';
  import { t } from '../../../../i18n';
  import {
    coresErrorText,
    type CoreKernel,
    type ForceProxyView,
    type KeyIssue,
    type SectionView
  } from '../../../../lib/configCores';
  import Select from '../../../Select.svelte';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import {
    KERNEL_DEFAULTS,
    commitSection,
    fieldTestId,
    getPath,
    issuesByField,
    setPath
  } from '../sectionForm';

  // Форма «Лог» Xray (D-07): уровень, пути access- и error-логов, «Логировать DNS-запросы».
  // Остаток секции (maskAddress и другие ключи) живёт в «Дополнительно» и при записи формы
  // не теряется. Запись идёт по уходу фокуса через commitSection; ключа нет в объекте — поле
  // пустое с подсказкой умолчания, очистка поля снимает ключ. Чужая секция (readonly)
  // показывает действующие значения и ничего не пишет.
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

  const ID = 'log';
  const uid = $props.id();
  const DEFAULTS = KERNEL_DEFAULTS['xray.log'] ?? {};
  const FIELD_KEYS = ['loglevel', 'access', 'error', 'dnsLog'];
  const LOG_ROOT = '/opt/var/log/';
  const LEVELS = ['none', 'error', 'warning', 'info', 'debug'];

  function clone(value: unknown): Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value)
      ? (JSON.parse(JSON.stringify(value)) as Record<string, unknown>)
      : {};
  }

  // Состояние формы выводится из вида секции; ввод его переопределяет, новый вид сбрасывает
  let form = $derived(clone(section.form));
  let advanced = $derived(section.advanced);
  let issues = $state<KeyIssue[]>([]);

  const level = $derived(String(getPath(form, 'loglevel') ?? ''));
  const access = $derived(String(getPath(form, 'access') ?? ''));
  const errorPath = $derived(String(getPath(form, 'error') ?? ''));
  // Путь вне каталога журналов: «Логи» файл не прочитают; сохранение не блокируется (D-26)
  const accessDenied = $derived(
    access.trim() !== '' &&
      access.trim().toLowerCase() !== 'none' &&
      !access.trim().startsWith(LOG_ROOT)
  );
  const dnsLogOn = $derived(getPath(form, 'dnsLog') === true);
  const dnsLogSet = $derived(getPath(form, 'dnsLog') !== undefined);

  const byField = $derived(issuesByField(issues));
  const advIssues = $derived(issues.filter((i) => !FIELD_KEYS.includes(i.key)));

  function onOff(on: boolean): string {
    return $t(on ? 'cfg.sec.value_on' : 'cfg.sec.value_off');
  }
  function issueText(path: string): string {
    const issue = byField.get(path);
    return issue ? coresErrorText($t, issue.code, { key: issue.key }) : '';
  }

  async function commit(): Promise<void> {
    if (readonly) return;
    issues = await commitSection(kernel, ID, form, advanced);
  }

  function onLevel(event: Event & { currentTarget: HTMLSelectElement }): void {
    form = setPath(form, 'loglevel', event.currentTarget.value);
    void commit();
  }

  function onText(path: string, value: string): void {
    form = setPath(form, path, value.trim() === '' ? '' : value);
  }

  function onDnsLog(event: Event & { currentTarget: HTMLInputElement }): void {
    const on = event.currentTarget.checked;
    // «Выключено» при отсутствующем ключе ключа не создаёт
    form = setPath(form, 'dnsLog', on ? true : dnsLogSet ? false : undefined);
    void commit();
  }

  async function saveAdvanced(text: string): Promise<void> {
    advanced = text;
    await commit();
  }
</script>

{#snippet pathField(path: string, label: string, value: string, hint: boolean)}
  {@const testId = fieldTestId(kernel, ID, path)}
  {@const denied = hint && accessDenied}
  {@const describedBy = byField.has(path)
    ? `${uid}-${path}-err`
    : [hint ? `${uid}-${path}-hint` : '', denied ? `${uid}-${path}-denied` : '']
        .filter((id) => id !== '')
        .join(' ') || undefined}
  <div class="xf-row">
    <label class="xf-lbl" for="{uid}-{path}">{label}</label>
    <div class="xf-ctl">
      <input
        id="{uid}-{path}"
        class="input xf-mono"
        class:input-error={byField.has(path)}
        type="text"
        spellcheck="false"
        autocomplete="off"
        autocapitalize="off"
        {readonly}
        aria-readonly={readonly ? 'true' : undefined}
        aria-invalid={byField.has(path) ? 'true' : 'false'}
        aria-describedby={describedBy}
        data-testid={testId}
        {value}
        oninput={(e) => onText(path, e.currentTarget.value)}
        onchange={commit}
      />
      {#if byField.has(path)}
        <span class="error-text" id="{uid}-{path}-err" role="alert">{issueText(path)}</span>
      {:else}
        {#if hint}
          <span class="xf-hint" id="{uid}-{path}-hint">{$t('cfg.sec.log_access_hint')}</span>
        {/if}
        {#if denied}
          <span class="warning-text" id="{uid}-{path}-denied" data-testid="{testId}-denied">
            {$t('cfg.sec.log_access_denied')}
          </span>
        {/if}
      {/if}
    </div>
  </div>
{/snippet}

<div class="xf" data-testid="section-form-xray-log">
  <div class="xf-row">
    <label class="xf-lbl" for="{uid}-loglevel">{$t('cfg.sec.f.loglevel')}</label>
    <div class="xf-ctl">
      {#if readonly}
        <input
          id="{uid}-loglevel"
          class="input"
          type="text"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'loglevel')}
          value={level === ''
            ? $t('cfg.sec.default_option', { value: DEFAULTS.loglevel ?? '' })
            : level}
        />
      {:else}
        <Select
          id="{uid}-loglevel"
          class="form-select"
          value={level}
          data-testid={fieldTestId(kernel, ID, 'loglevel')}
          onchange={onLevel}
        >
          <option value="">
            {$t('cfg.sec.default_option', { value: DEFAULTS.loglevel ?? '' })}
          </option>
          {#each LEVELS as item (item)}
            <option value={item}>{item}</option>
          {/each}
        </Select>
      {/if}
    </div>
  </div>

  {@render pathField('access', $t('cfg.sec.f.access'), access, true)}
  {@render pathField('error', $t('cfg.sec.f.error'), errorPath, false)}

  <div class="xf-row">
    <label class="xf-lbl" for="{uid}-dnslog">{$t('cfg.sec.f.dns_log')}</label>
    <div class="xf-ctl">
      {#if readonly}
        <input
          id="{uid}-dnslog"
          class="input"
          type="text"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'dnsLog')}
          value={onOff(dnsLogOn)}
        />
      {:else}
        <span class="xf-switch">
          <Toggle
            id="{uid}-dnslog"
            checked={dnsLogOn}
            data-testid={fieldTestId(kernel, ID, 'dnsLog')}
            onchange={onDnsLog}
          />
        </span>
      {/if}
    </div>
  </div>

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

  .xf-ctl :global(.input),
  .xf-ctl :global(.xcp-select) {
    min-width: 0;
  }

  .xf-ctl :global(input[readonly]) {
    color: var(--fg-secondary);
  }

  .xf-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .xf-hint {
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .xf-switch {
    display: inline-flex;
    align-items: center;
    min-height: var(--input-h);
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
