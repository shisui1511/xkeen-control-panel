<script lang="ts">
  import { t } from '../../i18n';
  import Badge, { type BadgeTone } from '../Badge.svelte';
  import type { FileOwner } from './fileOps';

  // Владелец файла в списке «Файлов» (D-05): «Панель», «XKeen», «Пользователь» — тот же вид,
  // что у бейджа владельца секции (SectionCard). Отпущенный файл панели — «Пользователь».
  let { owner }: { owner: FileOwner } = $props();

  const TONE: Record<FileOwner, BadgeTone> = {
    panel: 'info',
    xkeen: 'neutral',
    user: 'emphasis'
  };

  const label = $derived(
    owner === 'panel'
      ? $t('cfg.sec.owner_panel')
      : owner === 'xkeen'
        ? $t('cfg.sec.owner_xkeen')
        : $t('cfg.sec.owner_user')
  );
  const aria = $derived($t('cfg.files.owner_aria', { owner: label }));
</script>

<Badge variant="tag" tone={TONE[owner]} testid="files-owner-badge" data-owner={owner} title={aria}>
  <span class="sr-only">{aria}</span>
  <span aria-hidden="true">{label}</span>
</Badge>
