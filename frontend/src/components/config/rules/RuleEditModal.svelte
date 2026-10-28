<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Select from '../../Select.svelte';
  import SegmentedControl from '../../SegmentedControl.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError } from '../../../lib/configLayer';
  import { routingView, routingErrorText, saveRule } from '../../../lib/configRouting';
  import type { ListRef, RawRule, RuleView, Target } from '../../../lib/configRouting';
  import { condTypeKey, condXrayField, targetLabel } from '../../../lib/routingView';
  import ListPickerModal from './ListPickerModal.svelte';

  // Диалог правила: условия через «И», цель (группа, DIRECT или REJECT) и «своё правило ядра»
  // в режиме «Эксперт». Клиентская проверка значений — только подсказка формата для мгновенной
  // реакции; решение принимает ответ сервера (invalid_input с field и reason ставится у нужного
  // условия). Подсказка «Уже есть в правиле» ищет лишь точное совпадение значения среди правил
  // представления: перекрытия считает бэкенд, клиент их не пересчитывает (D-40).
  let {
    open,
    rule,
    initialList = null,
    onclose
  }: {
    open: boolean;
    /** Правка существующего правила; null — новое. */
    rule: RuleView | null;
    /** Новое правило начинается с условия-списка «файл · тег» (из инспектора и поиска). */
    initialList?: ListRef | null;
    onclose: () => void;
  } = $props();

  const uid = $props.id();

  interface CondDraft {
    key: number;
    type: string;
    /** Значения текстом: домены, IP, порты — по одному в строке или через запятую. */
    text: string;
    tcp: boolean;
    udp: boolean;
    lists: ListRef[];
    touched: boolean;
  }

  /** Типы условий в порядке списка; у каждого есть cfg.rules.type_<тип>. */
  const COND_TYPES = [
    'domain',
    'geosite',
    'ip',
    'geoip',
    'port',
    'network',
    'src_ip',
    'bittorrent'
  ];
  const TEXT_TYPES = new Set(['domain', 'ip', 'src_ip', 'port']);
  const LIST_TYPES = new Set(['geosite', 'geoip']);
  // Значения, по которым ищется повтор в других правилах
  const DUP_TYPES = new Set(['domain', 'ip', 'src_ip', 'port']);

  let nextKey = 0;
  let conds = $state<CondDraft[]>([]);
  let targetSel = $state('direct');
  let expert = $state(false);
  let seg = $state('conds');
  let rawKernel = $state('xray');
  let rawText = $state('');
  let saving = $state(false);
  let saveError = $state('');
  // Ошибки ответа сервера по ключу условия и для своего правила; правка поля убирает ошибку
  let serverCondErrors = $state<Record<number, string>>({});
  let rawError = $state('');

  const view = $derived($routingView);
  const groups = $derived(view?.groups ?? []);
  const installed = $derived(view?.installed ?? {});

  // --- Исходные значения ---

  function newCond(type = 'domain'): CondDraft {
    return { key: nextKey++, type, text: '', tcp: true, udp: false, lists: [], touched: false };
  }

  /** Первое условие нового правила: список из initialList либо пустое условие. */
  function initialCond(): CondDraft {
    const ref = initialList;
    if (ref === null) return newCond();
    const d = newCond(ref.kind === 'ip' ? 'geoip' : 'geosite');
    d.lists = [{ ...ref }];
    return d;
  }

  function targetValue(target: Target): string {
    return target.kind === 'group' ? `group:${target.id ?? ''}` : target.kind;
  }

  function targetOf(value: string): Target {
    if (value.startsWith('group:')) return { kind: 'group', id: value.slice('group:'.length) };
    return { kind: value === 'reject' ? 'reject' : 'direct' };
  }

  function fromRule(r: RuleView): CondDraft[] {
    return r.conds.map((c) => {
      const d = newCond(c.type);
      if (TEXT_TYPES.has(c.type)) d.text = c.values.join('\n');
      if (c.type === 'network') {
        d.tcp = c.values.includes('tcp');
        d.udp = c.values.includes('udp');
      }
      d.lists = c.lists.map((l) => ({ ...l }));
      return d;
    });
  }

  function init(r: RuleView | null) {
    nextKey = 0;
    saving = false;
    saveError = '';
    serverCondErrors = {};
    rawError = '';
    if (r === null) {
      conds = [initialCond()];
      targetSel = 'direct';
      expert = false;
      seg = 'conds';
      rawKernel = installed.xray || !installed.mihomo ? 'xray' : 'mihomo';
      rawText = '';
      return;
    }
    targetSel = targetValue(r.target);
    if (r.raw) {
      conds = [];
      expert = true;
      seg = 'raw';
      rawKernel = r.raw.kernel === 'mihomo' ? 'mihomo' : 'xray';
      rawText = r.raw.text;
    } else {
      conds = fromRule(r);
      expert = false;
      seg = 'conds';
      rawKernel = installed.xray || !installed.mihomo ? 'xray' : 'mihomo';
      rawText = '';
    }
  }

  $effect(() => {
    if (open) {
      const r = rule;
      untrack(() => init(r));
    }
  });

  // --- Разбор и проверка формата (только подсказка) ---

  function parseValues(text: string): string[] {
    return text
      .split(/[\s,]+/)
      .map((v) => v.trim())
      .filter((v) => v !== '');
  }

  const LABEL = '[\\p{L}\\p{N}_](?:[\\p{L}\\p{N}_-]*[\\p{L}\\p{N}_])?';
  const DOMAIN_RE = new RegExp(`^${LABEL}(?:\\.${LABEL})*$`, 'u');
  const PLAIN_TOKEN_RE = /^[^\s,()`"']+$/;
  const IPV4_RE = /^(\d{1,3})(?:\.(\d{1,3})){3}(?:\/(\d{1,2}))?$/;
  const IPV6_RE = /^[0-9a-fA-F:.]+(?:\/(\d{1,3}))?$/;
  const PORT_RE = /^(\d{1,5})(?:-(\d{1,5}))?$/;
  const DOMAIN_MAX = 253;

  function domainOk(v: string): boolean {
    const low = v.toLowerCase();
    if (low.startsWith('full:')) return plainDomain(v.slice(5));
    if (low.startsWith('keyword:')) return PLAIN_TOKEN_RE.test(v.slice(8));
    if (low.startsWith('regexp:')) return /^[^\s,`]+$/.test(v.slice(7));
    return plainDomain(v);
  }

  function plainDomain(v: string): boolean {
    return v.length > 0 && v.length <= DOMAIN_MAX && DOMAIN_RE.test(v.toLowerCase());
  }

  function ipOk(v: string): boolean {
    const m4 = IPV4_RE.exec(v);
    if (m4) {
      const octets = v.split('/')[0].split('.').map(Number);
      const prefix = m4[3] === undefined ? 0 : Number(m4[3]);
      return octets.every((o) => o <= 255) && prefix <= 32;
    }
    const m6 = IPV6_RE.exec(v);
    if (m6) {
      const prefix = m6[1] === undefined ? 0 : Number(m6[1]);
      return v.split('/')[0].includes(':') && prefix <= 128;
    }
    return false;
  }

  function portOk(v: string): boolean {
    const m = PORT_RE.exec(v);
    if (!m) return false;
    const lo = Number(m[1]);
    if (lo < 1 || lo > 65535) return false;
    if (m[2] === undefined) return true;
    const hi = Number(m[2]);
    return hi >= lo && hi <= 65535;
  }

  interface CondCheck {
    /** Условие готово к сохранению. */
    ok: boolean;
    /** Текст ошибки под полем; пустой — ошибки нет (поле просто ещё не заполнено). */
    error: string;
  }

  const okCheck: CondCheck = { ok: true, error: '' };

  function check(c: CondDraft): CondCheck {
    if (c.type === 'bittorrent') return { ok: true, error: '' };
    if (c.type === 'network') {
      const ok = c.tcp || c.udp;
      return { ok, error: ok ? '' : $t('cfg.rules.network_min') };
    }
    if (LIST_TYPES.has(c.type)) {
      const ok = c.lists.length > 0;
      return { ok, error: ok || !c.touched ? '' : $t('cfg.rules.err_values') };
    }
    const values = parseValues(c.text);
    if (values.length === 0) {
      return { ok: false, error: c.touched ? $t('cfg.rules.err_values') : '' };
    }
    if (c.type === 'domain') {
      return domainsBad(values) ? { ok: false, error: $t('cfg.rules.err_domain') } : okCheck;
    }
    if (c.type === 'port') {
      return values.some((v) => !portOk(v))
        ? { ok: false, error: $t('cfg.rules.err_port') }
        : okCheck;
    }
    const bad = values.find((v) => !ipOk(v));
    return bad === undefined
      ? okCheck
      : { ok: false, error: $t('cfg.rules.err_cidr', { value: bad }) };
  }

  function domainsBad(values: string[]): boolean {
    return values.some((v) => !domainOk(v));
  }

  const checks = $derived(conds.map(check));
  const condsValid = $derived(conds.length > 0 && checks.every((c) => c.ok));
  const rawValid = $derived(rawText.trim() !== '');
  const rawMode = $derived(expert && seg === 'raw');
  const canSave = $derived(!saving && (rawMode ? rawValid : condsValid));

  // --- Подсказка о повторном значении ---

  function normalizeValue(type: string, v: string): string {
    const low = v.trim().toLowerCase();
    return type === 'domain' && low.startsWith('domain:') ? low.slice(7) : low;
  }

  interface DupHint {
    n: number;
    target: string;
  }

  /** Первое другое правило, у которого есть то же значение того же типа условия. */
  function dupOf(c: CondDraft): DupHint | null {
    if (!DUP_TYPES.has(c.type) || view === null) return null;
    const mine = new Set(parseValues(c.text).map((v) => normalizeValue(c.type, v)));
    if (mine.size === 0) return null;
    for (const other of view.rules) {
      if (rule !== null && other.id === rule.id) continue;
      for (const oc of other.conds) {
        if (oc.type !== c.type) continue;
        if (oc.values.some((v) => mine.has(normalizeValue(c.type, v)))) {
          return { n: other.index, target: targetLabel($t, other.target, groups) };
        }
      }
    }
    return null;
  }

  const dups = $derived(conds.map(dupOf));

  // --- Поля Xray: одно условие на поле (cond_conflict) ---

  /** Первый тип, чьё поле Xray не занято ни одним условием; undefined — свободных полей нет. */
  const freeType = $derived.by(() => {
    const used = new Set(conds.map((c) => condXrayField(c.type)));
    return COND_TYPES.find((type) => !used.has(condXrayField(type)));
  });

  /**
   * Пункты списка типа для каждого условия: тип, чьё поле Xray занято другим условием,
   * неактивен (свой текущий тип остаётся доступным, в том числе у правила, загруженного
   * с конфликтом).
   */
  const typeOptions = $derived(
    conds.map((c, i) => {
      const taken = new Set(conds.filter((_, j) => j !== i).map((o) => condXrayField(o.type)));
      return COND_TYPES.map((type) => ({
        value: type,
        label: typeLabel(type),
        disabled: type !== c.type && taken.has(condXrayField(type))
      }));
    })
  );

  // Правило с ненайденным списком: предупреждение с именем файла и тега
  const missingLists = $derived(
    (rule?.issues ?? []).filter((i: { code: string }) => i.code === 'list_not_found')
  );

  // --- Правка ---

  function addCond() {
    if (freeType === undefined) return;
    conds.push(newCond(freeType));
  }

  function removeCond(i: number) {
    conds.splice(i, 1);
  }

  function setType(c: CondDraft, type: string) {
    c.type = type;
    c.lists = [];
    if (!TEXT_TYPES.has(type)) c.text = '';
    delete serverCondErrors[c.key];
  }

  function onValuesInput(c: CondDraft) {
    c.touched = true;
    saveError = '';
    delete serverCondErrors[c.key];
  }

  function removeList(c: CondDraft, i: number) {
    c.lists.splice(i, 1);
    c.touched = true;
    delete serverCondErrors[c.key];
  }

  // --- Выбор списка (поверх диалога правила) ---
  // Ключ условия, для которого открыт выбор; null — диалог выбора закрыт
  let pickerKey = $state<number | null>(null);
  const pickerCond = $derived(conds.find((c) => c.key === pickerKey) ?? null);

  function onPickLists(refs: ListRef[]) {
    const c = pickerCond;
    pickerKey = null;
    if (!c) return;
    c.lists = refs.map((r) => ({ ...r }));
    c.touched = true;
    saveError = '';
    delete serverCondErrors[c.key];
  }

  function onExpert() {
    if (!expert) seg = 'conds';
    rawError = '';
  }

  const targetOptions = $derived([
    ...groups.map((g) => ({ value: `group:${g.id}`, label: g.name })),
    { value: 'direct', label: 'DIRECT' },
    { value: 'reject', label: 'REJECT' }
  ]);

  const kernelOptions = [
    { value: 'xray', label: 'Xray' },
    { value: 'mihomo', label: 'Mihomo' }
  ];

  const segItems = $derived([
    { value: 'conds', label: $t('cfg.rules.seg_conds') },
    { value: 'raw', label: $t('cfg.rules.seg_raw') }
  ]);

  function typeLabel(type: string): string {
    const base = $t(condTypeKey(type));
    return type === 'bittorrent' ? `${base} (${$t('cfg.rules.only_xray')})` : base;
  }

  function capitalize(s: string): string {
    return s === '' ? s : s.charAt(0).toUpperCase() + s.slice(1);
  }

  /** Текст ошибки условия по причине из ответа сервера. */
  function condReasonText(reason: string): string {
    switch (reason) {
      case 'domain_invalid':
      case 'regexp_invalid':
        return $t('cfg.rules.err_domain');
      case 'port_invalid':
        return $t('cfg.rules.err_port');
      case 'values_required':
      case 'lists_required':
        return $t('cfg.rules.err_values');
      case 'network_invalid':
        return $t('cfg.rules.network_min');
      default: {
        const key = `cfg.routing.reason.${reason}`;
        const text = $t(key);
        return text === key ? reason : `${capitalize(text)}.`;
      }
    }
  }

  function rawReasonText(): string {
    return $t(rawKernel === 'xray' ? 'cfg.rules.raw_reason_xray' : 'cfg.rules.raw_reason_mihomo');
  }

  /** Раскладывает ошибку сохранения: у условия, у своего правила или общим текстом. */
  function showError(e: unknown) {
    const err = e as { code?: string; params?: Record<string, string> };
    if (err?.code === 'draft_conflict') {
      handleLayerError(e);
      return;
    }
    if (err?.code === 'invalid_input' && err.params?.field) {
      const field = err.params.field;
      const reason = err.params.reason ?? '';
      const m = /^conds\.(\d+)\./.exec(field);
      if (m) {
        const c = conds[Number(m[1])];
        if (c) {
          c.touched = true;
          serverCondErrors[c.key] = condReasonText(reason);
          return;
        }
      }
      if (field === 'raw' && reason === 'raw_invalid') {
        rawError = $t('cfg.rules.raw_invalid', { reason: rawReasonText() });
        return;
      }
    }
    saveError = routingErrorText($t, err?.code, err?.params);
  }

  function close() {
    // Пока идёт запись, диалог не закрывается: ответ определит, сохранено ли правило
    if (saving) return;
    onclose();
  }

  function buildConds() {
    return conds.map((c) => {
      let values: string[] = [];
      if (TEXT_TYPES.has(c.type)) values = parseValues(c.text);
      else if (c.type === 'network')
        values = [c.tcp ? 'tcp' : '', c.udp ? 'udp' : ''].filter(Boolean);
      return { type: c.type, values, lists: LIST_TYPES.has(c.type) ? c.lists : [] };
    });
  }

  async function save() {
    if (!canSave) return;
    saving = true;
    saveError = '';
    rawError = '';
    serverCondErrors = {};
    const raw: RawRule | null = rawMode ? { kernel: rawKernel, text: rawText.trim() } : null;
    try {
      await saveRule({
        id: rule?.id ?? '',
        enabled: rule?.enabled ?? true,
        origin: rule?.origin ?? '',
        conds: raw ? [] : buildConds(),
        target: targetOf(targetSel),
        raw
      });
      saving = false;
      onclose();
    } catch (e) {
      showError(e);
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t(rule === null ? 'cfg.rules.modal_new' : 'cfg.rules.modal_edit')}
  maxWidth="720px"
  dataTestid="rule-modal"
  onclose={close}
>
  <div class="re-body">
    {#each missingLists as issue, i (i)}
      <div class="alert alert-error re-alert" role="alert" data-testid="rule-list-missing">
        <Icon name="warning" size={16} class="re-alert-icon" />
        <span>
          {$t('cfg.rules.list_missing', {
            file: issue.params?.file ?? '?',
            tag: issue.params?.tag ?? '?'
          })}
        </span>
      </div>
    {/each}

    <div class="re-expert">
      <label class="re-expert-label">
        <span class="toggle-switch">
          <input
            type="checkbox"
            bind:checked={expert}
            disabled={saving}
            data-testid="rule-expert"
            onchange={onExpert}
          />
          <span class="toggle-slider"></span>
        </span>
        <span>{$t('cfg.rules.expert')}</span>
      </label>
      {#if expert}
        <SegmentedControl
          items={segItems}
          bind:value={seg}
          ariaLabel={$t('cfg.rules.seg_aria')}
          onchange={() => (rawError = '')}
        />
      {/if}
    </div>

    {#if rawMode}
      <section class="re-section" data-testid="rule-raw">
        <h3 class="re-title">{$t('cfg.rules.raw_title')}</h3>
        <div class="form-group">
          <label class="form-label" for="{uid}-raw-kernel">{$t('cfg.rules.raw_kernel')}</label>
          <Select
            id="{uid}-raw-kernel"
            bind:value={rawKernel}
            options={kernelOptions}
            disabled={saving}
            data-testid="rule-raw-kernel"
            onchange={() => (rawError = '')}
          />
        </div>
        <div class="form-group">
          <label class="form-label" for="{uid}-raw-text">{$t('cfg.rules.raw_rule')}</label>
          <textarea
            id="{uid}-raw-text"
            class="input re-text"
            class:input-error={!!rawError}
            rows="4"
            spellcheck="false"
            autocomplete="off"
            autocapitalize="off"
            placeholder={rawKernel === 'xray'
              ? '{"domain": ["example.com"]}'
              : 'PROCESS-NAME,example'}
            disabled={saving}
            aria-invalid={rawError ? 'true' : 'false'}
            aria-describedby={rawError ? `${uid}-raw-error` : undefined}
            data-testid="rule-raw-text"
            bind:value={rawText}
            oninput={() => {
              rawError = '';
              saveError = '';
            }}></textarea>
          {#if rawError}
            <span class="error-text re-error" id="{uid}-raw-error" data-testid="rule-raw-error">
              {rawError}
            </span>
          {/if}
          <p class="re-hint">{$t('cfg.rules.raw_hint')}</p>
        </div>
      </section>
    {:else}
      <section class="re-section" data-testid="rule-conds-block">
        <h3 class="re-title">{$t('cfg.rules.conds_title')}</h3>
        {#if conds.length === 0}
          <p class="re-error-line" role="alert" data-testid="rule-conds-error">
            {$t('cfg.rules.err_conds')}
          </p>
        {/if}
        {#each conds as c, i (c.key)}
          {@const state = checks[i]}
          {@const serverError = serverCondErrors[c.key]}
          {@const shownError = serverError || state.error}
          {#if i > 0}
            <div class="re-and" aria-hidden="true">{$t('cfg.rules.and')}</div>
          {/if}
          <div class="card re-cond" data-testid="rule-cond-{i}">
            <div class="re-cond-head">
              <div class="re-cond-type">
                <label class="sr-only" for="{uid}-type-{c.key}">{$t('cfg.rules.cond_type')}</label>
                <Select
                  id="{uid}-type-{c.key}"
                  value={c.type}
                  options={typeOptions[i]}
                  disabled={saving}
                  data-testid="rule-cond-type-{i}"
                  onchange={(e) => setType(c, e.currentTarget.value)}
                />
              </div>
              <button
                type="button"
                class="btn btn-secondary btn-sm re-remove"
                title={$t('cfg.rules.remove_cond')}
                aria-label={$t('cfg.rules.remove_cond_aria', { n: i + 1 })}
                disabled={saving}
                data-testid="rule-cond-remove-{i}"
                onclick={() => removeCond(i)}
              >
                <Icon name="delete" size={14} />
              </button>
            </div>

            {#if c.type === 'domain' || c.type === 'ip' || c.type === 'src_ip' || c.type === 'port'}
              {@const label =
                c.type === 'domain'
                  ? 'cfg.rules.domains_label'
                  : c.type === 'ip'
                    ? 'cfg.rules.ip_label'
                    : c.type === 'src_ip'
                      ? 'cfg.rules.src_ip_label'
                      : 'cfg.rules.ports_label'}
              {@const placeholder =
                c.type === 'domain'
                  ? 'cfg.rules.domains_placeholder'
                  : c.type === 'port'
                    ? 'cfg.rules.ports_placeholder'
                    : 'cfg.rules.ip_placeholder'}
              <div class="form-group">
                <label class="form-label" for="{uid}-values-{c.key}">{$t(label)}</label>
                <textarea
                  id="{uid}-values-{c.key}"
                  class="input re-text"
                  class:input-error={!!shownError}
                  rows={c.type === 'port' ? 2 : 4}
                  spellcheck="false"
                  autocomplete="off"
                  autocapitalize="off"
                  placeholder={$t(placeholder)}
                  disabled={saving}
                  aria-invalid={shownError ? 'true' : 'false'}
                  aria-describedby={shownError ? `${uid}-err-${c.key}` : undefined}
                  data-testid="rule-cond-values-{i}"
                  bind:value={c.text}
                  oninput={() => onValuesInput(c)}
                  onblur={() => (c.touched = true)}></textarea>
                {#if c.type === 'domain'}
                  <p class="re-hint">{$t('cfg.rules.domains_hint')}</p>
                {/if}
              </div>
            {:else if c.type === 'network'}
              <div class="re-net" role="group" aria-label={$t('cfg.rules.network_label')}>
                <label class="re-check">
                  <input
                    type="checkbox"
                    bind:checked={c.tcp}
                    disabled={saving}
                    data-testid="rule-net-tcp-{i}"
                  />
                  <span>{$t('cfg.rules.network_tcp')}</span>
                </label>
                <label class="re-check">
                  <input
                    type="checkbox"
                    bind:checked={c.udp}
                    disabled={saving}
                    data-testid="rule-net-udp-{i}"
                  />
                  <span>{$t('cfg.rules.network_udp')}</span>
                </label>
              </div>
            {:else if c.type === 'bittorrent'}
              <p class="re-hint">{$t('cfg.rules.bittorrent_hint')}</p>
            {:else}
              <div class="re-lists" data-testid="rule-cond-lists-{i}">
                {#each c.lists as list, li (`${list.file}:${list.tag}`)}
                  <span class="chip chip-default re-list-chip">
                    <span class="re-mono">{list.file} · {list.tag}</span>
                    <button
                      type="button"
                      class="re-list-remove"
                      aria-label={$t('cfg.rules.remove_list', {
                        name: `${list.file} · ${list.tag}`
                      })}
                      disabled={saving}
                      data-testid="rule-list-remove-{i}-{li}"
                      onclick={() => removeList(c, li)}
                    >
                      <Icon name="close" size={12} />
                    </button>
                  </span>
                {/each}
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  disabled={saving}
                  data-testid="rule-cond-pick-{i}"
                  onclick={() => (pickerKey = c.key)}
                >
                  <Icon name="add" size={14} />
                  {$t('cfg.rules.pick_list')}
                </button>
              </div>
            {/if}

            {#if shownError}
              <span
                class="error-text re-error"
                id="{uid}-err-{c.key}"
                role="alert"
                data-testid="rule-cond-error-{i}"
              >
                {shownError}
              </span>
            {/if}
            {#if dups[i]}
              <p class="re-dup" data-testid="rule-cond-dup-{i}">
                <Icon name="info" size={14} />
                <span>
                  {$t('cfg.rules.dup_hint', { n: dups[i].n, target: dups[i].target })}
                </span>
              </p>
            {/if}
            {#if TEXT_TYPES.has(c.type)}
              <p class="re-hint">{$t('cfg.rules.cond_hint')}</p>
            {/if}
          </div>
        {/each}
        <div>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            disabled={saving || freeType === undefined}
            data-testid="rule-add-cond"
            onclick={addCond}
          >
            <Icon name="add" size={14} />
            {$t('cfg.rules.add_cond')}
          </button>
          {#if conds.length >= 2}
            <p class="re-hint" data-testid="rule-cond-field-hint">
              {$t('cfg.rules.cond_field_hint')}
            </p>
          {/if}
        </div>
      </section>
    {/if}

    <section class="re-section">
      <h3 class="re-title">{$t('cfg.rules.target_title')}</h3>
      <div class="form-group">
        <label class="form-label" for="{uid}-target">{$t('cfg.rules.target_label')}</label>
        <Select
          id="{uid}-target"
          bind:value={targetSel}
          options={targetOptions}
          disabled={saving}
          data-testid="rule-target"
        />
      </div>
    </section>

    {#if saveError}
      <div class="alert alert-error re-alert" role="alert" data-testid="rule-save-error">
        <Icon name="warning" size={16} class="re-alert-icon" />
        <span>{saveError}</span>
      </div>
    {/if}

    <div class="re-footer">
      <Button
        variant="primary"
        disabled={!canSave}
        loading={saving}
        data-testid="rule-save"
        onclick={save}
      >
        {$t('cfg.nodes.save_draft')}
      </Button>
      <Button variant="secondary" disabled={saving} data-testid="rule-cancel" onclick={close}>
        {$t('cfg.nodes.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<ListPickerModal
  open={pickerKey !== null}
  kind={pickerCond?.type === 'geoip' ? 'ip' : 'site'}
  selected={pickerCond?.lists ?? []}
  onpick={onPickLists}
  onclose={() => (pickerKey = null)}
/>

<style>
  .re-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .re-body :global(.form-group) {
    margin-bottom: 0;
  }

  .re-alert {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .re-alert :global(.re-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .re-expert {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-3) var(--spacing-4);
  }

  .re-expert-label {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    cursor: pointer;
  }

  .re-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .re-title {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .re-cond {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
    padding: var(--spacing-3);
    border: 1px solid var(--border);
    box-shadow: none;
  }

  .re-cond-head {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .re-cond-type {
    flex: 1 1 auto;
    min-width: 0;
  }

  .re-remove {
    flex-shrink: 0;
    min-width: 32px;
    min-height: 32px;
    padding: 0 var(--spacing-2);
    justify-content: center;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .re-remove {
      min-width: 44px;
      min-height: 44px;
    }

    .re-footer :global(.btn) {
      min-height: 44px;
    }
  }

  .re-and {
    align-self: flex-start;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .re-text {
    width: 100%;
    height: auto;
    padding: var(--spacing-2) var(--spacing-3);
    min-height: 0;
    resize: vertical;
    overflow-y: auto;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    overflow-wrap: anywhere;
    word-break: break-word;
    white-space: pre-wrap;
  }

  .re-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .re-hint {
    margin: var(--spacing-1) 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .re-error {
    display: block;
    overflow-wrap: anywhere;
  }

  .re-error-line {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--danger);
  }

  .re-dup {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .re-dup :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .re-net {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-4);
  }

  .re-check {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    min-height: 32px;
    cursor: pointer;
  }

  .re-lists {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
  }

  .re-list-chip {
    white-space: normal;
    max-width: 100%;
  }

  .re-list-remove {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 24px;
    min-height: 24px;
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    border-radius: var(--radius-sm, 4px);
  }

  .re-list-remove:disabled {
    cursor: not-allowed;
  }

  .re-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
    padding-top: var(--spacing-2);
  }

  @media (max-width: 480px) {
    .re-footer {
      flex-direction: column;
    }

    .re-footer :global(.btn) {
      width: 100%;
    }
  }
</style>
