<script lang="ts">
  import { tick } from 'svelte';
  import Select from '../../../Select.svelte';
  import { t } from '../../../../i18n';
  import { layerSnapshot } from '../../../../lib/configLayer';
  import { loadRouting, routingView } from '../../../../lib/configRouting';
  import {
    setForceProxyGroup,
    type CoreKernel,
    type ForceProxyView,
    type KeyIssue,
    type SectionView
  } from '../../../../lib/configCores';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import { guarded } from '../coresOwner';
  import MihomoField from '../MihomoField.svelte';
  import MihomoToggle from '../MihomoToggle.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import { KERNEL_DEFAULTS, commitSection, fieldTestId, getPath, setPath } from '../sectionForm';

  // Форма «Входящие/TUN» Mihomo (SEC-02, D-12, D-16): listeners (имя, тип, адрес, порт),
  // блок TUN и группа для входящих force-proxy*. Listener с типом вне redir/tproxy/mixed/
  // socks/http — строка «Сложный слушатель»: он правится в «Дополнительно» и уходит в
  // секцию без изменений. Строки формы и записи «Дополнительно» сопоставляет сервер по
  // имени; переименование и удаление строки форма повторяет в записи «Дополнительно», чтобы
  // listener не вернулся со своими прочими полями. Смена tun и порта redir/tproxy
  // объявляет способ перезапуска по снимку слоя. Чужая секция (readonly) — только чтение.
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
  const defaults = KERNEL_DEFAULTS['mihomo.inbounds'] ?? {};
  const TYPES = ['redir', 'tproxy', 'mixed', 'socks', 'http'];
  const STACKS = ['system', 'gvisor', 'mixed'];

  type Obj = Record<string, unknown>;

  interface Row {
    key: string;
    name: string;
    type: string;
    listen: string;
    port: string;
    /** Строка формы так, как её отдал сервер; null — строка добавлена в форме. */
    orig: Obj | null;
    /** Имя в виде секции; по нему сервер находит запись «Дополнительно». */
    origName: string | null;
    complex: boolean;
    /** Поле строки правили: ошибки показываются только после правки. */
    touched: boolean;
  }

  function isObj(v: unknown): v is Obj {
    return v !== null && typeof v === 'object' && !Array.isArray(v);
  }

  function text(v: unknown): string {
    return v === undefined || v === null ? '' : String(v);
  }

  function rowsOf(form: unknown): Row[] {
    const list = getPath(form, 'listeners');
    if (!Array.isArray(list)) return [];
    return list.filter(isObj).map((o, i) => ({
      key: `r${i}`,
      name: text(o.name),
      type: text(o.type),
      listen: text(o.listen),
      port: text(o.port),
      orig: o,
      origName: text(o.name),
      complex: !TYPES.includes(text(o.type)),
      touched: false
    }));
  }

  // Значения формы: производные от вида секции, правка их переопределяет, новый вид
  // секции (после записи в черновик) сбрасывает к нему
  let rows = $derived<Row[]>(rowsOf(section.form));
  let local = $derived<Obj>(isObj(section.form) ? section.form : {});
  let issues = $state<KeyIssue[]>([]);
  let seq = 0;

  // --- Проверка строк ---

  function portOk(port: string): boolean {
    const p = port.trim();
    return /^\d{1,5}$/.test(p) && Number(p) >= 1 && Number(p) <= 65535;
  }

  function duplicate(list: Row[], i: number): boolean {
    const name = list[i].name.trim();
    return name !== '' && list.findIndex((r) => r.name.trim() === name) < i;
  }

  function nameError(list: Row[], i: number): string {
    return duplicate(list, i)
      ? $t('cfg.sec.err_duplicate', { what: $t('cfg.sec.f.name'), value: list[i].name.trim() })
      : '';
  }

  function portError(row: Row): string {
    return row.touched && !row.complex && !portOk(row.port) ? $t('cfg.sec.err_port') : '';
  }

  function rowOk(list: Row[], i: number): boolean {
    const r = list[i];
    return r.complex || (r.name.trim() !== '' && portOk(r.port) && !duplicate(list, i));
  }

  const allOk = (list: Row[]): boolean => list.every((_, i) => rowOk(list, i));

  // --- Запись ---

  function serialize(r: Row, ok: boolean): Obj | null {
    if (r.complex && r.orig !== null) return r.orig;
    if (!ok) return r.orig;
    const out: Obj = { name: r.name.trim(), type: r.type };
    if (r.listen.trim() !== '') out.listen = r.listen.trim();
    out.port = Number(r.port.trim());
    return out;
  }

  function formOf(list: Row[], tunValue: Obj): Obj | null {
    const items: Obj[] = [];
    for (let i = 0; i < list.length; i++) {
      const item = serialize(list[i], rowOk(list, i));
      if (item !== null) items.push(item);
    }
    const names = items.map((o) => text(o.name));
    if (new Set(names).size !== names.length) return null;
    const form: Obj = {};
    if (items.length > 0) form.listeners = items;
    if (Object.keys(tunValue).length > 0) form.tun = tunValue;
    return form;
  }

  function tunOf(source: Obj): Obj {
    const v = getPath(source, 'tun');
    return isObj(v) ? v : {};
  }

  /** Ключ записи «Дополнительно»: простое имя как есть, иначе в двойных кавычках. */
  function keyText(name: string): string {
    return /^[A-Za-z0-9_.-]+$/.test(name) ? name : JSON.stringify(name);
  }

  /** Ключ строки YAML-отображения и позиция сразу после двоеточия; не ключ — null. */
  function parseKey(line: string): { name: string; end: number } | null {
    const m = /^\s*(?:"((?:[^"\\]|\\.)*)"|'([^']*)'|([^:#\s'"][^:]*?))\s*:(?=\s|$)/.exec(line);
    if (m === null) return null;
    let name = m[2] ?? m[3] ?? '';
    if (m[1] !== undefined) {
      try {
        name = JSON.parse(`"${m[1]}"`) as string;
      } catch {
        name = m[1];
      }
    }
    return { name, end: m[0].length };
  }

  /**
   * Переименовывает (newName) или убирает (null) запись listener в «Дополнительно»:
   * записи без строки формы сервер считает новыми listeners, поэтому ключ остаётся в
   * тексте, пока строка формы жива, и уходит вместе с ней. Блок в иной записи (поток
   * в фигурных скобках) не трогается.
   */
  function editAdvanced(src: string, oldName: string, newName: string | null): string {
    const lines = src.split('\n');
    const start = lines.findIndex((l) => /^listeners:\s*$/.test(l));
    if (start < 0) return src;
    const indentOf = (l: string): number => l.length - l.trimStart().length;
    const blank = (l: string): boolean => l.trim() === '';
    let end = start + 1;
    while (end < lines.length && (blank(lines[end]) || /^\s/.test(lines[end]))) end++;
    const first = lines.slice(start + 1, end).find((l) => !blank(l));
    if (first === undefined) return src;
    const base = indentOf(first);
    let at = -1;
    for (let i = start + 1; i < end; i++) {
      if (blank(lines[i]) || indentOf(lines[i]) !== base) continue;
      if (parseKey(lines[i])?.name === oldName) {
        at = i;
        break;
      }
    }
    if (at < 0) return src;

    if (newName !== null) {
      const key = parseKey(lines[at]);
      if (key === null) return src;
      lines[at] = `${' '.repeat(base)}${keyText(newName)}:${lines[at].slice(key.end)}`;
      return lines.join('\n');
    }

    let stop = at + 1;
    while (stop < end && (blank(lines[stop]) || indentOf(lines[stop]) > base)) stop++;
    lines.splice(at, stop - at);
    const blockEnd = end - (stop - at);
    // Записей не осталось: убирается и сам ключ listeners
    if (!lines.slice(start + 1, blockEnd).some((l) => !blank(l))) {
      lines.splice(start, blockEnd - start);
    }
    return lines.join('\n');
  }

  /** Текст «Дополнительно» после правки строк: переименования и удаления повторяются в нём. */
  function advancedFor(next: Row[]): string {
    let out = section.advanced;
    const present = new Map(
      next.filter((r) => r.origName !== null).map((r) => [r.origName as string, r.name.trim()])
    );
    for (const original of rowsOf(section.form).map((r) => r.name)) {
      const now = present.get(original);
      if (now === undefined) out = editAdvanced(out, original, null);
      else if (now !== original && now !== '') out = editAdvanced(out, original, now);
    }
    return out;
  }

  async function save(form: Obj | null, advanced: string): Promise<void> {
    if (form === null) return;
    issues = await commitSection(kernel, ID, form, advanced);
  }

  /** Правка строк: в черновик уходит, только когда все строки верны. */
  function commitRows(next: Row[], force = false): void {
    rows = next;
    if (!force && !allOk(next)) return;
    void save(formOf(next, tunOf(local)), advancedFor(next));
  }

  function patch(i: number, change: Partial<Row>): void {
    commitRows(rows.map((r, j) => (j === i ? { ...r, ...change, touched: true } : r)));
  }

  function remove(i: number): void {
    commitRows(
      rows.filter((_, j) => j !== i),
      true
    );
  }

  async function addRow(): Promise<void> {
    const key = `n${seq++}`;
    rows = [
      ...rows,
      {
        key,
        name: '',
        type: 'mixed',
        listen: '',
        port: '',
        orig: null,
        origName: null,
        complex: false,
        touched: false
      }
    ];
    await tick();
    document.getElementById(`${uid}-${key}-name`)?.focus();
  }

  function setTun(path: string, value: unknown): void {
    local = setPath(local, `tun.${path}`, value);
    void save(formOf(rows, tunOf(local)), section.advanced);
  }

  async function saveAdvanced(value: string): Promise<void> {
    await save(formOf(rows, tunOf(local)), value);
  }

  // --- Тексты и объявления ---

  function tunHint(path: string): string {
    if (getPath(local, `tun.${path}`) !== undefined) return '';
    const value = defaults[`tun.${path}`];
    return value === undefined ? '' : $t('cfg.sec.default_hint', { value });
  }

  const stack = $derived(text(getPath(local, 'tun.stack')));
  const stackOptions = $derived(
    stack !== '' && !STACKS.includes(stack) ? [...STACKS, stack] : STACKS
  );

  // Объявления перезапуска считает слой по разнице черновика и применённого состояния
  const restartReasons = $derived(
    ($layerSnapshot?.cores?.restart ?? []).filter((n) => n.kernel === 'mihomo').map((n) => n.reason)
  );

  // --- Группа для входящих force-proxy* ---

  const forceShown = $derived((forceProxy?.tags.length ?? 0) > 0);
  const forceLocked = $derived(readonly || forceProxy?.editable !== true);

  $effect(() => {
    if (forceShown && $routingView === null) void loadRouting();
  });

  const groupOptions = $derived.by(() => {
    const list = ($routingView?.groups ?? []).map((g) => ({ id: g.id, name: g.name }));
    const current = forceProxy?.group ?? '';
    if (current !== '' && !list.some((g) => g.id === current)) {
      list.push({ id: current, name: current });
    }
    return list;
  });

  function setGroup(group: string): void {
    void guarded(() => setForceProxyGroup(kernel, group));
  }
</script>

<div class="mif" data-testid="section-form-mihomo-{ID}">
  <section class="mif-block" aria-labelledby="{uid}-listeners-title">
    <h4 class="mif-title" id="{uid}-listeners-title">{$t('cfg.sec.f.listeners')}</h4>

    {#if rows.length === 0 && !readonly}
      <p class="mif-note" data-testid="section-list-empty-mihomo-{ID}">
        {$t('cfg.sec.list_empty')}
      </p>
    {/if}

    {#each rows as row, i (row.key)}
      {@const nameErr = nameError(rows, i)}
      {@const portErr = portError(row)}
      <div class="mif-row" data-testid="section-listener-mihomo-{i}">
        {#if row.complex}
          <div class="mif-complex">
            <span class="mif-name">{row.name}</span>
            <span class="mif-note">{$t('cfg.sec.f.complex_listener')}</span>
          </div>
        {:else}
          <div class="mif-cell mif-cell-name">
            <label class="mif-lbl" for="{uid}-{row.key}-name">{$t('cfg.sec.f.name')}</label>
            <input
              id="{uid}-{row.key}-name"
              class="input mif-mono"
              class:input-error={nameErr !== ''}
              type="text"
              spellcheck="false"
              autocomplete="off"
              {readonly}
              aria-readonly={readonly ? 'true' : undefined}
              aria-invalid={nameErr !== '' ? 'true' : 'false'}
              aria-describedby={nameErr !== '' ? `${uid}-${row.key}-name-error` : undefined}
              data-testid="section-listener-name-mihomo-{i}"
              value={row.name}
              onchange={(e) => patch(i, { name: e.currentTarget.value })}
            />
            {#if nameErr !== ''}
              <span class="error-text mif-msg" id="{uid}-{row.key}-name-error">{nameErr}</span>
            {/if}
          </div>
          <div class="mif-cell mif-cell-type">
            <label class="mif-lbl" for="{uid}-{row.key}-type">{$t('cfg.sec.f.type')}</label>
            {#if readonly}
              <input
                id="{uid}-{row.key}-type"
                class="input"
                readonly
                aria-readonly="true"
                data-testid="section-listener-type-mihomo-{i}"
                value={row.type}
              />
            {:else}
              <Select
                id="{uid}-{row.key}-type"
                value={row.type}
                data-testid="section-listener-type-mihomo-{i}"
                onchange={(e) => patch(i, { type: e.currentTarget.value })}
              >
                {#each TYPES as ty (ty)}
                  <option value={ty}>{ty}</option>
                {/each}
              </Select>
            {/if}
          </div>
          <div class="mif-cell mif-cell-listen">
            <label class="mif-lbl" for="{uid}-{row.key}-listen">{$t('cfg.sec.f.listen')}</label>
            <input
              id="{uid}-{row.key}-listen"
              class="input mif-mono"
              type="text"
              spellcheck="false"
              autocomplete="off"
              {readonly}
              aria-readonly={readonly ? 'true' : undefined}
              data-testid="section-listener-listen-mihomo-{i}"
              value={row.listen}
              onchange={(e) => patch(i, { listen: e.currentTarget.value })}
            />
          </div>
          <div class="mif-cell mif-cell-port">
            <label class="mif-lbl" for="{uid}-{row.key}-port">{$t('cfg.sec.f.port')}</label>
            <input
              id="{uid}-{row.key}-port"
              class="input"
              class:input-error={portErr !== ''}
              type="number"
              inputmode="numeric"
              min="1"
              max="65535"
              step="1"
              {readonly}
              aria-readonly={readonly ? 'true' : undefined}
              aria-invalid={portErr !== '' ? 'true' : 'false'}
              aria-describedby={portErr !== '' ? `${uid}-${row.key}-port-error` : undefined}
              data-testid="section-listener-port-mihomo-{i}"
              value={row.port}
              onchange={(e) => patch(i, { port: e.currentTarget.value })}
            />
            {#if portErr !== ''}
              <span class="error-text mif-msg" id="{uid}-{row.key}-port-error">{portErr}</span>
            {/if}
          </div>
        {/if}
        {#if !readonly}
          <button
            type="button"
            class="btn btn-secondary btn-sm mif-remove"
            aria-label={`${$t('cfg.sec.f.remove_listener')} ${row.name}`.trim()}
            data-testid="section-remove-mihomo-inbounds-{i}"
            onclick={() => remove(i)}
          >
            {$t('cfg.sec.f.remove_listener')}
          </button>
        {/if}
      </div>
    {/each}

    {#if restartReasons.includes('port')}
      <span class="warning-text" data-testid="section-restart-note-port-mihomo-{ID}">
        {$t('cfg.sec.restart_xkeen')}
      </span>
    {/if}

    {#if !readonly}
      <div>
        <button
          type="button"
          class="btn btn-secondary btn-sm mif-add"
          data-testid="section-add-mihomo-{ID}"
          onclick={addRow}
        >
          {$t('cfg.sec.f.add_listener')}
        </button>
      </div>
    {/if}
  </section>

  {#if forceShown}
    <section class="mif-block" data-testid="section-force-proxy-mihomo">
      <MihomoField
        id="{uid}-force-proxy"
        label={$t('cfg.sec.f.force_proxy_group')}
        hint={$t('cfg.sec.sb_note')}
      >
        <Select
          id="{uid}-force-proxy"
          value={forceProxy?.group ?? ''}
          disabled={forceLocked}
          data-testid="section-force-proxy-select-mihomo"
          onchange={(e) => setGroup(e.currentTarget.value)}
        >
          <option value="">
            {$t('cfg.sec.default_option', { value: $t('groups.proxies') })}
          </option>
          {#each groupOptions as g (g.id)}
            <option value={g.id}>{g.name}</option>
          {/each}
        </Select>
        {#if forceLocked}
          <span class="mif-note" data-testid="section-force-proxy-locked-mihomo">
            {$t('cfg.sec.force_proxy_locked')}
          </span>
        {/if}
      </MihomoField>
    </section>
  {/if}

  <section class="mif-block" aria-labelledby="{uid}-tun-title">
    <h4 class="mif-title" id="{uid}-tun-title">{$t('cfg.sec.f.tun')}</h4>
    <MihomoToggle
      id="{uid}-tun-enable"
      label={$t('cfg.sec.f.tun_enable')}
      checked={getPath(local, 'tun.enable') === true}
      {readonly}
      hint={tunHint('enable')}
      testid={fieldTestId(kernel, ID, 'tun.enable')}
      onchange={(v) => setTun('enable', v)}
    />
    <MihomoField id="{uid}-tun-stack" label={$t('cfg.sec.f.stack')}>
      {#if readonly}
        <input
          id="{uid}-tun-stack"
          class="input"
          readonly
          aria-readonly="true"
          data-testid={fieldTestId(kernel, ID, 'tun.stack')}
          value={stack}
        />
      {:else}
        <Select
          id="{uid}-tun-stack"
          value={stack}
          data-testid={fieldTestId(kernel, ID, 'tun.stack')}
          onchange={(e) => setTun('stack', e.currentTarget.value)}
        >
          <option value="">
            {$t('cfg.sec.default_option', { value: defaults['tun.stack'] ?? '' })}
          </option>
          {#each stackOptions as s (s)}
            <option value={s}>{s}</option>
          {/each}
        </Select>
      {/if}
    </MihomoField>
    <MihomoToggle
      id="{uid}-tun-auto-route"
      label={$t('cfg.sec.f.auto_route')}
      checked={getPath(local, 'tun.auto-route') === true}
      {readonly}
      hint={tunHint('auto-route')}
      testid={fieldTestId(kernel, ID, 'tun.auto-route')}
      onchange={(v) => setTun('auto-route', v)}
    />
    {#if restartReasons.includes('tun')}
      <span
        class="warning-text mif-restart"
        data-testid="section-restart-note-mihomo-inbounds"
        data-reason="tun"
      >
        {$t('cfg.sec.restart_tun')}
      </span>
    {/if}
  </section>

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
  .mif {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .mif-block {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .mif-title {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .mif-note {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .mif-row {
    display: grid;
    grid-template-columns:
      minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1.4fr) minmax(0, 0.8fr)
      auto;
    gap: var(--spacing-3);
    align-items: end;
    min-width: 0;
    padding: var(--spacing-3);
    background: var(--bg-elevated);
    border: 1px solid var(--border-light);
    border-radius: var(--radius-md);
  }

  .mif-cell {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .mif-lbl {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .mif-cell :global(.input) {
    min-width: 0;
  }

  .mif-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .mif-msg {
    margin-top: 0;
    overflow-wrap: anywhere;
  }

  .mif-complex {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    grid-column: 1 / span 4;
    min-width: 0;
  }

  .mif-name {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .mif-remove {
    justify-self: end;
  }

  .mif-restart {
    margin-top: 0;
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .mif :global(.input),
    .mif :global(.xcp-select select),
    .mif-remove,
    .mif-add {
      min-height: 44px;
    }
  }

  @media (max-width: 768px) {
    .mif-row {
      grid-template-columns: minmax(0, 1fr);
      align-items: stretch;
    }

    .mif-complex {
      grid-column: auto;
    }

    .mif-remove {
      width: 100%;
      justify-self: stretch;
    }
  }
</style>
