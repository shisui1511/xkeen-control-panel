<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    draftChanges,
    handleLayerError,
    refetchLayerState,
    type LayerApply
  } from '../../../lib/configLayer';
  import {
    coresErrorText,
    fetchTakePreview,
    setAccessLog,
    takeSections,
    type CoreKernel,
    type SectionPreview,
    type TakePreview
  } from '../../../lib/configCores';
  import { accessLogFailure } from '../../../lib/accessLog';
  import { runningKernels } from '../../../stores';
  import { coresBusy } from './coresOwner';

  // Диалог взятия секции или всех секций ядра (SEC-01, XCFG-03, D-01…D-04, D-12, D-25).
  // Показывает, какие файлы уйдут в резервные копии, какие останутся на месте, какие секции
  // не будут взяты и почему, и только по явному «Взять секцию» пишет желание в черновик.
  // При комментариях JSONC нужно согласие на их потерю (D-02). До подтверждения диалог
  // ничего не пишет ни в черновик, ни на диск. С then (секция «Лог» Xray со страницы «Логи»,
  // D-14) подтверждение берёт секцию и включает или выключает access-лог одним применением;
  // при открытом черновике вместо тела показывается блокировка: секция меняет владельца
  // только отдельным шагом.
  let {
    open = $bindable(false),
    kernel,
    mode,
    section = '',
    ids,
    forceGroup = '',
    then,
    ontaken
  }: {
    open?: boolean;
    kernel: CoreKernel;
    mode: 'one' | 'all';
    /** Секция для режима one. */
    section?: string;
    /** Секции ядра для режима all (все, что показывает вкладка). */
    ids: string[];
    /** Группа, в которую панель направит входящие force-proxy. */
    forceGroup?: string;
    /** Действие после взятия секции «Лог»: включить или выключить access-лог. */
    then?: 'enable' | 'disable';
    /** Секции взяты; в режиме then вторым аргументом приходит результат применения. */
    ontaken: (ids: string[], applied?: LayerApply) => void | Promise<void>;
  } = $props();

  // Разделитель, по которому текст со списком файлов режется на часть до и после списка
  const FILES_TOKEN = '\u0001files\u0001';

  let preview = $state<TakePreview | null>(null);
  let loading = $state(false);
  let failed = $state(false);
  let busy = $state(false);
  let ack = $state(false);
  let takeError = $state('');
  // Отказ применения в режиме then: текст до и после вывода ядра и сам вывод (моноширинный блок)
  let takeFailure = $state<{ before: string; after: string; output: string } | null>(null);
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  const wanted = $derived(mode === 'one' ? [section] : ids);

  async function load() {
    const mine = ++seq;
    loading = true;
    failed = false;
    preview = null;
    ack = false;
    takeError = '';
    takeFailure = null;
    try {
      const data = await fetchTakePreview(kernel, wanted);
      if (mine === seq) preview = data;
    } catch {
      if (mine === seq) failed = true;
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function reset() {
    seq++;
    preview = null;
    loading = false;
    failed = false;
    busy = false;
    ack = false;
    takeError = '';
    takeFailure = null;
  }

  $effect(() => {
    if (open) untrack(load);
    else untrack(reset);
  });

  const rows = $derived<SectionPreview[]>(preview?.sections ?? []);
  const takeable = $derived(rows.filter((r) => !r.blocked));
  const blockedRows = $derived(rows.filter((r) => !!r.blocked));
  const moveRows = $derived(takeable.filter((r) => r.move.length > 0));
  const noFileRows = $derived(takeable.filter((r) => r.preset && r.move.length === 0));
  const leftFiles = $derived([...new Set(takeable.flatMap((r) => r.left))]);
  const commentFiles = $derived([...new Set(takeable.flatMap((r) => r.comments))]);
  const needAck = $derived(commentFiles.length > 0);
  const forceProxy = $derived(
    kernel === 'xray' &&
      takeable.some((r) => r.id === 'inbounds') &&
      (preview?.force_proxy_tags?.length ?? 0) > 0
  );
  // Открытый черновик не даёт взять секцию «Лог» отдельным шагом (D-14)
  const draftBlock = $derived(then !== undefined && $draftChanges > 0);
  const xrayRunning = $derived($runningKernels.includes('xray'));
  const canConfirm = $derived(
    preview !== null && takeable.length > 0 && (!needAck || ack) && !busy && !draftBlock
  );

  function sectionName(id: string): string {
    return $t(`cfg.sec.title.${kernel}.${id}`);
  }

  function kernelName(): string {
    return kernel === 'xray' ? 'Xray' : 'Mihomo';
  }

  /** Текст до и после списка файлов; сам список выводится отдельно, моноширинным. */
  function around(key: string): [string, string] {
    const [before, after = ''] = $t(key, { files: FILES_TOKEN }).split(FILES_TOKEN);
    return [before, after];
  }

  const filesParts = $derived(around('cfg.sec.take_files'));
  const leftParts = $derived(around('cfg.sec.take_left'));
  const commentParts = $derived(around('cfg.sec.comments_warn'));
  // В режиме all список файлов идёт по секциям: вводная часть и хвост предложения отдельно
  const allIntro = $derived(filesParts[0].trimEnd());
  const allTail = $derived(filesParts[1].replace(/^[.\s]+/, ''));

  const title = $derived(
    then === 'enable'
      ? $t('xlog.take_title_on')
      : then === 'disable'
        ? $t('xlog.take_title_off')
        : mode === 'one'
          ? $t('cfg.sec.take_title_one', { name: sectionName(section) })
          : $t('cfg.sec.take_title_all', { kernel: kernelName() })
  );
  const confirmLabel = $derived(
    then === 'enable'
      ? $t('xlog.take_on')
      : then === 'disable'
        ? $t('xlog.take_off')
        : $t('cfg.sec.take_confirm')
  );

  function blockedReason(r: SectionPreview): string {
    return $t(`cfg.sec.blocked.${r.blocked}`, { file: r.blocked_file ?? '?' });
  }

  function close() {
    // Пока идёт запрос, диалог не закрывается: ответ определит, что записано в черновик
    if (busy) return;
    open = false;
  }

  function errorText(err: unknown): string {
    const e = err as { code?: string; params?: Record<string, string> } | null;
    if (e?.code === 'section_takeover_failed' || e?.code === 'take_changed') {
      return $t('cfg.sec.take_changed', { file: e.params?.file ?? '?' });
    }
    return coresErrorText($t, e?.code, e?.params);
  }

  /** Режим then: берёт секцию и применяет log.access; отказ ядра остаётся в диалоге. */
  async function confirmThen(enabled: boolean) {
    busy = true;
    coresBusy.set(true);
    takeError = '';
    takeFailure = null;
    let view: LayerApply;
    try {
      view = await setAccessLog(enabled, ack);
    } catch (err) {
      takeError = errorText(err);
      return;
    } finally {
      busy = false;
      coresBusy.set(false);
    }
    await refetchLayerState().catch(() => {});
    const failure = accessLogFailure(view);
    if (failure) {
      const [before, after = ''] = $t(
        failure.kind === 'test' ? 'xlog.test_failed' : 'apply.restart_failed',
        { reason: FILES_TOKEN }
      ).split(FILES_TOKEN);
      takeFailure = { before, after, output: failure.reason };
      return;
    }
    open = false;
    await ontaken(['log'], view);
  }

  async function confirmTake() {
    if (!canConfirm) return;
    if (then !== undefined) {
      await confirmThen(then === 'enable');
      return;
    }
    const taking = takeable.map((r) => r.id);
    busy = true;
    coresBusy.set(true);
    takeError = '';
    try {
      await takeSections(kernel, taking, ack);
    } catch (err) {
      if ((err as { code?: string } | null)?.code === 'draft_conflict') {
        // Устаревшая ревизия: тост и перечитывание, диалог закрывается
        handleLayerError(err);
        busy = false;
        open = false;
      } else {
        takeError = errorText(err);
      }
      return;
    } finally {
      busy = false;
      coresBusy.set(false);
    }
    open = false;
    ontaken(taking);
  }
</script>

<Modal
  {open}
  {title}
  maxWidth="640px"
  testid={then === undefined ? 'take-section-modal' : 'xlog-take-modal'}
  onclose={close}
>
  <div class="ts-body" data-mode={mode}>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="ts-scroll"
      role="region"
      aria-label={title}
      tabindex="0"
      data-testid="take-section-content"
    >
      {#if draftBlock}
        <div class="alert alert-warning ts-alert" role="alert" data-testid="xlog-draft-block">
          <Icon name="warning" size={16} class="ts-alert-icon" />
          <span>{$t('xlog.draft_block')}</span>
        </div>
        <a
          class="ts-link"
          href="#/config"
          data-testid="xlog-open-config"
          onclick={() => (open = false)}
        >
          {$t('xlog.open_config')}
        </a>
      {:else if loading}
        <div class="ts-skeleton" aria-hidden="true" data-testid="take-section-loading">
          <Skeleton type="rect" height="20px" />
          <Skeleton type="rect" height="20px" />
          <Skeleton type="rect" height="20px" />
        </div>
      {:else if failed}
        <div class="alert alert-error ts-alert" role="alert" data-testid="take-section-error">
          <Icon name="warning" size={16} class="ts-alert-icon" />
          <span>{$t('cfg.load_error_body')}</span>
        </div>
        <div class="ts-retry">
          <Button variant="secondary" data-testid="take-section-retry" onclick={() => void load()}>
            {$t('cfg.load_error_retry')}
          </Button>
        </div>
      {:else if preview}
        {#if moveRows.length > 0}
          {#if mode === 'one'}
            <p class="ts-text" data-testid="take-section-files">
              {filesParts[0]}<span class="ts-files">{moveRows[0].move.join(', ')}</span
              >{filesParts[1]}
            </p>
          {:else}
            <div class="ts-group" data-testid="take-section-files">
              <p class="ts-text">{allIntro}</p>
              <ul class="ts-list">
                {#each moveRows as r (r.id)}
                  <li class="ts-item" data-section={r.id}>
                    <span class="ts-label">{sectionName(r.id)}</span>
                    <span class="ts-files">{r.move.join(', ')}</span>
                  </li>
                {/each}
              </ul>
              <p class="ts-text">{allTail}</p>
            </div>
          {/if}
        {/if}
        {#if leftFiles.length > 0}
          <p class="ts-text" data-testid="take-section-left">
            {leftParts[0]}<span class="ts-files">{leftFiles.join(', ')}</span>{leftParts[1]}
          </p>
        {/if}
        {#each noFileRows as r (r.id)}
          <p class="ts-text" data-testid="take-section-nofile">
            {#if mode === 'all'}
              <span class="ts-label">{sectionName(r.id)}</span>
            {/if}
            {$t('cfg.sec.take_no_file')}
          </p>
        {/each}
        {#each blockedRows as r (r.id)}
          <p class="ts-text ts-blocked" data-testid="take-section-blocked-{r.id}">
            {#if mode === 'all'}
              <span class="ts-label">{sectionName(r.id)}</span>
            {/if}
            {$t('cfg.sec.take_blocked', { reason: blockedReason(r) })}
          </p>
        {/each}

        {#if needAck}
          <div class="alert alert-warning ts-alert" data-testid="take-section-comments">
            <Icon name="warning" size={16} class="ts-alert-icon" />
            <span>
              {commentParts[0]}<span class="ts-files">{commentFiles.join(', ')}</span
              >{commentParts[1]}
            </span>
          </div>
          <label class="ts-ack">
            <input
              type="checkbox"
              bind:checked={ack}
              disabled={busy}
              data-testid="take-section-comments-ack"
            />
            <span>{$t('cfg.sec.comments_ack')}</span>
          </label>
        {/if}

        {#if forceProxy}
          <div class="alert alert-warning ts-alert" data-testid="take-section-force-proxy">
            <Icon name="warning" size={16} class="ts-alert-icon" />
            <span>{$t('cfg.sec.take_force_proxy', { group: forceGroup })}</span>
          </div>
        {/if}

        {#if then === undefined}
          <div class="alert alert-warning ts-alert" data-testid="take-section-consequence">
            <Icon name="warning" size={16} class="ts-alert-icon" />
            <span>{$t('cfg.sec.take_consequence')}</span>
          </div>
        {:else if xrayRunning}
          <div class="alert alert-warning ts-alert" data-testid="xlog-take-restart">
            <Icon name="warning" size={16} class="ts-alert-icon" />
            <span>{$t('xlog.take_restart_note')}</span>
          </div>
        {/if}
      {/if}

      {#if takeFailure}
        <div class="alert alert-error ts-alert" role="alert" data-testid="xlog-take-failure">
          <Icon name="warning" size={16} class="ts-alert-icon" />
          <div class="ts-fail">
            <span>{takeFailure.before}</span>
            <pre class="ts-pre font-mono" data-testid="xlog-take-output">{takeFailure.output}</pre>
            <span>{takeFailure.after}</span>
          </div>
        </div>
      {/if}

      {#if takeError !== ''}
        <div class="alert alert-error ts-alert" role="alert" data-testid="take-section-take-error">
          <Icon name="warning" size={16} class="ts-alert-icon" />
          <span>{takeError}</span>
        </div>
      {/if}
    </div>

    <div class="ts-footer">
      <Button
        variant="primary"
        disabled={!canConfirm}
        loading={busy}
        data-testid="take-section-confirm"
        onclick={confirmTake}
      >
        {confirmLabel}
      </Button>
      <Button variant="secondary" disabled={busy} data-testid="take-section-cancel" onclick={close}>
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .ts-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  /* Длинный список файлов прокручивается в теле диалога, кнопки остаются на виду */
  .ts-scroll {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
    max-height: 50dvh;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .ts-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .ts-group {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .ts-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .ts-item {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .ts-text {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .ts-label {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .ts-files {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    font-weight: 400;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .ts-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .ts-alert :global(.ts-alert-icon) {
    flex-shrink: 0;
    margin-top: var(--spacing-0-5);
  }

  .ts-ack {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    min-height: 44px;
    font-size: var(--font-size-base);
    color: var(--fg-primary);
    cursor: pointer;
  }

  .ts-ack input {
    flex-shrink: 0;
    width: 18px;
    height: 18px;
    accent-color: var(--accent);
  }

  .ts-fail {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  /* Вывод ядра: ограниченная высота и прокрутка, длинные строки переносятся */
  .ts-pre {
    margin: 0;
    max-height: 160px;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: var(--font-size-xs);
    scrollbar-width: thin;
  }

  .ts-link {
    align-self: flex-start;
    color: var(--accent);
    text-decoration: underline;
  }

  .ts-retry {
    display: flex;
  }

  .ts-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .ts-footer :global(.btn) {
      min-height: 44px;
    }

    .ts-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .ts-footer :global(.btn) {
      flex: 1 1 100%;
    }
  }
</style>
