<script lang="ts">
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import SegmentedControl from '../SegmentedControl.svelte';
  import PingTargetQuickMenu from '../PingTargetQuickMenu.svelte';
  import ViewGrid from '../../lib/components/icons/ViewGrid.svelte';
  import ViewList from '../../lib/components/icons/ViewList.svelte';
  import type { ProxiesViewMode } from '../../lib/proxyViewPrefs';

  interface Props {
    filterQuery: string;
    viewMode: ProxiesViewMode;
    loading: boolean;
    onSearchInput?: () => void;
    onExpandAll?: () => void;
    onCollapseAll?: () => void;
    onRefresh?: () => void;
  }

  let {
    filterQuery = $bindable(''),
    viewMode = $bindable('grid'),
    loading = false,
    onSearchInput,
    onExpandAll,
    onCollapseAll,
    onRefresh
  }: Props = $props();
</script>

<div class="proxy-filter-bar">
  <input
    class="group-search"
    type="search"
    bind:value={filterQuery}
    oninput={() => onSearchInput?.()}
    placeholder={$t('proxies.filter_placeholder')}
    aria-label={$t('proxies.filter_placeholder')}
  />
  <SegmentedControl
    value={viewMode}
    ariaLabel={$t('proxies.view_mode_label')}
    items={[
      { value: 'grid', label: $t('proxies.view_mode_grid'), icon: ViewGrid },
      { value: 'list', label: $t('proxies.view_mode_list'), icon: ViewList }
    ]}
    onchange={(mode) => (viewMode = mode as ProxiesViewMode)}
  />
  <Button variant="secondary" onclick={() => onExpandAll?.()} title={$t('proxies.expand_all')}>
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      style="margin-right: 6px;"
    >
      <polyline points="6 9 12 15 18 9" />
      <polyline points="6 4 12 10 18 4" />
    </svg>
    {$t('proxies.expand_all')}
  </Button>
  <Button variant="secondary" onclick={() => onCollapseAll?.()} title={$t('proxies.collapse_all')}>
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      style="margin-right: 6px;"
    >
      <polyline points="18 15 12 9 6 15" />
      <polyline points="18 20 12 14 6 20" />
    </svg>
    {$t('proxies.collapse_all')}
  </Button>
  <Button variant="secondary" onclick={() => onRefresh?.()} disabled={loading}>
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    >
      <path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" />
    </svg>
    {loading ? $t('app.loading') : $t('app.refresh')}
  </Button>
  <PingTargetQuickMenu />
</div>

<style>
  .proxy-filter-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .group-search {
    padding: 6px 12px;
    border: 1px solid var(--border);
    background: var(--bg-input);
    color: var(--fg-primary);
    border-radius: var(--radius-sm);
    font-size: 13px;
    width: 200px;
    transition: border-color 0.2s;
  }

  .group-search:focus {
    border-color: var(--accent);
    outline: none;
  }

  @media (max-width: 768px) {
    .group-search {
      width: 100%;
    }
  }
</style>
