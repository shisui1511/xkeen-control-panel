<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { t } from '../../../../i18n';
  import { loadRouting, routingView } from '../../../../lib/configRouting';
  import {
    coresErrorText,
    setForceProxyGroup,
    type CoreKernel,
    type ForceProxyView,
    type KeyIssue,
    type SectionView
  } from '../../../../lib/configCores';
  import Select from '../../../Select.svelte';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import { guarded } from '../coresOwner';
  import { isForceProxyDefaultGroup } from '../forceProxyDefaultGroup';
  import { commitSection, fieldTestId } from '../sectionForm';

  // Форма «Входящие» Xray (SEC-02, D-10, D-12, D-16): строки Тег, Протокол, Адрес прослушивания,
  // Порт. Входящие, которые читает XKeen (прозрачные redirect/tproxy и force-proxy*), закреплены:
  // тег и протокол — текст с бейджем, правится только порт, а его смена объявляется заранее.
  // Остальные поля входящего живут в «Дополнительно» (объект «тег → поля») и при записи формы
  // не теряются. Строка с ошибкой в черновик не пишется: вместо неё уходит прежняя, у новой
  // строки — ничего. Чужая секция (readonly) показывает действующие значения и ничего не пишет.
  let {
    kernel,
    section,
    readonly,
    forceProxy
  }: {
    kernel: CoreKernel;
    section: SectionView;
    readonly: boolean;
    forceProxy?: ForceProxyView | undefined;
    routingTaken?: boolean;
  } = $props();

  const ID = 'inbounds';
  const uid = $props.id();
  const NEW_PROTOCOLS = ['socks', 'http', 'mixed'];
  // Хвосты ключей закреплённых значений «<тег>.<путь>» (XrayPinned)
  const PINNED_SUFFIXES = [
    '.protocol',
    '.settings.followRedirect',
    '.settings.network',
    '.streamSettings.sockopt.tproxy'
  ];

  type Obj = Record<string, unknown>;

  interface Row {
    key: number;
    /** Строка формы из вида секции; null — добавленная в этой сессии. */
    orig: Obj | null;
    tag: string;
    protocol: string;
    listen: string;
    port: string;
  }

  function str(value: unknown): string {
    return value === undefined || value === null ? '' : String(value);
  }

  // Ключ строки — её номер в виде секции: после перечитывания поля остаются на месте и не
  // теряют фокус; у добавленной строки ключ следующий за наибольшим
  function rowsOf(form: unknown): Row[] {
    if (!Array.isArray(form)) return [];
    return form.map((raw, key) => {
      const o = (raw !== null && typeof raw === 'object' ? raw : {}) as Obj;
      return {
        key,
        orig: o,
        tag: str(o.tag),
        protocol: str(o.protocol),
        listen: str(o.listen),
        port: str(o.port)
      };
    });
  }

  // Строки выводятся из вида секции; ввод их переопределяет, новый вид сбрасывает
  let rows = $derived(rowsOf(section.form));
  let advanced = $derived(section.advanced);
  let issues = $state<KeyIssue[]>([]);

  const pinnedTags = $derived.by(() => {
    const tags: string[] = [];
    for (const p of section.pinned) {
      const suffix = PINNED_SUFFIXES.find((s) => p.key.endsWith(s));
      if (suffix) tags.push(p.key.slice(0, -suffix.length));
    }
    return tags;
  });

  function pinned(row: Row): boolean {
    return row.orig !== null && pinnedTags.includes(str(row.orig.tag));
  }

  // Порты секции до правок: с ними сравнивается порт закреплённой строки. Пока в секции нет
  // несохранённых правок, база обновляется; с правками остаётся записанное значение.
  let baseline = $state.raw<Record<string, string> | null>(null);
  $effect(() => {
    const ports: Record<string, string> = {};
    for (const r of rowsOf(section.form)) ports[r.tag] = r.port;
    const dirty = section.dirty;
    untrack(() => {
      if (baseline === null || !dirty) baseline = ports;
    });
  });

  interface RowError {
    tag: string;
    port: string;
  }

  const errors = $derived.by<RowError[]>(() =>
    rows.map((r, i) => {
      if (readonly) return { tag: '', port: '' };
      const before = rows.slice(0, i);
      const tag = r.tag.trim();
      let tagErr = '';
      if (pinned(r)) tagErr = '';
      else if (tag === '') tagErr = $t('cfg.sec.err_tag_required');
      else if (before.some((o) => o.tag.trim() === tag)) {
        tagErr = $t('cfg.sec.err_duplicate', { what: $t('cfg.sec.f.tag'), value: tag });
      }

      const port = r.port.trim();
      const unchanged = r.orig !== null && port === str(r.orig.port);
      let portErr = '';
      if (!unchanged && !(/^\d{1,5}$/.test(port) && Number(port) >= 1 && Number(port) <= 65535)) {
        portErr = $t('cfg.sec.err_port');
      } else if (
        before.some(
          (o) => listenOf(o) === listenOf(r) && o.port.trim() === port && sharesNetwork(o, r)
        )
      ) {
        portErr = $t('cfg.sec.err_duplicate', {
          what: $t('cfg.sec.f.listen_port'),
          value: `${listenOf(r)}:${port}`
        });
      }
      return { tag: tagErr, port: portErr };
    })
  );

  // Остаток «Дополнительно» как объект «тег → поля»; не разобрался — пустой
  const advancedObj = $derived.by<Obj>(() => {
    try {
      const obj: unknown = JSON.parse(advanced);
      return obj !== null && typeof obj === 'object' && !Array.isArray(obj) ? (obj as Obj) : {};
    } catch {
      return {};
    }
  });

  // Транспорт входящего (tcp, udp), как его читает сервер: settings.network через запятую, без
  // значения — tcp. У закреплённых строк значение приходит в закреплённых, у остальных — из
  // остатка, у добавленных в этой сессии — tcp.
  function networksOf(row: Row): string[] {
    let raw: unknown;
    if (row.orig !== null) {
      const tag = str(row.orig.tag);
      raw = section.pinned.find((p) => p.key === `${tag}.settings.network`)?.value;
      if (raw === undefined) {
        const entry = advancedObj[tag !== '' ? tag : `#${row.key}`];
        const settings =
          entry !== null && typeof entry === 'object' ? (entry as Obj).settings : null;
        if (settings !== null && typeof settings === 'object') raw = (settings as Obj).network;
      }
    }
    const list = str(raw)
      .split(',')
      .map((n) => n.trim().toLowerCase())
      .filter((n) => n !== '');
    return list.length > 0 ? list : ['tcp'];
  }

  // Повтор адреса и порта — только при общем транспорте: redirect (tcp) и tproxy (udp) XKeen
  // стоят на одном порту, и это верная раскладка
  function sharesNetwork(a: Row, b: Row): boolean {
    const other = networksOf(b);
    return networksOf(a).some((n) => other.includes(n));
  }

  // Адрес прослушивания для сравнения повтора: пустой равен 0.0.0.0, как на сервере
  function listenOf(row: Row): string {
    return row.listen.trim() || '0.0.0.0';
  }

  // Порт закреплённой строки изменён: способ применения станет другим (D-16)
  function portChanged(row: Row): boolean {
    if (baseline === null || row.orig === null) return false;
    const was = baseline[str(row.orig.tag)];
    return was !== undefined && was !== row.port.trim();
  }

  // Отказы сервера: сырые ключи — в «Дополнительно», остальные — под списком
  const advIssues = $derived(issues.filter((i) => i.code.startsWith('raw_')));
  const listIssues = $derived(
    issues
      .filter((i) => !i.code.startsWith('raw_'))
      .map((i) => coresErrorText($t, i.code, { key: i.key }))
      .filter((line, i, all) => all.indexOf(line) === i)
  );

  /**
   * Текст «Дополнительно» с учётом правок строк: у удалённого входящего запись остатка
   * убирается, у переименованного — переезжает под новый тег. Иначе сервер вернул бы такое
   * входящее из остатка. Не разобрался как объект — текст остаётся как есть.
   */
  function advancedFor(valid: boolean[]): string {
    let obj: unknown;
    try {
      obj = JSON.parse(advanced);
    } catch {
      return advanced;
    }
    if (obj === null || typeof obj !== 'object' || Array.isArray(obj)) return advanced;
    const rest = obj as Obj;
    const kept = rows.flatMap((r) => (r.orig !== null ? [str(r.orig.tag)] : []));
    let changed = false;
    for (const origRow of rowsOf(section.form)) {
      const tag = str(origRow.orig?.tag);
      if (tag !== '' && !kept.includes(tag) && tag in rest) {
        delete rest[tag];
        changed = true;
      }
    }
    const moved: [string, unknown][] = [];
    rows.forEach((r, i) => {
      if (r.orig === null || !valid[i]) return;
      const from = str(r.orig.tag);
      const to = r.tag.trim();
      if (from !== '' && to !== from && from in rest) {
        moved.push([to, rest[from]]);
        delete rest[from];
        changed = true;
      }
    });
    for (const [to, value] of moved) rest[to] = value;
    return changed ? JSON.stringify(rest, null, 2) : advanced;
  }

  async function commit(): Promise<void> {
    if (readonly) return;
    const valid = errors.map((e) => e.tag === '' && e.port === '');
    const out: Obj[] = [];
    rows.forEach((r, i) => {
      if (!valid[i]) {
        if (r.orig !== null) out.push(r.orig);
        return;
      }
      const item: Obj = { tag: r.orig !== null && pinned(r) ? r.orig.tag : r.tag.trim() };
      item.protocol = r.protocol;
      if (r.listen.trim() !== '') item.listen = r.listen.trim();
      const port = r.port.trim();
      item.port = r.orig !== null && port === str(r.orig.port) ? r.orig.port : Number(port);
      out.push(item);
    });
    issues = await commitSection(kernel, ID, out, advancedFor(valid));
  }

  function patch(key: number, change: Partial<Row>): void {
    rows = rows.map((r) => (r.key === key ? { ...r, ...change } : r));
  }

  /** Поле строки изменено: строка с ошибкой в черновик не уходит. */
  async function onField(index: number): Promise<void> {
    const e = errors[index];
    if (e === undefined || e.tag !== '' || e.port !== '') return;
    await commit();
  }

  async function addRow(): Promise<void> {
    const key = rows.reduce((max, r) => Math.max(max, r.key), -1) + 1;
    rows = [...rows, { key, orig: null, tag: '', protocol: 'socks', listen: '', port: '' }];
    await tick();
    document.getElementById(`${uid}-tag-${key}`)?.focus();
  }

  function removeRow(key: number): void {
    rows = rows.filter((r) => r.key !== key);
    void commit();
  }

  async function saveAdvanced(text: string): Promise<void> {
    advanced = text;
    await commit();
  }

  // --- Группа для входящих force-proxy (D-12) ---

  const fpShown = $derived((forceProxy?.tags.length ?? 0) > 0);
  const fpEditable = $derived(!readonly && forceProxy?.editable === true);
  const groups = $derived($routingView?.groups ?? []);
  // Группа не выбрана — берётся группа «Прокси»
  const fpValue = $derived(
    forceProxy?.group || groups.find((g) => isForceProxyDefaultGroup(g.name))?.id || ''
  );

  $effect(() => {
    if (fpShown && $routingView === null) void loadRouting();
  });

  async function onGroup(event: Event & { currentTarget: HTMLSelectElement }): Promise<void> {
    const group = event.currentTarget.value;
    await guarded(() => setForceProxyGroup(kernel, group));
  }
</script>

<div class="xf" data-testid="section-form-xray-inbounds">
  <div class="xi-block">
    <span class="xi-title" id="{uid}-list-lbl">{$t('cfg.sec.title.xray.inbounds')}</span>

    {#if rows.length === 0}
      <p class="xi-note" data-testid="{fieldTestId(kernel, ID, 'list')}-empty">
        {$t('cfg.sec.list_empty')}
      </p>
    {/if}

    <ul class="xi-list" aria-labelledby="{uid}-list-lbl">
      {#each rows as row, i (row.key)}
        {@const fixed = pinned(row)}
        {@const err = errors[i] ?? { tag: '', port: '' }}
        <li
          class="xi-item"
          data-testid="section-inbound-row-{i}"
          data-pinned={fixed ? 'true' : 'false'}
        >
          <div class="xi-f">
            {#if fixed && !readonly}
              <span class="xi-lbl">{$t('cfg.sec.f.tag')}</span>
              <span class="xi-text" data-testid="section-inbound-tag-{i}">
                {row.tag}
              </span>
              <span class="badge xi-badge" data-testid="section-inbound-pinned-{i}">
                {$t('cfg.sec.pinned')}
              </span>
            {:else}
              <label class="xi-lbl" for="{uid}-tag-{row.key}">{$t('cfg.sec.f.tag')}</label>
              <input
                id="{uid}-tag-{row.key}"
                class="input xi-mono"
                class:input-error={err.tag !== ''}
                type="text"
                spellcheck="false"
                autocomplete="off"
                autocapitalize="off"
                {readonly}
                aria-readonly={readonly ? 'true' : undefined}
                aria-invalid={err.tag !== '' ? 'true' : 'false'}
                aria-describedby={err.tag !== '' ? `${uid}-tag-${row.key}-err` : undefined}
                data-testid="{fieldTestId(kernel, ID, 'tag')}-{i}"
                value={row.tag}
                oninput={(e) => patch(row.key, { tag: e.currentTarget.value })}
                onchange={() => onField(i)}
              />
              {#if readonly && fixed}
                <span class="badge xi-badge" data-testid="section-inbound-pinned-{i}">
                  {$t('cfg.sec.pinned')}
                </span>
              {/if}
              {#if err.tag !== ''}
                <span class="error-text" id="{uid}-tag-{row.key}-err" role="alert">{err.tag}</span>
              {/if}
            {/if}
          </div>

          <div class="xi-f">
            {#if row.orig === null && !readonly}
              <label class="xi-lbl" for="{uid}-protocol-{row.key}">{$t('cfg.sec.f.protocol')}</label
              >
              <Select
                id="{uid}-protocol-{row.key}"
                class="form-select"
                value={row.protocol}
                data-testid="{fieldTestId(kernel, ID, 'protocol')}-{i}"
                onchange={(e) => {
                  patch(row.key, { protocol: e.currentTarget.value });
                  void onField(i);
                }}
              >
                {#each NEW_PROTOCOLS as item (item)}
                  <option value={item}>{item}</option>
                {/each}
              </Select>
            {:else if readonly}
              <label class="xi-lbl" for="{uid}-protocol-{row.key}">{$t('cfg.sec.f.protocol')}</label
              >
              <input
                id="{uid}-protocol-{row.key}"
                class="input xi-mono"
                type="text"
                readonly
                aria-readonly="true"
                data-testid="{fieldTestId(kernel, ID, 'protocol')}-{i}"
                value={row.protocol}
              />
            {:else}
              <span class="xi-lbl">{$t('cfg.sec.f.protocol')}</span>
              <span class="xi-text" data-testid="section-inbound-protocol-{i}">
                {row.protocol}
              </span>
            {/if}
          </div>

          <div class="xi-f">
            {#if fixed && !readonly}
              <span class="xi-lbl">{$t('cfg.sec.f.listen')}</span>
              <span class="xi-text" data-testid="section-inbound-listen-{i}">
                {row.listen}
              </span>
            {:else}
              <label class="xi-lbl" for="{uid}-listen-{row.key}">{$t('cfg.sec.f.listen')}</label>
              <input
                id="{uid}-listen-{row.key}"
                class="input xi-mono"
                type="text"
                spellcheck="false"
                autocomplete="off"
                autocapitalize="off"
                {readonly}
                aria-readonly={readonly ? 'true' : undefined}
                data-testid="{fieldTestId(kernel, ID, 'listen')}-{i}"
                value={row.listen}
                oninput={(e) => patch(row.key, { listen: e.currentTarget.value })}
                onchange={() => onField(i)}
              />
            {/if}
          </div>

          <div class="xi-f">
            <label class="xi-lbl" for="{uid}-port-{row.key}">{$t('cfg.sec.f.port')}</label>
            <input
              id="{uid}-port-{row.key}"
              class="input xi-mono"
              class:input-error={err.port !== ''}
              type="text"
              inputmode="numeric"
              autocomplete="off"
              {readonly}
              aria-readonly={readonly ? 'true' : undefined}
              aria-invalid={err.port !== '' ? 'true' : 'false'}
              aria-describedby={err.port !== ''
                ? `${uid}-port-${row.key}-err`
                : fixed
                  ? `${uid}-port-${row.key}-hint`
                  : undefined}
              data-testid="{fieldTestId(kernel, ID, 'port')}-{i}"
              value={row.port}
              oninput={(e) => patch(row.key, { port: e.currentTarget.value })}
              onchange={() => onField(i)}
            />
            {#if err.port !== ''}
              <span class="error-text" id="{uid}-port-{row.key}-err" role="alert">{err.port}</span>
            {:else if fixed}
              <span class="xi-hint" id="{uid}-port-{row.key}-hint">
                {$t('cfg.sec.f.port_xkeen')}
              </span>
            {/if}
            {#if fixed && !readonly && portChanged(row)}
              <span class="warning-text" data-testid="section-restart-note-xray-inbounds">
                {$t('cfg.sec.restart_xkeen')}
              </span>
            {/if}
          </div>

          {#if !readonly && !fixed}
            <button
              type="button"
              class="btn btn-secondary btn-sm xi-remove"
              aria-label="{$t('cfg.sec.f.remove_inbound')} {row.tag || i + 1}"
              data-testid="{fieldTestId(kernel, ID, 'remove')}-{i}"
              onclick={() => removeRow(row.key)}
            >
              {$t('cfg.sec.f.remove_inbound')}
            </button>
          {/if}
        </li>
      {/each}
    </ul>

    {#if listIssues.length > 0}
      <div class="error-text xi-issues" role="alert" data-testid="section-inbounds-issues">
        {#each listIssues as line (line)}
          <span class="xi-line">{line}</span>
        {/each}
      </div>
    {/if}

    {#if !readonly}
      <div class="xi-actions">
        <button
          type="button"
          class="btn btn-secondary btn-sm xi-add"
          data-testid={fieldTestId(kernel, ID, 'add')}
          onclick={addRow}
        >
          {$t('cfg.sec.f.add_inbound')}
        </button>
      </div>
    {/if}
  </div>

  {#if fpShown}
    <div class="xi-fp" data-testid="section-force-proxy-xray">
      <label class="xi-lbl" for="{uid}-fp">{$t('cfg.sec.f.force_proxy_group')}</label>
      <Select
        id="{uid}-fp"
        class="form-select"
        value={fpValue}
        disabled={!fpEditable}
        data-testid="section-force-proxy-select-xray"
        onchange={onGroup}
      >
        {#each groups as g (g.id)}
          <option value={g.id}>{g.name}</option>
        {/each}
      </Select>
      {#if forceProxy?.editable === false}
        <span class="xi-hint" data-testid="section-force-proxy-locked-xray">
          {$t('cfg.sec.force_proxy_locked')}
        </span>
      {/if}
      <span class="xi-hint">{$t('cfg.sec.sb_note')}</span>
    </div>
  {/if}

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

  .xi-block {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .xi-title {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .xi-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .xi-item {
    display: grid;
    grid-template-columns:
      minmax(0, 1.3fr) minmax(0, 0.9fr) minmax(0, 1.2fr) minmax(0, 0.9fr)
      auto;
    gap: var(--spacing-2) var(--spacing-3);
    align-items: start;
    min-width: 0;
  }

  .xi-f {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .xi-lbl {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .xi-f :global(.input),
  .xi-f :global(.xcp-select),
  .xi-fp :global(.xcp-select) {
    min-width: 0;
  }

  .xi-f :global(input[readonly]) {
    color: var(--fg-secondary);
  }

  .xi-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .xi-text {
    display: flex;
    align-items: center;
    min-height: var(--input-h);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .xi-badge {
    align-self: flex-start;
  }

  .xi-hint,
  .xi-note {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .xi-f :global(.warning-text),
  .xi-f :global(.error-text) {
    overflow-wrap: anywhere;
  }

  .xi-remove {
    align-self: end;
    flex-shrink: 0;
  }

  .xi-actions {
    display: flex;
  }

  .xi-issues {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .xi-line {
    display: block;
  }

  .xi-fp {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    max-width: 480px;
  }

  @media (max-width: 768px) {
    .xi-item {
      grid-template-columns: minmax(0, 1fr);
      padding: var(--spacing-3);
      border: 1px solid var(--border-light);
      border-radius: var(--radius-md);
    }

    .xi-remove {
      align-self: stretch;
      width: 100%;
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .xi-remove,
    .xi-add {
      min-height: 44px;
    }
  }
</style>
