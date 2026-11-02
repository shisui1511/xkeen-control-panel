<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { formatBytes } from '../../../lib/format';
  import { formatDate } from '../../../lib/nodesView';
  import type { ProfileEntry } from '../../../lib/configProfiles';

  // Строка списка профилей Mihomo (UI-SPEC разделы 2 и 3): профиль панели (всегда есть, D-11),
  // обычный config.yaml XKeen и профиль пользователя. Строка только показывает и сообщает о
  // нажатиях: решения принимает карточка и сервер. Каждая блокировка — атрибут disabled плюс
  // постоянная строка рядом (aria-describedby), title лишь дополняет её. У профиля панели нет
  // «Править», «Переименовать» и «Удалить» (D-24).
  interface Props {
    kind: 'panel' | 'user' | 'xkeen';
    /** Имя профиля: у панели xcp-main, у обычного файла config.yaml. */
    name: string;
    entry?: ProfileEntry;
    active: boolean;
    /** Профиль панели: файл xcp-main.yaml есть. */
    panelFile?: boolean;
    /** Профиль панели: Mihomo ведёт панель (тогда доступно «Заполнить из профиля»). */
    panelOwns?: boolean;
    /** Профиль панели: взятие ждёт «Применить», кнопки возврата нет. */
    panelPending?: boolean;
    /** Профиль — цель возврата при отпускании: удалить нельзя (D-12). */
    returnTarget?: boolean;
    /** Идёт любая операция карточки: все действия неактивны. */
    busy: boolean;
    /** У какой кнопки показан признак загрузки: activate, take, delete или пусто. */
    loadingAction?: string;
    /** Открытый черновик блокирует «Сделать активным» (D-08). */
    activateBlocked?: boolean;
    /** Открытый черновик блокирует «Управлять в панели» (возврат идёт применением). */
    takeBlocked?: boolean;
    conflict?: boolean;
    /** id постоянной строки о блокировке черновиком в шапке карточки. */
    hintId?: string;
    busyTitle?: string;
    onactivate?: () => void;
    ontake?: () => void;
    onedit?: () => void;
    oncopy?: () => void;
    /** Переименование: пусто — готово, иначе текст ошибки для поля. */
    onrename?: (to: string) => Promise<string>;
    ondelete?: () => void;
    onfill?: () => void;
  }

  let {
    kind,
    name,
    entry,
    active,
    panelFile = false,
    panelOwns = false,
    panelPending = false,
    returnTarget = false,
    busy,
    loadingAction = '',
    activateBlocked = false,
    takeBlocked = false,
    conflict = false,
    hintId,
    busyTitle,
    onactivate,
    ontake,
    onedit,
    oncopy,
    onrename,
    ondelete,
    onfill
  }: Props = $props();

  const uid = $props.id();
  const prevId = `${uid}-prev`;
  const copyOffId = `${uid}-copy-off`;
  const renameId = `${uid}-rename`;
  const renameErrId = `${uid}-rename-err`;

  const ownerLabel = $derived(
    $t(
      kind === 'panel'
        ? 'cfg.sec.owner_panel'
        : kind === 'xkeen'
          ? 'cfg.sec.owner_xkeen'
          : 'cfg.sec.owner_user'
    )
  );
  const ownerClass = $derived(
    kind === 'panel' ? 'badge-primary' : kind === 'user' ? 'badge-type' : ''
  );
  const meta = $derived(
    entry ? `${formatDate(entry.mtime * 1000)} · ${formatBytes(entry.size)}` : ''
  );
  const conflictTitle = $derived(conflict ? $t('kernel.conflict_blocked') : busyTitle);

  // Переименование: поле вместо имени, Enter подтверждает, Esc отменяет
  let renaming = $state(false);
  let renameTo = $state('');
  let renameError = $state('');
  let renameBusy = $state(false);

  /** Кнопка «Переименовать» этой строки: на неё возвращается фокус после отмены. */
  function focusRenameButton(): void {
    document
      .querySelector<HTMLButtonElement>(`[data-testid="profile-rename-${CSS.escape(name)}"]`)
      ?.focus();
  }

  function startRename(): void {
    renameTo = name;
    renameError = '';
    renaming = true;
  }

  /** Поле переименования получает фокус и выделение при появлении. */
  function focusField(el: HTMLInputElement): void {
    el.focus();
    el.select();
  }

  async function cancelRename(): Promise<void> {
    renaming = false;
    renameError = '';
    await tick();
    focusRenameButton();
  }

  async function submitRename(): Promise<void> {
    const to = renameTo.trim();
    if (to === name) {
      await cancelRename();
      return;
    }
    renameBusy = true;
    const message = (await onrename?.(to)) ?? '';
    renameBusy = false;
    // Успех: строка пересоздастся под новым именем, фокус вернёт карточка
    if (message !== '') renameError = message;
  }
</script>

<li
  class="pr"
  class:is-active={active}
  aria-current={active ? 'true' : undefined}
  data-testid={kind === 'panel' ? 'profile-panel-row' : `profile-row-${name}`}
  data-owner={kind}
  data-active={active ? 'true' : 'false'}
>
  <div class="pr-info">
    <div class="pr-line">
      {#if renaming}
        <label class="sr-only" for={renameId}>{$t('cfg.prof.rename_label')}</label>
        <input
          id={renameId}
          class="input pr-rename"
          {@attach focusField}
          bind:value={renameTo}
          autocomplete="off"
          spellcheck="false"
          disabled={renameBusy}
          aria-invalid={renameError !== '' ? 'true' : 'false'}
          aria-describedby={renameError !== '' ? renameErrId : undefined}
          data-testid="profile-rename-input-{name}"
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              void submitRename();
            } else if (e.key === 'Escape') {
              e.preventDefault();
              void cancelRename();
            }
          }}
        />
      {:else if kind === 'panel'}
        <span class="pr-name pr-name-plain">{$t('cfg.prof.panel_name')}</span>
      {:else}
        <span class="pr-name">{name}</span>
      {/if}
      <span class="pr-badges">
        <span class="badge {ownerClass}">{ownerLabel}</span>
        {#if active}
          <span class="badge badge-success" data-testid="profile-active-{name}">
            {$t('cfg.prof.active')}
          </span>
        {/if}
        {#if kind === 'panel' && panelPending}
          <span class="badge badge-warning">{$t('cfg.sec.owner_pending')}</span>
        {/if}
      </span>
    </div>
    {#if renaming && renameError !== ''}
      <span
        class="error-text pr-error"
        id={renameErrId}
        role="alert"
        data-testid="profile-rename-error-{name}"
      >
        {renameError}
      </span>
    {/if}
    <p class="pr-meta">
      {#if kind === 'panel'}
        {$t(panelFile ? 'cfg.prof.panel_file_on' : 'cfg.prof.panel_file_off')}
      {:else}
        {meta}
      {/if}
    </p>
    {#if kind === 'user' && returnTarget && !active}
      <p class="pr-meta" id={prevId} data-testid="profile-prev-target-{name}">
        {$t('cfg.prof.prev_target')}
      </p>
    {/if}
    {#if kind === 'panel' && !panelFile}
      <p class="pr-meta" id={copyOffId} data-testid="profile-panel-copy-off">
        {$t('cfg.prof.copy_panel_off')}
      </p>
    {/if}
  </div>

  <div class="pr-actions">
    {#if renaming}
      <Button
        variant="secondary"
        class="btn-sm"
        loading={renameBusy}
        data-testid="profile-rename-save-{name}"
        onclick={() => void submitRename()}
      >
        {$t('profiles.save')}
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={renameBusy}
        data-testid="profile-rename-cancel-{name}"
        onclick={() => void cancelRename()}
      >
        {$t('app.cancel')}
      </Button>
    {:else if kind === 'panel'}
      {#if panelOwns}
        <Button
          variant="secondary"
          class="btn-sm"
          disabled={busy}
          title={busyTitle}
          data-testid="profile-panel-fill"
          onclick={onfill}
        >
          {$t('cfg.prof.fill_open')}
        </Button>
      {:else if !panelPending}
        <Button
          variant="secondary"
          class="btn-sm"
          disabled={busy || conflict || takeBlocked}
          loading={loadingAction === 'take'}
          title={conflictTitle}
          ariaDescribedby={takeBlocked ? hintId : undefined}
          data-testid="profile-panel-take"
          onclick={ontake}
        >
          {$t('cfg.sec.take_btn')}
        </Button>
      {/if}
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={busy || !panelFile}
        title={busyTitle}
        ariaDescribedby={!panelFile ? copyOffId : undefined}
        data-testid="profile-panel-copy"
        onclick={oncopy}
      >
        <Icon name="copy" size={14} />
        {$t('cfg.prof.copy')}
      </Button>
    {:else if kind === 'xkeen'}
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={busy}
        title={busyTitle}
        data-testid="profile-edit-{name}"
        onclick={onedit}
      >
        <Icon name="edit" size={14} />
        {$t('profiles.edit')}
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={busy}
        title={busyTitle}
        data-testid="profile-copy-{name}"
        onclick={oncopy}
      >
        <Icon name="copy" size={14} />
        {$t('cfg.prof.copy')}
      </Button>
    {:else}
      {#if !active}
        <Button
          variant="secondary"
          class="btn-sm pr-activate"
          disabled={busy || conflict || activateBlocked}
          loading={loadingAction === 'activate'}
          title={conflictTitle}
          ariaDescribedby={activateBlocked ? hintId : undefined}
          data-testid="profile-activate-{name}"
          onclick={onactivate}
        >
          {$t('cfg.prof.activate')}
        </Button>
      {/if}
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={busy}
        title={busyTitle}
        data-testid="profile-edit-{name}"
        onclick={onedit}
      >
        <Icon name="edit" size={14} />
        {$t('profiles.edit')}
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={busy}
        title={busyTitle}
        data-testid="profile-copy-{name}"
        onclick={oncopy}
      >
        <Icon name="copy" size={14} />
        {$t('cfg.prof.copy')}
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        disabled={busy}
        title={busyTitle}
        data-testid="profile-rename-{name}"
        onclick={startRename}
      >
        {$t('profiles.rename')}
      </Button>
      {#if !active}
        <Button
          variant="secondary"
          class="btn-sm"
          disabled={busy || returnTarget}
          loading={loadingAction === 'delete'}
          title={busyTitle}
          ariaDescribedby={returnTarget ? prevId : undefined}
          data-testid="profile-delete-{name}"
          onclick={ondelete}
        >
          <Icon name="delete" size={14} />
          {$t('profiles.delete')}
        </Button>
      {/if}
    {/if}
  </div>
</li>

<style>
  .pr {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--spacing-3);
    min-width: 0;
    padding: var(--spacing-4);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card-subtle);
  }

  .pr.is-active {
    border-color: var(--accent-border);
  }

  .pr-info {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    flex: 1 1 16rem;
    min-width: 0;
  }

  .pr-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .pr-name {
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .pr-name-plain {
    font-family: inherit;
  }

  .pr-badges {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
  }

  .pr-meta {
    margin: 0;
    font-size: var(--font-size-xs);
    line-height: 1.5;
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .pr-rename {
    flex: 1 1 12rem;
    min-width: 0;
    max-width: 24rem;
    font-family: var(--font-family-mono);
  }

  .pr-error {
    margin-top: 0;
    overflow-wrap: anywhere;
  }

  .pr-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .pr-actions :global(.btn) {
    min-width: 0;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .pr-actions {
      flex: 1 1 100%;
    }

    .pr-actions :global(.btn) {
      flex: 1 1 auto;
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .pr-actions {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .pr-actions :global(.pr-activate) {
      grid-column: 1 / -1;
    }
  }
</style>
