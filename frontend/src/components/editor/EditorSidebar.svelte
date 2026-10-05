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
    show: boolean;
    xrayFiles: ConfigFileInfo[];
    mihomoFiles: ConfigFileInfo[];
    selectedFile: string;
    activeKernel?: string;
    /** Узкий экран: панель выезжает листом поверх редактора, а не занимает колонку */
    overlay?: boolean;
    onClose?: () => void;
    onLoadFile: (path: string, isPreviewClick: boolean) => void;
    onCreateFile: () => void;
    onRenameFile: (file: ConfigFileInfo) => void;
    onDuplicateFile: (file: ConfigFileInfo) => void;
    onDownloadFile: (file: ConfigFileInfo) => void;
    onDeleteFile: (file: ConfigFileInfo) => void;
    onViewBackups: (file: ConfigFileInfo) => void;
  }

  let {
    show = $bindable(true),
    xrayFiles,
    mihomoFiles,
    selectedFile,
    activeKernel = '',
    overlay = false,
    onClose,
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
  bind:show
  {overlay}
  {onClose}
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
