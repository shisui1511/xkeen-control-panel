<script module lang="ts">
  // Черновик строк своих зон переживает перемонтирование при переключении вкладки ядра и
  // фоновое обновление вида, если серверный список зон не менялся. syncedKey — список зон,
  // который строки уже отражают (последняя своя запись или последний список с сервера),
  // syncedOwn — это своя запись, ответа на которую ещё не было; echoKeys — свои более ранние
  // записи, ответ на которые ещё может прийти, пока следующая запись в пути: такой ответ
  // строки не перезаписывает.
  const MAX_ECHO_KEYS = 8;
  const draftState = $state<{
    syncedKey: string;
    syncedOwn: boolean;
    echoKeys: string[];
    rows: string[];
  }>({
    syncedKey: '',
    syncedOwn: false,
    echoKeys: [],
    rows: []
  });
</script>

<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';

  // Локальные зоны перехвата DNS (DNSF-02, D-18): встроенные зоны и служебный домен роутера
  // показываются только для чтения (.badge), свои зоны редактируются построчно с проверкой
  // доменного имени и защитой от повторов.
  let {
    builtin,
    zones,
    readonly = false,
    onchange
  }: {
    /** Встроенные зоны и служебный домен роутера из вида сервера. */
    builtin: string[];
    /** Свои зоны из черновика или сохранённых параметров. */
    zones: string[];
    /** Запрет правок во время транзакции или блокировки карточки. */
    readonly?: boolean;
    /** Запись валидного набора своих зон в черновик секции dns_takeover. */
    onchange: (zones: string[]) => void;
  } = $props();

  const uid = $props.id();

  function normalizeZone(raw: string): string {
    return raw.trim().replace(/\.+$/, '').toLowerCase();
  }

  function validZoneLabel(label: string): boolean {
    if (label.length < 1 || label.length > 63) return false;
    if (label.startsWith('-') || label.endsWith('-')) return false;
    return /^[a-z0-9_-]+$/.test(label);
  }

  /** Проверка доменного имени зоны по тем же правилам, что ValidZone на сервере. */
  function validZone(raw: string): boolean {
    const trimmed = raw.trim();
    if (trimmed === '' || trimmed.startsWith('.') || trimmed.includes('..')) return false;
    const z = normalizeZone(trimmed);
    if (z === '') return false;
    if (/[\s/:@?#[\]\\]/.test(z)) return false;
    let ascii = z;
    if (/[^\x20-\x7e]/.test(z)) {
      try {
        const u = new URL(`http://${z}`);
        if (u.username || u.password || u.port || u.pathname !== '/' || u.search || u.hash) {
          return false;
        }
        ascii = u.hostname.toLowerCase();
      } catch {
        return false;
      }
    }
    if (ascii === '' || ascii.length > 253) return false;
    return ascii.split('.').every(validZoneLabel);
  }

  function isReverseZone(zone: string): boolean {
    const z = normalizeZone(zone);
    return z.endsWith('.in-addr.arpa') || z.endsWith('.ip6.arpa');
  }

  // Обратные зоны частных сетей сворачиваются в одну плашку в позиции первой обратной зоны
  const builtinBadges = $derived.by<Array<{ key: string; text: string; mono: boolean }>>(() => {
    const items: Array<{ key: string; text: string; mono: boolean }> = [];
    let reverseShown = false;
    for (const z of builtin) {
      if (isReverseZone(z)) {
        if (!reverseShown) {
          reverseShown = true;
          items.push({ key: '__reverse__', text: $t('cfg.dns.zones_reverse'), mono: false });
        }
      } else if (z.trim() !== '') {
        items.push({ key: z, text: z, mono: true });
      }
    }
    return items;
  });

  // Встроенные имена после нормализации: словарь для проверки повторов
  const builtinNames = $derived(
    Object.fromEntries(
      builtin.map((b) => normalizeZone(b)).flatMap((b) => (b === '' ? [] : [[b, true]]))
    ) as Record<string, true>
  );

  // Синхронизация с серверным списком зон, когда он действительно изменился извне (другая
  // вкладка, сброс черновика, отказ записи). Эффект зависит только от списка с сервера:
  // собственная запись меняет syncedKey заранее, а ответ на более раннюю свою запись
  // (echoKeys) строки не перезаписывает
  $effect(() => {
    const propKey = JSON.stringify(zones.map((z) => normalizeZone(z)).filter(Boolean));
    const source = zones;
    untrack(() => {
      if (draftState.syncedKey === propKey) {
        draftState.echoKeys = [];
        draftState.syncedOwn = false;
        return;
      }
      if (draftState.echoKeys.includes(propKey)) return;
      draftState.syncedKey = propKey;
      draftState.syncedOwn = false;
      draftState.echoKeys = [];
      draftState.rows = [...source];
    });
  });

  const rowErrors = $derived.by<Array<string | null>>(() => {
    // Повторы среди своих строк: первая строка верна, следующие с тем же именем — ошибка
    const firstIndex: Record<string, number> = {};
    return draftState.rows.map((raw, i) => {
      const norm = normalizeZone(raw);
      if (norm === '') return null;
      if (!validZone(raw)) return $t('cfg.dns.err_zone_format');
      firstIndex[norm] ??= i;
      if (builtinNames[norm] || firstIndex[norm] !== i) {
        return $t('cfg.dns.err_zone_dup', { zone: norm });
      }
      return null;
    });
  });

  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  function clearDebounce(): void {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer);
      debounceTimer = null;
    }
  }

  // Отложенная запись не теряется при уходе со страницы или смене вкладки ядра
  onDestroy(() => {
    if (debounceTimer !== null) commitIfValid();
  });

  // Пустые строки (только что добавленные) в черновик не попадают и записи не держат;
  // строка с ошибкой держит запись целиком, пока ошибка есть
  function commitIfValid(): void {
    clearDebounce();
    if (readonly) return;
    if (rowErrors.some((e) => e !== null)) return;
    const clean = draftState.rows.map((r) => normalizeZone(r)).filter((z) => z !== '');
    const nextKey = JSON.stringify(clean);
    if (nextKey === draftState.syncedKey) return;
    // Прежняя своя запись может быть ещё в пути: её ответ — эхо, а не правка извне. Список с
    // сервера в эхо не попадает: его возврат (отказ записи, сброс) перезаписывает строки
    if (draftState.syncedOwn) {
      draftState.echoKeys = [...draftState.echoKeys, draftState.syncedKey].slice(-MAX_ECHO_KEYS);
    }
    draftState.syncedKey = nextKey;
    draftState.syncedOwn = true;
    onchange(clean);
  }

  function scheduleCommit(): void {
    clearDebounce();
    debounceTimer = setTimeout(() => {
      debounceTimer = null;
      commitIfValid();
    }, 200);
  }

  function handleInput(index: number, value: string): void {
    draftState.rows = draftState.rows.map((r, i) => (i === index ? value : r));
    scheduleCommit();
  }

  async function addRow(): Promise<void> {
    if (readonly) return;
    clearDebounce();
    const nextIndex = draftState.rows.length;
    draftState.rows = [...draftState.rows, ''];
    await tick();
    const el = document.getElementById(`${uid}-zone-${nextIndex}`);
    if (el instanceof HTMLInputElement) el.focus();
  }

  function removeRow(index: number): void {
    if (readonly) return;
    clearDebounce();
    draftState.rows = draftState.rows.filter((_, i) => i !== index);
    commitIfValid();
  }
</script>

<div class="dlz">
  <div class="dlz-group">
    <span class="form-label">{$t('cfg.dns.zones_builtin')}</span>
    <div class="dlz-builtin" data-testid="dns-zones-builtin">
      {#each builtinBadges as item (item.key)}
        <span class="badge dlz-badge" class:dlz-mono={item.mono}>{item.text}</span>
      {/each}
    </div>
  </div>

  <div class="dlz-group">
    <span class="form-label">{$t('cfg.dns.zones_custom')}</span>
    {#if draftState.rows.length === 0}
      <p class="dlz-caption" data-testid="dns-zones-empty">{$t('cfg.dns.zones_empty')}</p>
    {:else}
      <ul class="dlz-list">
        {#each draftState.rows as row, i (i)}
          {@const err = rowErrors[i]}
          {@const inputId = `${uid}-zone-${i}`}
          {@const errId = `${uid}-zone-err-${i}`}
          <li class="dlz-item">
            <div class="dlz-row">
              <label class="sr-only" for={inputId}>
                {$t('cfg.dns.zones_custom')}
                {i + 1}
              </label>
              <input
                id={inputId}
                type="text"
                class="input dlz-input dlz-mono"
                class:input-error={err !== null}
                value={row}
                disabled={readonly}
                spellcheck="false"
                autocomplete="off"
                aria-invalid={err !== null ? 'true' : undefined}
                aria-describedby={err !== null ? errId : undefined}
                data-testid="dns-zone-{i}"
                oninput={(e) => handleInput(i, e.currentTarget.value)}
                onchange={() => commitIfValid()}
              />
              <Button
                variant="secondary"
                class="btn-sm"
                disabled={readonly}
                ariaLabel={row.trim() !== ''
                  ? `${$t('cfg.dns.zone_remove')}: ${row.trim()}`
                  : undefined}
                data-testid="dns-zone-remove-{i}"
                onclick={() => removeRow(i)}
              >
                {$t('cfg.dns.zone_remove')}
              </Button>
            </div>
            {#if err !== null}
              <p
                class="error-text dlz-error"
                id={errId}
                role="alert"
                data-testid="dns-zone-error-{i}"
              >
                {err}
              </p>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    <div class="dlz-add">
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={readonly}
        data-testid="dns-zone-add"
        onclick={() => void addRow()}
      >
        {$t('cfg.dns.zone_add')}
      </Button>
    </div>

    <p class="dlz-caption">{$t('cfg.dns.zones_hint')}</p>
  </div>
</div>

<style>
  .dlz {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .dlz-group {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dlz-builtin {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .dlz-badge {
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .dlz-mono {
    font-family: var(--font-family-mono);
    overflow-wrap: anywhere;
  }

  .dlz-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .dlz-item {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .dlz-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .dlz-row :global(.dlz-input) {
    flex: 1 1 12rem;
    min-width: 0;
  }

  .dlz-add {
    display: flex;
    flex-wrap: wrap;
  }

  .dlz-caption {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .dlz-error {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .dlz :global(.input),
    .dlz :global(.btn) {
      min-height: 44px;
    }
  }
</style>
