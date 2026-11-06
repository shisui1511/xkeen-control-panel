<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../../../i18n';
  import {
    coresErrorText,
    type CoreKernel,
    type ForceProxyView,
    type KeyIssue,
    type PinnedValue,
    type SectionView
  } from '../../../../lib/configCores';
  import { dnsTakeover, DEFAULT_DNS_GROUP_NAME } from '../../../../lib/configDns';
  import Select from '../../../Select.svelte';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import {
    KERNEL_DEFAULTS,
    commitSection,
    fieldTestId,
    getPath,
    issuesByField,
    setPath
  } from '../sectionForm';

  // Базовая форма DNS Xray (D-09; Phase 148 её расширит): серверы строками, стратегия
  // запросов, «Отключить кэш». Серверы-объекты (domains, port и т. п.) остаются в
  // «Дополнительно». Запись в черновик идёт по уходу фокуса через commitSection; ключа нет
  // в объекте — поле пустое с подсказкой умолчания, очистка поля снимает ключ. Чужая секция
  // (readonly) показывает действующие значения и ничего не пишет. Props — общий контракт форм.
  // При перехвате DNS на Xray (D-19, D-20) блок «Перехват DNS» показывает закреплённые
  // строки с сервера (section.pinned: слушатель, исходящий dns, маршрут, серверы), а под
  // списком серверов — подпись о группе-цели; у чужой секции блока нет.
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
  const DEFAULTS = KERNEL_DEFAULTS['xray.dns'] ?? {};
  const FIELD_KEYS = ['servers', 'queryStrategy', 'disableCache'];
  const STRATEGIES = ['UseIP', 'UseIPv4', 'UseIPv6'];

  interface ServerRow {
    key: number;
    text: string;
  }

  // Ключ строки — её номер в виде секции: после перечитывания поля остаются на месте и
  // не теряют фокус; у добавленной строки ключ следующий за наибольшим
  function rowsOf(list: unknown): ServerRow[] {
    return Array.isArray(list)
      ? list.map((item, key) => ({ key, text: typeof item === 'string' ? item : '' }))
      : [];
  }
  function clone(value: unknown): Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value)
      ? (JSON.parse(JSON.stringify(value)) as Record<string, unknown>)
      : {};
  }

  // Состояние формы выводится из вида секции; ввод его переопределяет, новый вид сбрасывает
  let form = $derived(clone(section.form));
  let servers = $derived(rowsOf(getPath(section.form, 'servers')));
  let advanced = $derived(section.advanced);
  let issues = $state<KeyIssue[]>([]);

  // Серверы-объекты ушли в «Дополнительно»: формы списка у секции нет
  const complex = $derived(
    Array.isArray(getPath(section.value, 'servers')) &&
      getPath(section.form, 'servers') === undefined
  );

  const strategy = $derived(String(getPath(form, 'queryStrategy') ?? ''));
  const cacheOff = $derived(getPath(form, 'disableCache') === true);
  const cacheSet = $derived(getPath(form, 'disableCache') !== undefined);

  const byField = $derived(issuesByField(issues));
  const advIssues = $derived(issues.filter((i) => !FIELD_KEYS.includes(i.key)));

  // Перехват DNS держит Xray: состояние, ядро и группу-цель считает сервер
  const TAKEOVER_STATES = ['on', 'window', 'applying'];
  const takeover = $derived(
    $dnsTakeover !== null &&
      $dnsTakeover.kernel === 'xray' &&
      TAKEOVER_STATES.includes($dnsTakeover.state)
  );
  // Группа не пришла — группа-цель по умолчанию (D-09); default_name — имя пробы резолва, не
  // группа. Группа удалена — подписи нет (карточка в это время пишет «Группа … удалена»)
  const takeoverGroup = $derived.by(() => {
    const g = $dnsTakeover?.group;
    if (!g) return DEFAULT_DNS_GROUP_NAME;
    if (g.missing) return '';
    return g.name || g.id || DEFAULT_DNS_GROUP_NAME;
  });

  // Ключи-коды закреплённых строк показываются подписями; прочие — как пришли
  const PINNED_LABELS: Record<string, string> = {
    'dns.servers.zones': 'cfg.dns.pinned_zones',
    'dns.servers.nodes': 'cfg.dns.pinned_nodes'
  };
  const pinned = $derived<PinnedValue[]>(
    section.pinned.map((p) =>
      PINNED_LABELS[p.key] === undefined ? p : { key: $t(PINNED_LABELS[p.key]), value: p.value }
    )
  );

  function onOff(on: boolean): string {
    return $t(on ? 'cfg.sec.value_on' : 'cfg.sec.value_off');
  }
  function issueText(path: string): string {
    const issue = byField.get(path);
    return issue ? coresErrorText($t, issue.code, { key: issue.key }) : '';
  }

  async function commit(): Promise<void> {
    if (readonly) return;
    let payload: unknown = form;
    if (!complex) {
      const list = servers.map((s) => s.text.trim()).filter((s) => s !== '');
      payload = list.length > 0 ? { ...form, servers: list } : form;
    }
    issues = await commitSection(kernel, ID, payload, advanced);
  }

  function setServer(key: number, text: string): void {
    servers = servers.map((s) => (s.key === key ? { ...s, text } : s));
  }

  async function addServer(): Promise<void> {
    const key = servers.reduce((max, s) => Math.max(max, s.key), -1) + 1;
    servers = [...servers, { key, text: '' }];
    await tick();
    document.getElementById(`${uid}-server-${key}`)?.focus();
  }

  function removeServer(key: number): void {
    servers = servers.filter((s) => s.key !== key);
    void commit();
  }

  function onStrategy(event: Event & { currentTarget: HTMLSelectElement }): void {
    form = setPath(form, 'queryStrategy', event.currentTarget.value);
    void commit();
  }

  function onCache(event: Event & { currentTarget: HTMLInputElement }): void {
    const on = event.currentTarget.checked;
    // «Выключено» при отсутствующем ключе ключа не создаёт
    form = setPath(form, 'disableCache', on ? true : cacheSet ? false : undefined);
    void commit();
  }

  async function saveAdvanced(text: string): Promise<void> {
    advanced = text;
    await commit();
  }
</script>

<div class="xf" data-testid="section-form-xray-dns">
  <div class="xf-row">
    <span class="xf-lbl" id="{uid}-servers-lbl">{$t('cfg.sec.f.servers')}</span>
    <div class="xf-ctl">
      {#if complex}
        <p class="xf-note" data-testid="{fieldTestId(kernel, ID, 'servers')}-complex">
          {$t('cfg.sec.f.complex_servers')}
        </p>
      {:else}
        {#if servers.length === 0}
          <p class="xf-note" data-testid="{fieldTestId(kernel, ID, 'servers')}-empty">
            {$t('cfg.sec.list_empty')}
          </p>
        {/if}
        <ul class="xf-list" aria-labelledby="{uid}-servers-lbl">
          {#each servers as row, i (row.key)}
            <li class="xf-item">
              <input
                id="{uid}-server-{row.key}"
                class={['input xf-mono', { 'input-error': byField.has('servers') }]}
                type="text"
                spellcheck="false"
                autocomplete="off"
                autocapitalize="off"
                aria-label="{$t('cfg.sec.f.servers')} {i + 1}"
                aria-invalid={byField.has('servers') ? 'true' : 'false'}
                aria-describedby={byField.has('servers') ? `${uid}-servers-err` : undefined}
                {readonly}
                aria-readonly={readonly ? 'true' : undefined}
                data-testid="{fieldTestId(kernel, ID, 'servers')}-{i}"
                value={row.text}
                oninput={(e) => setServer(row.key, e.currentTarget.value)}
                onchange={commit}
              />
              {#if !readonly}
                <button
                  type="button"
                  class="btn btn-secondary btn-sm xf-remove"
                  aria-label="{$t('cfg.sec.f.remove_server')} {i + 1}"
                  data-testid="{fieldTestId(kernel, ID, 'servers')}-remove-{i}"
                  onclick={() => removeServer(row.key)}
                >
                  {$t('cfg.sec.f.remove_server')}
                </button>
              {/if}
            </li>
          {/each}
        </ul>
        {#if byField.has('servers')}
          <span class="error-text" id="{uid}-servers-err" role="alert">{issueText('servers')}</span>
        {/if}
        {#if !readonly}
          <div class="xf-actions">
            <button
              type="button"
              class="btn btn-secondary btn-sm xf-add"
              data-testid="{fieldTestId(kernel, ID, 'servers')}-add"
              onclick={addServer}
            >
              {$t('cfg.sec.f.add_server')}
            </button>
          </div>
        {/if}
      {/if}
      {#if takeover && !readonly && takeoverGroup !== ''}
        <p class="xf-hint" data-testid="{fieldTestId(kernel, ID, 'servers')}-via-group">
          {$t('cfg.sec.f.servers_via_group', { group: takeoverGroup })}
        </p>
      {/if}
    </div>
  </div>

  <div class="xf-row">
    <label class="xf-lbl" for="{uid}-strategy">{$t('cfg.sec.f.query_strategy')}</label>
    <div class="xf-ctl">
      {#if readonly}
        <input
          id="{uid}-strategy"
          class="input"
          type="text"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'querystrategy')}
          value={strategy === ''
            ? $t('cfg.sec.default_option', { value: DEFAULTS.queryStrategy ?? '' })
            : strategy}
        />
      {:else}
        <Select
          id="{uid}-strategy"
          class="form-select"
          value={strategy}
          data-testid={fieldTestId(kernel, ID, 'querystrategy')}
          onchange={onStrategy}
        >
          <option value="">
            {$t('cfg.sec.default_option', { value: DEFAULTS.queryStrategy ?? '' })}
          </option>
          {#each STRATEGIES as item (item)}
            <option value={item}>{item}</option>
          {/each}
        </Select>
      {/if}
    </div>
  </div>

  <div class="xf-row">
    <label class="xf-lbl" for="{uid}-cache">{$t('cfg.sec.f.disable_cache')}</label>
    <div class="xf-ctl">
      {#if readonly}
        <input
          id="{uid}-cache"
          class="input"
          type="text"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'disablecache')}
          value={onOff(cacheOff)}
        />
      {:else}
        <span class="xf-switch">
          <label class="toggle-switch">
            <input
              id="{uid}-cache"
              type="checkbox"
              checked={cacheOff}
              aria-describedby={cacheSet ? undefined : `${uid}-cache-hint`}
              data-testid={fieldTestId(kernel, ID, 'disablecache')}
              onchange={onCache}
            />
            <span class="toggle-slider"></span>
          </label>
        </span>
        {#if !cacheSet}
          <span class="xf-hint" id="{uid}-cache-hint">
            {$t('cfg.sec.default_hint', { value: onOff(DEFAULTS.disableCache === 'true') })}
          </span>
        {/if}
      {/if}
    </div>
  </div>

  {#if !readonly && pinned.length > 0}
    <section
      class="xf-pinned"
      aria-labelledby="{uid}-pinned-title"
      data-testid="section-pinned-xray-dns"
    >
      <h4 class="xf-pinned-title" id="{uid}-pinned-title">{$t('cfg.dns.pinned_title')}</h4>
      <PinnedValues {kernel} id="dns-takeover" items={pinned} />
    </section>
  {/if}

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

  .xf-ctl :global(.input) {
    min-width: 0;
  }

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

  .xf-note,
  .xf-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .xf-pinned {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .xf-pinned-title {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .xf-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .xf-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .xf-item .input {
    flex: 1 1 auto;
  }

  .xf-remove {
    flex-shrink: 0;
  }

  .xf-actions {
    display: flex;
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

    .xf-item {
      flex-direction: column;
      align-items: stretch;
    }

    .xf-remove {
      width: 100%;
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .xf-remove,
    .xf-add {
      min-height: 44px;
    }
  }
</style>
