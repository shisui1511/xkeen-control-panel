<script lang="ts">
  import Modal from '../Modal.svelte';
  import { t } from '../../i18n';

  interface Props {
    createOpen: boolean;
    renameOpen: boolean;
    initialRenameValue?: string;
    onCreate: (fileName: string) => void;
    onRename: (newName: string) => void;
    onCloseCreate: () => void;
    onCloseRename: () => void;
  }

  let {
    createOpen,
    renameOpen,
    initialRenameValue = '',
    onCreate,
    onRename,
    onCloseCreate,
    onCloseRename
  }: Props = $props();

  let newFileName = $state('');
  let renameTarget = $state('');

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
  <input
    id="new-file-name"
    type="text"
    bind:value={newFileName}
    placeholder={$t('editor.file_name')}
    class="input"
    style="margin-bottom: 16px; width: 100%;"
    onkeydown={(e) => e.key === 'Enter' && handleCreate()}
  />
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
  <input
    id="rename-target"
    type="text"
    bind:value={renameTarget}
    placeholder={$t('editor.new_name')}
    class="input"
    style="margin-bottom: 16px; width: 100%;"
    onkeydown={(e) => e.key === 'Enter' && handleRename()}
  />
  <div class="confirm-modal-actions">
    <button onclick={onCloseRename} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={handleRename} class="btn btn-primary">
      {$t('app.rename')}
    </button>
  </div>
</Modal>
