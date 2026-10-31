<script lang="ts">
  import Select from '../../../Select.svelte';
  import { t } from '../../../../i18n';
  import type {
    CoreKernel,
    ForceProxyView,
    KeyIssue,
    SectionView
  } from '../../../../lib/configCores';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import MihomoField, { describedBy } from '../MihomoField.svelte';
  import MihomoToggle from '../MihomoToggle.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import {
    KERNEL_DEFAULTS,
    commitSection,
    fieldTestId,
    getPath,
    intField,
    setPath
  } from '../sectionForm';

  // Форма «Основное» Mihomo (D-07, D-10, D-26): порты, доступ из LAN, адрес привязки,
  // режим, уровень лога и IPv6. Закреплённые routing-mark, external-controller-unix и
  // profile.store-selected — текст PinnedValues; остаток секции, в том числе прочие
  // подключи profile, — «Дополнительно» (YAML). Ключа нет — поле пустое с умолчанием ядра
  // в подсказке; в секцию ключ попадает, только если его изменили. Чужая секция (readonly)
  // показывает действующие значения полями только для чтения.
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

  const ID = 'main';
  const uid = $props.id();
  const defaults = KERNEL_DEFAULTS['mihomo.main'] ?? {};

  const PORTS = [
    { path: 'mixed-port', label: 'cfg.sec.f.mixed_port' },
    { path: 'port', label: 'cfg.sec.f.http_port' },
    { path: 'socks-port', label: 'cfg.sec.f.socks_port' },
    { path: 'redir-port', label: 'cfg.sec.f.redir_port' },
    { path: 'tproxy-port', label: 'cfg.sec.f.tproxy_port' }
  ] as const;
  const MODES = ['rule', 'global', 'direct'];
  const LOG_LEVELS = ['silent', 'error', 'warning', 'info', 'debug'];

  // Значения формы: производные от вида секции, правка их переопределяет, новый вид
  // секции (после записи в черновик) сбрасывает к нему
  let local = $derived<Record<string, unknown>>(
    section.form !== null && typeof section.form === 'object' && !Array.isArray(section.form)
      ? (section.form as Record<string, unknown>)
      : {}
  );
  // Неверный ввод порта: текст остаётся в поле, в черновик не уходит
  let bad = $state<Record<string, string>>({});
  let issues = $state<KeyIssue[]>([]);

  function str(path: string): string {
    const v = getPath(local, path);
    return v === undefined || v === null ? '' : String(v);
  }

  function hintOf(path: string): string {
    if (str(path) !== '') return '';
    const value = defaults[path];
    return value === undefined ? '' : $t('cfg.sec.default_hint', { value });
  }

  async function save(next: Record<string, unknown>, text: string): Promise<void> {
    issues = await commitSection(kernel, ID, next, text);
  }

  function setField(path: string, value: unknown): void {
    const next = setPath(local, path, value);
    local = next;
    void save(next, section.advanced);
  }

  function errorOf(path: string): string {
    return bad[path] === undefined ? '' : intField(bad[path], 0, 65535);
  }

  function dropBad(path: string): void {
    bad = Object.fromEntries(Object.entries(bad).filter(([key]) => key !== path));
  }

  function onPort(path: string, raw: string): void {
    if (intField(raw, 0, 65535) !== '') {
      bad = { ...bad, [path]: raw };
      return;
    }
    dropBad(path);
    const text = raw.trim();
    setField(path, text === '' ? undefined : Number(text));
  }

  function options(list: string[], path: string): string[] {
    const current = str(path);
    return current !== '' && !list.includes(current) ? [...list, current] : list;
  }

  function defaultOption(path: string): string {
    return $t('cfg.sec.default_option', { value: defaults[path] ?? '' });
  }

  async function saveAdvanced(text: string): Promise<void> {
    await save(local, text);
  }
</script>

<div class="mmf" data-testid="section-form-mihomo-{ID}">
  <div class="mmf-fields">
    {#each PORTS as p (p.path)}
      {@const id = `${uid}-${p.path}`}
      {@const error = errorOf(p.path)}
      {@const hint = $t('cfg.sec.f.port_off_hint')}
      <MihomoField {id} label={$t(p.label)} {hint} {error}>
        <input
          {id}
          class="input"
          class:input-error={error !== ''}
          type="number"
          inputmode="numeric"
          min="0"
          max="65535"
          step="1"
          {readonly}
          aria-readonly={readonly ? 'true' : undefined}
          aria-invalid={error !== '' ? 'true' : 'false'}
          aria-describedby={describedBy(id, error, hint)}
          data-testid={fieldTestId(kernel, ID, p.path)}
          value={bad[p.path] ?? str(p.path)}
          onchange={(e) => onPort(p.path, e.currentTarget.value)}
        />
      </MihomoField>
    {/each}

    <MihomoToggle
      id="{uid}-allow-lan"
      label={$t('cfg.sec.f.allow_lan')}
      checked={getPath(local, 'allow-lan') === true}
      {readonly}
      hint={hintOf('allow-lan')}
      testid={fieldTestId(kernel, ID, 'allow-lan')}
      onchange={(v) => setField('allow-lan', v)}
    />

    <MihomoField
      id="{uid}-bind-address"
      label={$t('cfg.sec.f.bind_address')}
      hint={hintOf('bind-address')}
    >
      <input
        id="{uid}-bind-address"
        class="input mmf-mono"
        type="text"
        spellcheck="false"
        autocomplete="off"
        {readonly}
        aria-readonly={readonly ? 'true' : undefined}
        aria-describedby={describedBy(`${uid}-bind-address`, '', hintOf('bind-address'))}
        data-testid={fieldTestId(kernel, ID, 'bind-address')}
        value={str('bind-address')}
        onchange={(e) => setField('bind-address', e.currentTarget.value.trim())}
      />
    </MihomoField>

    <MihomoField id="{uid}-mode" label={$t('cfg.sec.f.mode')}>
      {#if readonly}
        <input
          id="{uid}-mode"
          class="input"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'mode')}
          value={str('mode')}
        />
      {:else}
        <Select
          id="{uid}-mode"
          value={str('mode')}
          data-testid={fieldTestId(kernel, ID, 'mode')}
          onchange={(e) => setField('mode', e.currentTarget.value)}
        >
          <option value="">{defaultOption('mode')}</option>
          {#each options(MODES, 'mode') as m (m)}
            <option value={m}>{m}</option>
          {/each}
        </Select>
      {/if}
    </MihomoField>

    <MihomoField id="{uid}-log-level" label={$t('cfg.sec.f.log_level')}>
      {#if readonly}
        <input
          id="{uid}-log-level"
          class="input"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'log-level')}
          value={str('log-level')}
        />
      {:else}
        <Select
          id="{uid}-log-level"
          value={str('log-level')}
          data-testid={fieldTestId(kernel, ID, 'log-level')}
          onchange={(e) => setField('log-level', e.currentTarget.value)}
        >
          <option value="">{defaultOption('log-level')}</option>
          {#each options(LOG_LEVELS, 'log-level') as m (m)}
            <option value={m}>{m}</option>
          {/each}
        </Select>
      {/if}
    </MihomoField>

    <MihomoToggle
      id="{uid}-ipv6"
      label={$t('cfg.sec.f.ipv6')}
      checked={getPath(local, 'ipv6') === true}
      {readonly}
      hint={hintOf('ipv6')}
      testid={fieldTestId(kernel, ID, 'ipv6')}
      onchange={(v) => setField('ipv6', v)}
    />
  </div>

  <PinnedValues {kernel} id={ID} items={section.pinned} />
  <AdvancedBlock
    {kernel}
    id={ID}
    text={section.advanced}
    {readonly}
    {issues}
    oncommit={saveAdvanced}
  />
</div>

<style>
  .mmf {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .mmf-fields {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .mmf-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .mmf :global(.input),
    .mmf :global(.xcp-select select) {
      min-height: 44px;
    }
  }
</style>
