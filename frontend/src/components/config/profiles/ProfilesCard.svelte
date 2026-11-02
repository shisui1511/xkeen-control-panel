<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { t } from '../../../i18n';
  import { editorOpenRequest, isConflict, showConfirm, showToast } from '../../../stores';
  import Button from '../../Button.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, draftChanges, layerSnapshot } from '../../../lib/configLayer';
  import { onCoresChanged } from '../../../lib/configCores';
  import {
    deleteProfile,
    fetchProfiles,
    profileErrorText,
    renameProfile,
    type ProfilesState
  } from '../../../lib/configProfiles';
  import TakeRoutingModal from '../routing/TakeRoutingModal.svelte';
  import { activateOwnProfile, confirmMihomoReturn, routingBusy } from '../routing/routingOwner';
  import FillSectionsModal from './FillSectionsModal.svelte';
  import NewProfileModal from './NewProfileModal.svelte';
  import ProfileRow from './ProfileRow.svelte';

  // Карточка «Профили» во вкладке Mihomo (PART-02, PART-03, D-06…D-12, D-24, D-27): профиль
  // панели (всегда первым), обычный config.yaml XKeen и профили пользователя по алфавиту,
  // «Сделать активным» с подтверждением по переходу, переименование и удаление. Решения
  // принимает сервер: владельца, цель возврата и блокировку черновиком отдаёт список и снимок
  // слоя, карточка их только отражает. Диалоги «Новый профиль» и «Заполнить из профиля»
  // открываются через openNew и openFill.
  // running — Mihomo запущен; panelPrimary — над карточкой плашка с primary «Управлять в
  // панели», тогда «Новый профиль» становится secondary: на первом экране одна акцентная кнопка.
  let { running, panelPrimary }: { running: boolean; panelPrimary: boolean } = $props();

  const uid = $props.id();
  const titleId = `${uid}-title`;
  const hintId = `${uid}-draft-hint`;

  const MIHOMO_DIR = '/opt/etc/mihomo';
  const PANEL_NAME = 'xcp-main';
  const PLAIN_NAME = 'config.yaml';
  const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$/;

  let profiles = $state<ProfilesState | null>(null);
  let loadFailed = $state(false);
  // Ключ идущей операции карточки: activate:<имя>, delete:<имя>
  let busyKey = $state<string | null>(null);
  let takeOpen = $state(false);
  let takeBusy = $state(false);
  let newOpen = $state(false);
  let newMode = $state<'template' | 'copy'>('template');
  let newFrom = $state('');
  let fillOpen = $state(false);

  // Номер запроса: поздний ответ устаревшего чтения не затирает свежий
  let seq = 0;

  async function load(): Promise<void> {
    const mine = ++seq;
    try {
      const next = await fetchProfiles();
      if (mine !== seq) return;
      profiles = next;
      loadFailed = false;
    } catch (e) {
      if (mine !== seq) return;
      if ((e as { status?: number } | null)?.status === 401) return;
      // Прежний список остаётся на экране; ошибка нужна, только пока списка ещё нет
      loadFailed = true;
    }
  }

  function retry(): void {
    loadFailed = false;
    void load();
  }

  onMount(() => {
    void load();
    return onCoresChanged(() => void load());
  });

  // --- Состояние для блокировок (UI-SPEC раздел 3) ---

  const routing = $derived($layerSnapshot?.routing ?? null);
  const panelEntry = $derived(profiles?.profiles.find((p) => p.owner === 'panel'));
  const userProfiles = $derived(profiles?.profiles.filter((p) => p.owner === 'user') ?? []);
  const plainConfig = $derived(profiles?.plain_config ?? null);
  const mihomoTaken = $derived(routing?.mihomo?.taken === true);
  const panelOwns = $derived(mihomoTaken || panelEntry?.active === true);
  // Взятие записано в черновик, файлы ещё не переключены
  const panelPending = $derived(!mihomoTaken && routing?.desired_mihomo === true);
  // Значения секций Mihomo сохранены при прошлом отпускании: «Управлять в панели» — возврат
  const returnMode = $derived(routing?.mihomo_saved === true);
  const draftOpen = $derived($draftChanges > 0);
  // Переход на свой профиль идёт применением через панель (D-08): черновик его блокирует; смена
  // одного своего профиля на другой черновика не касается
  const activateBlocked = $derived(draftOpen && (panelOwns || panelPending));
  const takeBlocked = $derived(draftOpen && returnMode);
  const showDraftHint = $derived(activateBlocked || takeBlocked);
  const working = $derived($routingBusy || $applyRunning || busyKey !== null);
  const busyTitle = $derived($applyRunning ? $t('cfg.routing.apply_busy_hint') : undefined);
  const returnTarget = $derived(profiles?.return_target ?? '');
  // Имя профиля, который останется в списке при возврате Mihomo панели
  const keepName = $derived(profiles?.active || PLAIN_NAME);
  const switchingName = $derived(
    busyKey !== null && busyKey.startsWith('activate:') ? busyKey.slice('activate:'.length) : ''
  );
  const loadingAction = (name: string): string =>
    busyKey === `activate:${name}` ? 'activate' : busyKey === `delete:${name}` ? 'delete' : '';

  // --- Диалоги следующего плана ---

  /** «Новый профиль»: из шаблона или копией профиля from. */
  function openNew(mode: 'template' | 'copy', from = ''): void {
    newMode = mode;
    newFrom = from;
    newOpen = true;
  }

  /**
   * Профиль создан: список из ответа, фокус на новой строке и тост с «Сделать активным».
   * Профиль сам не активируется: действие тоста открывает то же подтверждение, что кнопка строки.
   */
  async function created(name: string, state: ProfilesState): Promise<void> {
    profiles = state;
    loadFailed = false;
    await tick();
    document.querySelector<HTMLElement>(`[data-testid="profile-row-${CSS.escape(name)}"]`)?.focus();
    const tr = $t;
    showToast('success', tr('profiles.created', { name }), 10000, {
      label: tr('cfg.prof.activate'),
      onClick: () => {
        // Переход идёт применением через панель: открытый черновик его блокирует (D-08)
        if (activateBlocked) {
          showToast('warning', tr('cfg.prof.draft_block'), 6000);
          return;
        }
        void activate(name);
      }
    });
  }

  /** «Заполнить из профиля»: значения секций профиля панели из шаблона или профиля. */
  function openFill(): void {
    fillOpen = true;
  }

  // --- Действия ---

  /** «Сделать активным»: подтверждение по переходу (панель → свой, свой → свой), затем запрос. */
  async function activate(name: string): Promise<void> {
    if (working) return;
    const tr = $t;
    const fromPanel = panelOwns;
    const parts: string[] = [];
    let title: string;
    let confirmLabel: string;
    if (fromPanel) {
      title = tr('cfg.prof.to_user_title', { name });
      confirmLabel = tr('cfg.prof.to_user_confirm');
      parts.push(tr('cfg.prof.to_user_body'));
      // Маршрутизацией Xray управляет панель: без Xray или при XKeen фраза была бы неверной
      if (routing?.xray?.taken === true) parts.push(tr('cfg.prof.xray_stays'));
      if (returnTarget === PLAIN_NAME) parts.push(tr('cfg.prof.to_user_adopt'));
    } else {
      title = tr('profiles.activate_confirm_title', { name });
      confirmLabel = tr('profiles.activate');
      parts.push(tr('profiles.activate_confirm_msg'));
      // Обычный config.yaml переезжает в profiles/ внутри активации (D-27)
      if (plainConfig) parts.push(tr('cfg.prof.to_user_adopt'));
    }
    if (!running) parts.push(tr('cfg.prof.confirm_stopped'));
    const ok = await showConfirm({
      variant: 'warning',
      title,
      message: parts.join(' '),
      confirmLabel,
      cancelLabel: tr('app.cancel')
    });
    if (!ok) return;
    busyKey = `activate:${name}`;
    try {
      await activateOwnProfile(name);
    } finally {
      busyKey = null;
      await load();
    }
  }

  async function take(): Promise<void> {
    if (working) return;
    if (!returnMode) {
      takeOpen = true;
      return;
    }
    takeBusy = true;
    try {
      await confirmMihomoReturn(keepName, !running);
    } finally {
      takeBusy = false;
      await load();
    }
  }

  function edit(path: string): void {
    editorOpenRequest.set(`${MIHOMO_DIR}/${path}`);
    window.location.hash = '#/editor';
  }

  /** Переименование: пусто — готово, иначе текст ошибки под полем (D-12, D-24). */
  async function rename(from: string, to: string): Promise<string> {
    const tr = $t;
    if (!NAME_RE.test(to)) return tr('profiles.name_rules');
    if (/^xcp-/i.test(to)) return tr('cfg.prof.err_reserved');
    if (profiles?.profiles.some((p) => p.name === to)) {
      return tr('cfg.prof.err_exists', { name: to });
    }
    try {
      profiles = await renameProfile(from, to);
    } catch (e) {
      return profileErrorText(tr, e, to);
    }
    await tick();
    document
      .querySelector<HTMLButtonElement>(`[data-testid="profile-rename-${CSS.escape(to)}"]`)
      ?.focus();
    return '';
  }

  async function remove(name: string): Promise<void> {
    if (working) return;
    const tr = $t;
    const ok = await showConfirm({
      variant: 'danger',
      title: tr('profiles.delete_confirm_title', { name }),
      message: tr('cfg.prof.delete_body'),
      confirmLabel: tr('cfg.prof.delete_confirm'),
      cancelLabel: tr('app.cancel')
    });
    if (!ok) return;
    busyKey = `delete:${name}`;
    try {
      profiles = await deleteProfile(name);
    } catch (e) {
      showToast('error', `${tr('profiles.error')}: ${profileErrorText(tr, e, name)}`, 8000);
    } finally {
      busyKey = null;
    }
  }
</script>

<section class="card pc" aria-labelledby={titleId} data-testid="profiles-card">
  <div class="pc-head">
    <div class="pc-titles">
      <h3 class="pc-title" id={titleId}>{$t('cfg.prof.title')}</h3>
      <p class="pc-caption">{$t('cfg.prof.subtitle')}</p>
    </div>
    <Button
      variant={panelPrimary ? 'secondary' : 'primary'}
      class="pc-new"
      disabled={profiles === null}
      data-testid="profiles-new"
      onclick={() => openNew('template')}
    >
      {$t('cfg.prof.new_btn')}
    </Button>
  </div>

  {#if !running}
    <p class="pc-status" role="status" data-testid="profiles-kernel-stopped">
      <Icon name="info" size={16} class="pc-status-icon" />
      <span>{$t('cfg.prof.kernel_stopped')}</span>
    </p>
  {/if}
  {#if working}
    <p class="pc-status" role="status" data-testid="profiles-busy">
      <Icon name="info" size={16} class="pc-status-icon" />
      <span>
        {switchingName !== ''
          ? $t('cfg.prof.switching', { name: switchingName })
          : $t('cfg.routing.apply_busy_hint')}
      </span>
    </p>
  {/if}
  {#if $isConflict}
    <p class="pc-status" role="status" data-testid="profiles-conflict">
      <Icon name="warning" size={16} class="pc-status-icon pc-warn" />
      <span>{$t('kernel.conflict_blocked')}</span>
    </p>
  {/if}
  {#if showDraftHint}
    <p class="pc-status pc-warning" id={hintId} role="status" data-testid="profiles-draft-hint">
      <Icon name="warning" size={16} class="pc-status-icon pc-warn" />
      <span>{$t('cfg.prof.draft_block')}</span>
    </p>
  {/if}

  {#if profiles === null && loadFailed}
    <div class="alert alert-error pc-alert" role="alert" data-testid="profiles-load-error">
      <Icon name="warning" size={16} class="pc-status-icon" />
      <span class="pc-alert-text">{$t('cfg.prof.load_error')}</span>
      <Button variant="secondary" class="btn-sm" data-testid="profiles-retry" onclick={retry}>
        {$t('cfg.load_error_retry')}
      </Button>
    </div>
  {:else if profiles === null}
    <div class="pc-skeleton" aria-hidden="true" data-testid="profiles-skeleton">
      <Skeleton type="rect" height="56px" />
      <Skeleton type="rect" height="56px" />
    </div>
  {:else}
    <ul class="pc-list" aria-busy={working} data-testid="profiles-list">
      <ProfileRow
        kind="panel"
        name={PANEL_NAME}
        entry={panelEntry}
        active={panelEntry?.active === true}
        panelFile={panelEntry !== undefined}
        {panelOwns}
        {panelPending}
        busy={working}
        loadingAction={takeBusy && $routingBusy ? 'take' : ''}
        {takeBlocked}
        conflict={$isConflict}
        {hintId}
        {busyTitle}
        ontake={() => void take()}
        oncopy={() => openNew('copy', PANEL_NAME)}
        onfill={openFill}
      />
      {#if plainConfig}
        <ProfileRow
          kind="xkeen"
          name={PLAIN_NAME}
          entry={plainConfig}
          active={plainConfig.active}
          busy={working}
          {busyTitle}
          onedit={() => edit(PLAIN_NAME)}
          oncopy={() => openNew('copy', PLAIN_NAME)}
        />
      {/if}
      {#each userProfiles as p (p.name)}
        <ProfileRow
          kind="user"
          name={p.name}
          entry={p}
          active={p.active}
          returnTarget={returnTarget === p.name}
          busy={working}
          loadingAction={loadingAction(p.name)}
          {activateBlocked}
          conflict={$isConflict}
          {hintId}
          {busyTitle}
          onactivate={() => void activate(p.name)}
          onedit={() => edit(`profiles/${p.name}.yaml`)}
          oncopy={() => openNew('copy', p.name)}
          onrename={(to) => rename(p.name, to)}
          ondelete={() => void remove(p.name)}
        />
      {/each}
    </ul>
    {#if userProfiles.length === 0}
      <div data-testid="profiles-empty">
        <EmptyState
          title={$t('cfg.prof.empty_title')}
          description={$t('cfg.prof.empty_body')}
          plain
        />
      </div>
    {/if}
  {/if}

  <div class="pc-foot">
    <a class="pc-link" href="#/rules?tab=providers" data-testid="profiles-rule-providers-link">
      <span>{$t('cfg.prof.providers_link')}</span>
      <Icon name="arrow-right" size={14} />
    </a>
  </div>
</section>

<NewProfileModal
  bind:open={newOpen}
  startMode={newMode}
  startFrom={newFrom}
  {profiles}
  oncreated={created}
/>

<FillSectionsModal bind:open={fillOpen} profileNames={userProfiles.map((p) => p.name)} />

<TakeRoutingModal open={takeOpen} mode="take" scope="mihomo" onclose={() => (takeOpen = false)} />

<style>
  .pc {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .pc-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .pc-titles {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    flex: 1 1 16rem;
    min-width: 0;
  }

  .pc-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .pc-caption {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .pc-status {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-2);
    margin: 0;
    min-width: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .pc-warning {
    color: var(--warning);
  }

  .pc-status :global(.pc-status-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .pc-status :global(.pc-warn) {
    color: var(--warning);
  }

  .pc-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
  }

  .pc-alert-text {
    flex: 1 1 12rem;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pc-skeleton,
  .pc-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .pc-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .pc-foot {
    display: flex;
    min-width: 0;
  }

  .pc-link {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    min-height: 24px;
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--accent-text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .pc-link:hover {
    text-decoration: underline;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .pc-head :global(.pc-new),
    .pc-alert :global(.btn) {
      flex: 1 1 100%;
      min-height: 44px;
    }

    .pc-link {
      min-height: 44px;
    }
  }
</style>
