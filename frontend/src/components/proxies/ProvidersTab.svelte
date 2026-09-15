<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '../../i18n';
  import { capabilities, devMode } from '../../stores';
  import SubscriptionList from '../subscriptions/SubscriptionList.svelte';
  import SubscriptionFormModal from '../subscriptions/SubscriptionFormModal.svelte';
  import type { ProvidersState } from './providersState.svelte';

  interface Props {
    state: ProvidersState;
    onOpenDiagnostic: (sub: any) => void;
  }

  let { state = $bindable(), onOpenDiagnostic }: Props = $props();

  onMount(() => {
    window.addEventListener('click', state.handleClickOutside);
  });

  onDestroy(() => {
    window.removeEventListener('click', state.handleClickOutside);
  });
</script>

<div class="providers-tab-root">
  {#if $capabilities?.xray && !$capabilities.xray.conf_dir_exists && $capabilities.active_kernel === 'xray'}
    <div class="confdir-warning">
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        style="flex-shrink:0"
      >
        <path
          d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
        />
        <line x1="12" y1="9" x2="12" y2="13" />
        <line x1="12" y1="17" x2="12.01" y2="17" />
      </svg>
      <span>{$t('subscr.confdir_warning').replace('{dir}', $capabilities.xray.conf_dir)}</span>
    </div>
  {/if}

  <div class="providers-view">
    {#if state.subscriptions.length === 0}
      <div
        class="card text-center"
        style="padding: 3rem; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1rem;"
      >
        <p style="color: var(--fg-secondary); margin: 0;">
          {$t('subscr.empty')}
        </p>
        <button class="btn btn-primary" onclick={() => state.openAddModal()}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            style="margin-right: 6px;"
          >
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          {$t('subscr.add_first')}
        </button>
      </div>
    {:else}
      <SubscriptionList
        subscriptions={state.subscriptions}
        expandedSubs={state.expandedSubs}
        refreshLoading={state.refreshLoading}
        activeDropdownId={state.activeDropdownId}
        subNodesLoading={state.subNodesLoading}
        subNodes={state.subNodes}
        subHealth={state.subHealth}
        checkingNodes={state.checkingNodes}
        subNodesError={state.subNodesError}
        getNodeSource={(sub) => state.getNodeSource(sub)}
        devMode={$devMode}
        stats={state.stats}
        dialerProxyTargets={state.dialerProxyTargets}
        onToggleExpand={(id) => state.toggleExpand(id)}
        onRefreshSub={(id) => state.refreshSubscription(id)}
        onEditSub={(sub) => state.openEditModal(sub)}
        onDeleteSub={(id) => state.deleteSubscription(id)}
        {onOpenDiagnostic}
        onSetActiveNode={(subId, nodeTag) => state.setActiveNode(subId, nodeTag)}
        onCheckNodeHealth={(subId, nodeTag) => state.checkNodeHealth(subId, nodeTag)}
        onToggleDropdown={(id) => state.toggleDropdown(id)}
        onRetryNodes={(subId) => state.loadMihomoNodes(subId)}
        onSetDialerProxy={(subId, nodeTag, targetTag) =>
          state.handleSetDialerProxy(subId, nodeTag, targetTag)}
      />
    {/if}
  </div>
</div>

<SubscriptionFormModal
  isOpen={state.showAddModal}
  editingSub={state.editingSub}
  bind:formName={state.formName}
  bind:formEnableXray={state.formEnableXray}
  bind:formEnableMihomo={state.formEnableMihomo}
  bind:formURL={state.formURL}
  bind:formInterval={state.formInterval}
  bind:formRoutingMode={state.formRoutingMode}
  bind:formTagPrefix={state.formTagPrefix}
  bind:formFilterName={state.formFilterName}
  bind:formFilterType={state.formFilterType}
  bind:formFilterTransport={state.formFilterTransport}
  bind:formExcludeFilter={state.formExcludeFilter}
  bind:formExcludeType={state.formExcludeType}
  bind:formMihomoGroups={state.formMihomoGroups}
  bind:formEnabled={state.formEnabled}
  bind:formUseProviderInterval={state.formUseProviderInterval}
  bind:formSockoptMark={state.formSockoptMark}
  bind:formSockoptFastOpen={state.formSockoptFastOpen}
  bind:formSockoptMptcp={state.formSockoptMptcp}
  availableMihomoGroups={state.availableMihomoGroups}
  onClose={() => state.closeModal()}
  onSave={() => state.saveSubscription()}
/>

<style>
  .confdir-warning {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    margin-bottom: 16px;
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--warning) 40%, transparent);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    line-height: 1.5;
  }
  .confdir-warning svg {
    color: var(--warning);
    margin-top: 2px;
  }
</style>
