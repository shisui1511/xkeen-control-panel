<script lang="ts">
  import Modal from '../Modal.svelte';
  import { t } from '../../i18n';
  import { isXrayRootPath, matchXKeenStoplist } from '../../lib/xkeenStoplist';

  interface Props {
    createOpen: boolean;
    renameOpen: boolean;
    initialRenameValue?: string;
    /** Каталог, в котором создаётся файл (для подсказки стоп-списка XKeen). */
    createDir?: string;
    /** Каталог переименовываемого файла. */
    renameDir?: string;
    xrayDir: string;
    onCreate: (fileName: string) => void;
    onRename: (newName: string) => void;
    onCloseCreate: () => void;
    onCloseRename: () => void;
  }

  let {
    createOpen,
    renameOpen,
    initialRenameValue = '',
    createDir = '',
    renameDir = '',
    xrayDir,
    onCreate,
    onRename,
    onCloseCreate,
    onCloseRename
  }: Props = $props();

  let newFileName = $state('');
  let renameTarget = $state('');

  // XKeen отменяет запуск Xray из-за таких имён только в корне каталога Xray.
  function stoplistWord(dir: string, name: string): string | null {
    const trimmed = name.trim();
    if (!trimmed || !dir || !isXrayRootPath(`${dir.replace(/\/$/, '')}/${trimmed}`, xrayDir)) {
      return null;
    }
    return matchXKeenStoplist(trimmed);
  }

  let createWord = $derived(stoplistWord(createDir, newFileName));
  let renameWord = $derived(stoplistWord(renameDir, renameTarget));

  $effect(() => {
    if (createOpen) {
      newFileName = '';
    }
  });

  $effect(() => {
    if (renameOpen) {
      renameTarget = initialRenameValue;
    }
  });

  function handleCreate() {
    if (!newFileName.trim()) return;
    onCreate(newFileName.trim());
  }

  function handleRename() {
    if (!renameTarget.trim()) return;
    onRename(renameTarget.trim());
  }
</script>

<Modal isOpen={createOpen} title={$t('editor.create_file')} onclose={onCloseCreate}>
  <label for="new-file-name" class="sr-only">{$t('editor.file_name')}</label>
  <div class="name-field">
    <input
      id="new-file-name"
      type="text"
      bind:value={newFileName}
      placeholder={$t('editor.file_name')}
      class="input"
      style="width: 100%;"
      aria-describedby={createWord ? 'new-file-hint' : undefined}
      onkeydown={(e) => e.key === 'Enter' && handleCreate()}
    />
    {#if createWord}
      <p id="new-file-hint" class="form-hint form-hint--warning" role="status">
        {$t('stoplist.hint', { word: createWord })}
      </p>
    {/if}
  </div>
  <div class="confirm-modal-actions">
    <button onclick={onCloseCreate} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={handleCreate} class="btn btn-primary">
      {$t('app.create')}
    </button>
  </div>
</Modal>

<Modal isOpen={renameOpen} title={$t('editor.rename_file')} onclose={onCloseRename}>
  <label for="rename-target" class="sr-only">{$t('editor.new_name')}</label>
  <div class="name-field">
    <input
      id="rename-target"
      type="text"
      bind:value={renameTarget}
      placeholder={$t('editor.new_name')}
      class="input"
      style="width: 100%;"
      aria-describedby={renameWord ? 'rename-file-hint' : undefined}
      onkeydown={(e) => e.key === 'Enter' && handleRename()}
    />
    {#if renameWord}
      <p id="rename-file-hint" class="form-hint form-hint--warning" role="status">
        {$t('stoplist.hint', { word: renameWord })}
      </p>
    {/if}
  </div>
  <div class="confirm-modal-actions">
    <button onclick={onCloseRename} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={handleRename} class="btn btn-primary">
      {$t('app.rename')}
    </button>
  </div>
</Modal>

<style>
  .name-field {
    margin-bottom: 16px;
  }

  .form-hint {
    margin: 6px 0 0;
    font-size: var(--font-size-xs);
    line-height: 1.4;
    color: var(--fg-secondary);
  }

  .form-hint--warning {
    color: var(--warning);
  }
</style>
