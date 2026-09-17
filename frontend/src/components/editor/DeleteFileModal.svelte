<script lang="ts">
  import Modal from '../Modal.svelte';
  import { t } from '../../i18n';

  interface Props {
    isOpen: boolean;
    filePath: string;
    onConfirm: () => void;
    onClose: () => void;
  }

  let { isOpen, filePath, onConfirm, onClose }: Props = $props();

  let fileName = $derived(filePath ? filePath.split('/').pop() || '' : '');
</script>

<Modal {isOpen} title={`${$t('editor.delete_file')}: ${fileName}`} onclose={onClose}>
  <p style="margin: 0;">
    {$t('editor.delete_confirm_body', { file: fileName })}
  </p>
  <div class="confirm-modal-actions" style="margin-top: 16px;">
    <button onclick={onClose} class="btn btn-secondary">
      {$t('app.cancel')}
    </button>
    <button onclick={onConfirm} class="btn btn-danger">
      {$t('app.delete')}
    </button>
  </div>
</Modal>
