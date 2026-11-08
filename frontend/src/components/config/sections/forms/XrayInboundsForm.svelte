<script lang="ts">
  import { tick } from 'svelte';
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
    /** Ключ #each: «s<номер>» у строки вида, localId у добавленной в этой сессии. */
    key: string;
    /** Номер строки в виде секции: по нему ключ «#<номер>» остатка входящего без тега. */
    index: number;
    /** Строка формы из вида секции; null — добавленная, ещё не ушедшая в черновик. */
    orig: Obj | null;
    /** Локальный ключ добавленной строки; у строки вида его нет. */
    localId?: string;
    tag: string;
    protocol: string;
    listen: string;
    port: string;
  }

  function str(value: unknown): string {
    return value === undefined || value === null ? '' : String(value);
  }

  // Ключ строки вида — её номер: после перечитывания поля остаются на месте и не теряют фокус
  function rowsOf(form: unknown): Row[] {
    if (!Array.isArray(form)) return [];
    return form.map((raw, index) => {
      const o = (raw !== null && typeof raw === 'object' ? raw : {}) as Obj;
      return {
        key: `s${index}`,
        index,
        orig: o,
        tag: str(o.tag),
        protocol: str(o.protocol),
        listen: str(o.listen),
        port: str(o.port)
      };
    });
  }

  // Строки вида выводятся из вида секции; ввод их переопределяет, новый вид обновляет. Добавленные
  // строки, которые ещё не ушли в черновик (нет порта, ошибка проверки), живут отдельно в
  // localRows: событие черновика от другой правки перечитывает вид, но их не трогает.
  let serverRows = $derived(rowsOf(section.form));
  let localRows = $state.raw<Row[]>([]);
  // Тег строки, ушедшей из localRows в черновик → её localId: строка вида с этим тегом остаётся
  // тем же элементом списка, поле в фокусе не пересоздаётся при приходе нового вида
  let adoptedKeys = $state.raw<Map<string, string>>(new Map());
  // Номер для localId: crypto.randomUUID нет вне защищённого контекста (панель по http)
  let localSeq = 0;

  const rows = $derived.by<Row[]>(() => {
    const used = new Set<string>();
    const server = serverRows.map((r) => {
      const id = r.orig !== null ? adoptedKeys.get(str(r.orig.tag).trim()) : undefined;
      if (id === undefined || used.has(id)) return r;
      used.add(id);
      return { ...r, key: id };
    });
    // Чужая секция (readonly) показывает только действующие значения
    return readonly ? server : [...server, ...localRows];
  });

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
  // Прежняя база держится в обычной переменной: это кэш вычисления, а не состояние формы.
  let lastBaseline: Record<string, string> | null = null;
  const baseline = $derived.by<Record<string, string>>(() => {
    const ports: Record<string, string> = {};
    for (const r of rowsOf(section.form)) ports[r.tag] = r.port;
    if (lastBaseline === null || !section.dirty) lastBaseline = ports;
    return lastBaseline;
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
        const entry = advancedObj[tag !== '' ? tag : `#${row.index}`];
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
    if (row.orig === null) return false;
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
   * Текст «Дополнительно» с учётом правок строк. Запись остатка входящего с тегом ключ несёт
   * тегом, без тега — «#<номер в списке>». У удалённого входящего запись убирается, у
   * переименованного или получившего тег — переезжает под новый ключ, у входящего без тега
   * после удаления строки выше — под новый номер. Иначе сервер не нашёл бы остаток и потерял
   * settings, streamSettings, sniffing. outIndex — номер строки в уходящем списке (null — строка
   * не уходит). Не разобрался как объект — текст остаётся как есть.
   */
  function advancedFor(valid: boolean[], outIndex: (number | null)[]): string {
    let obj: unknown;
    try {
      obj = JSON.parse(advanced);
    } catch {
      return advanced;
    }
    if (obj === null || typeof obj !== 'object' || Array.isArray(obj)) return advanced;
    const rest = obj as Obj;
    const keyOf = (r: Row): string => {
      const tag = str(r.orig?.tag);
      return tag !== '' ? tag : `#${r.index}`;
    };
    // Ключи строк вида секции до правок: все снимаются, уцелевшие возвращаются под новым ключом
    const taken = new Map<string, unknown>();
    let changed = false;
    for (const origRow of rowsOf(section.form)) {
      const key = keyOf(origRow);
      if (key in rest) {
        taken.set(key, rest[key]);
        delete rest[key];
      }
    }
    const placed: [string, string][] = [];
    rows.forEach((r, i) => {
      const index = outIndex[i];
      if (r.orig === null || index === null || index === undefined) return;
      const from = keyOf(r);
      const tag = valid[i] ? r.tag.trim() : str(r.orig.tag);
      placed.push([from, tag !== '' ? tag : `#${index}`]);
    });
    for (const [from, to] of placed) {
      if (!taken.has(from)) continue;
      rest[to] = taken.get(from);
      taken.delete(from);
      if (to !== from) changed = true;
    }
    if (taken.size > 0) changed = true;
    return changed ? JSON.stringify(rest, null, 2) : advanced;
  }

  async function commit(): Promise<void> {
    if (readonly) return;
    const valid = errors.map((e) => e.tag === '' && e.port === '');
    const out: Obj[] = [];
    const outIndex: (number | null)[] = [];
    // Добавленные строки, ушедшие в этой записи, и принятые строки, сменившие тег
    const sent: { localId: string; item: Obj }[] = [];
    const renamed: { from: string; to: string; id: string }[] = [];
    rows.forEach((r, i) => {
      if (!valid[i]) {
        if (r.orig !== null) {
          outIndex.push(out.length);
          out.push(r.orig);
        } else {
          outIndex.push(null);
        }
        return;
      }
      const item: Obj = { tag: r.orig !== null && pinned(r) ? r.orig.tag : r.tag.trim() };
      item.protocol = r.protocol;
      if (r.listen.trim() !== '') item.listen = r.listen.trim();
      const port = r.port.trim();
      item.port = r.orig !== null && port === str(r.orig.port) ? r.orig.port : Number(port);
      outIndex.push(out.length);
      out.push(item);
      if (r.orig === null && r.localId !== undefined) sent.push({ localId: r.localId, item });
      else if (r.orig !== null && r.key !== `s${r.index}`) {
        renamed.push({ from: str(r.orig.tag).trim(), to: str(item.tag), id: r.key });
      }
    });
    const result = await commitSection(kernel, ID, out, advancedFor(valid, outIndex));
    issues = result;
    if (result.length === 0) adopt(sent, renamed);
  }

  /**
   * Запись принята: ушедшие добавленные строки переходят из localRows к строкам вида (до
   * прихода нового вида — с записанным значением как исходным), их localId остаётся ключом
   * строки вида с тем же тегом. Значения, введённые за время записи, сохраняются. Строку,
   * удалённую за время записи, не возвращает; строку, которую уже принёс новый вид, не дублирует.
   */
  function adopt(
    sent: { localId: string; item: Obj }[],
    renamed: { from: string; to: string; id: string }[]
  ): void {
    if (sent.length === 0 && renamed.length === 0) return;
    const keys = new Map(adoptedKeys);
    for (const { from, to, id } of renamed) {
      if (keys.get(from) === id) keys.delete(from);
      keys.set(to, id);
    }
    const appended: Row[] = [];
    const moved = new Set<string>();
    for (const { localId, item } of sent) {
      const cur = localRows.find((r) => r.localId === localId);
      if (cur === undefined) continue;
      const tag = str(item.tag);
      keys.set(tag, localId);
      moved.add(localId);
      if (serverRows.some((r) => r.orig !== null && str(r.orig.tag).trim() === tag)) continue;
      const index = serverRows.length + appended.length;
      appended.push({
        key: `s${index}`,
        index,
        orig: item,
        tag: cur.tag,
        protocol: cur.protocol,
        listen: cur.listen,
        port: cur.port
      });
    }
    adoptedKeys = keys;
    if (appended.length > 0) serverRows = [...serverRows, ...appended];
    if (moved.size > 0) localRows = localRows.filter((r) => !moved.has(r.localId ?? ''));
  }

  /** Ввод в поле: строка вида правится в serverRows, добавленная — в localRows по localId. */
  function patch(row: Row, change: Partial<Row>): void {
    if (row.orig === null) {
      localRows = localRows.map((r) => (r.localId === row.localId ? { ...r, ...change } : r));
    } else {
      serverRows = serverRows.map((r) => (r.index === row.index ? { ...r, ...change } : r));
    }
  }

  /** Поле строки изменено: строка с ошибкой в черновик не уходит. */
  async function onField(index: number): Promise<void> {
    const e = errors[index];
    if (e === undefined || e.tag !== '' || e.port !== '') return;
    await commit();
  }

  async function addRow(): Promise<void> {
    const localId = `l${++localSeq}`;
    localRows = [
      ...localRows,
      {
        key: localId,
        index: -1,
        localId,
        orig: null,
        tag: '',
        protocol: 'socks',
        listen: '',
        port: ''
      }
    ];
    await tick();
    document.getElementById(`${uid}-tag-${localId}`)?.focus();
  }

  function removeRow(row: Row): void {
    if (row.orig === null) {
      localRows = localRows.filter((r) => r.localId !== row.localId);
    } else {
      serverRows = serverRows.filter((r) => r.index !== row.index);
    }
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
              <span class="badge badge--tag xi-badge" data-testid="section-inbound-pinned-{i}">
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
                oninput={(e) => patch(row, { tag: e.currentTarget.value })}
                onchange={() => onField(i)}
              />
              {#if readonly && fixed}
                <span class="badge badge--tag xi-badge" data-testid="section-inbound-pinned-{i}">
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
                  patch(row, { protocol: e.currentTarget.value });
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
                oninput={(e) => patch(row, { listen: e.currentTarget.value })}
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
              oninput={(e) => patch(row, { port: e.currentTarget.value })}
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
              onclick={() => removeRow(row)}
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
