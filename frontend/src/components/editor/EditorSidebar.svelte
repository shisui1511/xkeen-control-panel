<script lang="ts">
  import ResponsiveSidebarPanel from '../ResponsiveSidebarPanel.svelte';
  import FileTree from './FileTree.svelte';
  import { t } from '../../i18n';

  export interface ConfigFileInfo {
    name: string;
    path: string;
    size: number;
  }

  interface Props {
    open: boolean;
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
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
