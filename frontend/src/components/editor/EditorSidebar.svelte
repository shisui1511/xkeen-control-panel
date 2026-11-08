<script lang="ts">
  import ResponsiveSidebarPanel from '../ResponsiveSidebarPanel.svelte';
  import FileTree from './FileTree.svelte';
  import { t } from '../../i18n';
  import type { ConfigFileInfo, FileListStatus } from './fileOps';

  interface Props {
    open: boolean;
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
    /** Каталог XKeen: только во вкладке «Файлы» раздела (D-04) */
    xkeenFiles?: ConfigFileInfo[];
    /** Исход загрузки списка во вкладке «Файлы» */
    filesStatus?: FileListStatus;
    onRetryFiles?: () => void;
    selectedFile: string;
    /** Путь файла, который сейчас открывается: строка списка показывает занятость */
    loadingPath?: string | null;
    activeKernel?: string;
    /** Узкий экран: панель выезжает листом поверх редактора, а не занимает колонку */
    overlay?: boolean;
    onclose?: () => void;
    onLoadFile: (path: string, isPreviewClick: boolean) => void;
    onCreateFile: () => void;
    onRenameFile: (file: ConfigFileInfo) => void;
    onDuplicateFile: (file: ConfigFileInfo) => void;
    onDownloadFile: (file: ConfigFileInfo) => void;
    onDeleteFile: (file: ConfigFileInfo) => void;
    onViewBackups: (file: ConfigFileInfo) => void;
  }

  let {
    open: show = $bindable(true),
    xrayFiles,
    mihomoFiles,
    xkeenFiles,
    filesStatus = 'ready',
    onRetryFiles,
    selectedFile,
    loadingPath = null,
    activeKernel = '',
    overlay = false,
    onclose: onClose,
    onLoadFile,
    onCreateFile,
    onRenameFile,
    onDuplicateFile,
    onDownloadFile,
    onDeleteFile,
    onViewBackups
  }: Props = $props();
</script>

<ResponsiveSidebarPanel
  bind:open={show}
  {overlay}
  onclose={onClose}
  title={$t('editor.tab_files')}
  storageKey="editor_filetree_width"
  defaultWidth={240}
  minWidth={160}
  maxWidth={450}
>
  <FileTree
    {xrayFiles}
    {mihomoFiles}
    {xkeenFiles}
    status={filesStatus}
    onRetry={onRetryFiles}
    {selectedFile}
    {loadingPath}
    {activeKernel}
    {onLoadFile}
    {onCreateFile}
    {onRenameFile}
    {onDuplicateFile}
    {onDownloadFile}
    {onDeleteFile}
    {onViewBackups}
  />
</ResponsiveSidebarPanel>
