<script lang="ts">
  import { tick } from 'svelte';
  import Select from '../../../Select.svelte';
  import { t } from '../../../../i18n';
  import type {
    CoreKernel,
    ForceProxyView,
    KeyIssue,
    SectionView
  } from '../../../../lib/configCores';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import MihomoField from '../MihomoField.svelte';
  import MihomoToggle from '../MihomoToggle.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import { KERNEL_DEFAULTS, commitSection, fieldTestId, getPath, setPath } from '../sectionForm';

  // Базовая форма DNS Mihomo (D-09): «Включить DNS», режим, IPv6 и два списка серверов
  // (nameserver, default-nameserver). Поля DNS через прокси (fake-ip-range,
  // proxy-server-nameserver, nameserver-policy, listen) форма не рисует: они остаются в
  // «Дополнительно», Phase 148 расширит эту же форму. Чужая секция (readonly) показывает
  // действующие значения полями только для чтения.
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

  const ID = 'dns';
  const uid = $props.id();
  const defaults = KERNEL_DEFAULTS['mihomo.dns'] ?? {};
  const MODES = ['fake-ip', 'redir-host', 'normal'];

  const LISTS = [
    { path: 'nameserver', label: 'cfg.sec.f.nameserver' },
    { path: 'default-nameserver', label: 'cfg.sec.f.default_nameserver' }
  ] as const;
  type ListPath = (typeof LISTS)[number]['path'];

  function formObject(): Record<string, unknown> {
    return section.form !== null && typeof section.form === 'object' && !Array.isArray(section.form)
      ? (section.form as Record<string, unknown>)
      : {};
  }

  function stringsOf(obj: unknown, path: string): string[] {
    const v = getPath(obj, path);
    return Array.isArray(v) ? v.map((x) => String(x)) : [];
  }

  // Значения формы: производные от вида секции, правка их переопределяет, новый вид
  // секции (после записи в черновик) сбрасывает к нему
  let local = $derived<Record<string, unknown>>(formObject());
  let lists = $derived<Record<ListPath, string[]>>({
    nameserver: stringsOf(formObject(), 'nameserver'),
    'default-nameserver': stringsOf(formObject(), 'default-nameserver')
  });
  let issues = $state<KeyIssue[]>([]);

  function str(path: string): string {
    const v = getPath(local, path);
    return v === undefined || v === null ? '' : String(v);
  }

  // Режим вне списка (значение, заданное в профиле вручную) остаётся выбираемым
  const modeOptions = $derived(
    str('enhanced-mode') !== '' && !MODES.includes(str('enhanced-mode'))
      ? [...MODES, str('enhanced-mode')]
      : MODES
  );

  function hintOf(path: string): string {
    if (str(path) !== '') return '';
    const value = defaults[path];
    return value === undefined ? '' : $t('cfg.sec.default_hint', { value });
  }

  /** Форма целиком: скаляры и списки без пустых строк; опустевший список снимает ключ. */
  function build(scalars: Record<string, unknown>, all: Record<ListPath, string[]>) {
    let next = scalars;
    for (const { path } of LISTS) {
      const clean = all[path].map((s) => s.trim()).filter((s) => s !== '');
      next = setPath(next, path, clean.length > 0 ? clean : undefined);
    }
    return next;
  }

  async function save(next: Record<string, unknown>, text: string): Promise<void> {
    issues = await commitSection(kernel, ID, next, text);
  }

  function setField(path: string, value: unknown): void {
    local = setPath(local, path, value);
    void save(build(local, lists), section.advanced);
  }

  function setRows(path: ListPath, rows: string[], commit: boolean): void {
    lists = { ...lists, [path]: rows };
    if (commit) void save(build(local, lists), section.advanced);
  }

  async function addRow(path: ListPath): Promise<void> {
    const rows = [...lists[path], ''];
    setRows(path, rows, false);
    await tick();
    document.getElementById(`${uid}-${path}-${rows.length - 1}`)?.focus();
  }

  async function saveAdvanced(text: string): Promise<void> {
    await save(build(local, lists), text);
  }
</script>

<div class="mdf" data-testid="section-form-mihomo-{ID}">
  <div class="mdf-fields">
    <MihomoToggle
      id="{uid}-enable"
      label={$t('cfg.sec.f.dns_enable')}
      checked={getPath(local, 'enable') === true}
      {readonly}
      hint={hintOf('enable')}
      testid={fieldTestId(kernel, ID, 'enable')}
      onchange={(v) => setField('enable', v)}
    />

    <MihomoField id="{uid}-enhanced-mode" label={$t('cfg.sec.f.enhanced_mode')}>
      {#if readonly}
        <input
          id="{uid}-enhanced-mode"
          class="input"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'enhanced-mode')}
          value={str('enhanced-mode')}
        />
      {:else}
        <Select
          id="{uid}-enhanced-mode"
          value={str('enhanced-mode')}
          data-testid={fieldTestId(kernel, ID, 'enhanced-mode')}
          onchange={(e) => setField('enhanced-mode', e.currentTarget.value)}
        >
          <option value="">
            {$t('cfg.sec.default_option', { value: defaults['enhanced-mode'] ?? '' })}
          </option>
          {#each modeOptions as m (m)}
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

    {#each LISTS as list (list.path)}
      {@const rows = lists[list.path]}
      <MihomoField id="{uid}-{list.path}" label={$t(list.label)} group>
        {#if rows.length === 0 && !readonly}
          <span class="mdf-empty" data-testid="section-list-empty-mihomo-{ID}-{list.path}">
            {$t('cfg.sec.list_empty')}
          </span>
        {/if}
        {#each rows as row, i (i)}
          <div class="mdf-row">
            <input
              id="{uid}-{list.path}-{i}"
              class="input mdf-mono"
              type="text"
              spellcheck="false"
              autocomplete="off"
              {readonly}
              aria-readonly={readonly ? 'true' : undefined}
              aria-label="{$t(list.label)} {i + 1}"
              data-testid="{fieldTestId(kernel, ID, list.path)}-{i}"
              value={row}
              onchange={(e) =>
                setRows(
                  list.path,
                  rows.map((v, j) => (j === i ? e.currentTarget.value : v)),
                  true
                )}
            />
            {#if !readonly}
              <button
                type="button"
                class="btn btn-secondary btn-sm mdf-btn"
                data-testid="section-remove-mihomo-{ID}-{list.path}-{i}"
                onclick={() =>
                  setRows(
                    list.path,
                    rows.filter((_, j) => j !== i),
                    true
                  )}
              >
                {$t('cfg.sec.f.remove_server')}
              </button>
            {/if}
          </div>
        {/each}
        {#if !readonly}
          <div>
            <button
              type="button"
              class="btn btn-secondary btn-sm mdf-btn"
              data-testid="section-add-mihomo-{ID}-{list.path}"
              onclick={() => addRow(list.path)}
            >
              {$t('cfg.sec.f.add_server')}
            </button>
          </div>
        {/if}
      </MihomoField>
    {/each}
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
  .mdf {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .mdf-fields {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .mdf-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .mdf-row .input {
    flex: 1 1 12rem;
  }

  .mdf-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .mdf-empty {
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .mdf :global(.input),
    .mdf :global(.xcp-select select),
    .mdf-btn {
      min-height: 44px;
    }
  }
</style>
