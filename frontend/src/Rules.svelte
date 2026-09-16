<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from './i18n';
  import { showToast } from './stores';
  import {
    apiFetch,
    fetchCustomRules,
    saveCustomRules,
    flushFakeIP,
    fetchRuleProviders,
    updateRuleProvider,
    type UserRule,
    type RuleProvider
  } from './lib/api';
  import PageHeader from './PageHeader.svelte';
  import Tabs, { type TabItem } from './components/Tabs.svelte';
  import Button from './components/Button.svelte';
  import Icon from './lib/components/Icon.svelte';
  import UserRulesTab from './components/rules/UserRulesTab.svelte';
  import RuleProvidersTab from './components/rules/RuleProvidersTab.svelte';
  import RouteDiagnosticTab from './components/rules/RouteDiagnosticTab.svelte';
  import AllKernelRulesTab, { type KernelRule } from './components/rules/AllKernelRulesTab.svelte';

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

  // Active tab state
  type TabKey = 'exceptions' | 'providers' | 'diagnostic' | 'all_rules';
  let activeTab = $state<TabKey>('exceptions');

  // Custom Rules State
  let customRules = $state<UserRule[]>([]);
  let loadingCustom = $state(false);

  // Kernel Rules State
  let kernelRules = $state<KernelRule[]>([]);
  let loadingKernelRules = $state(false);

  // Rule Providers State
  let ruleProviders = $state<RuleProvider[]>([]);
  let loadingProviders = $state(false);

  // Proxy Groups State
  let proxyGroups = $state<string[]>([]);

  // Fake-IP flushing state
  let flushingFakeIP = $state(false);

  // Subtitle mapping based on active tab
  let currentSubtitle = $derived.by(() => {
    switch (activeTab) {
      case 'exceptions':
        return $t('rules.exceptions_subtitle');
      case 'providers':
        return $t('rules.providers_subtitle');
      case 'diagnostic':
        return $t('rules.diagnostic_subtitle');
      case 'all_rules':
        return $t('rules.all_rules_subtitle');
      default:
        return $t('rules.subtitle');
    }
  });

  // Tab definitions
  let tabItems = $derived<TabItem[]>([
    { value: 'exceptions', label: $t('rules.tab_exceptions'), testId: 'tab-exceptions' },
    { value: 'providers', label: $t('rules.tab_providers'), testId: 'tab-providers' },
    { value: 'diagnostic', label: $t('rules.tab_diagnostic'), testId: 'tab-diagnostic' },
    { value: 'all_rules', label: $t('rules.tab_all_rules'), testId: 'tab-all_rules' }
  ]);

  async function loadCustomRules() {
    loadingCustom = true;
    try {
      customRules = await fetchCustomRules();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    } finally {
      loadingCustom = false;
    }
  }

  async function handleSaveCustomRules(rules: UserRule[]) {
    try {
      const res = await saveCustomRules(rules);
      customRules = rules;
      if (res.reloaded) {
        showToast('success', $t('rules.custom_saved'));
      } else {
        showToast('success', $t('rules.custom_saved'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
      throw e;
    }
  }

  async function loadKernelRules() {
    loadingKernelRules = true;
    try {
      const res = await apiFetch('/api/mihomo/proxy/rules');
      if (res.ok) {
        const data = await res.json();
        kernelRules = data.rules || [];
      }
    } catch (e: any) {
      if (e?.status === 401) return;
    } finally {
      loadingKernelRules = false;
    }
  }

  async function loadProviders() {
    loadingProviders = true;
    try {
      ruleProviders = await fetchRuleProviders();
    } catch (e: any) {
      if (e?.status === 401) return;
    } finally {
      loadingProviders = false;
    }
  }

  async function loadProxyGroups() {
    try {
      const res = await apiFetch('/api/mihomo/proxy/proxies');
      if (res.ok) {
        const data = await res.json();
        const proxies = data.proxies || {};
        const groups: string[] = [];
        for (const [name, info] of Object.entries(proxies) as [string, any][]) {
          const type = (info.type || '').toLowerCase();
          if (['selector', 'urltest', 'fallback', 'loadbalance', 'relay'].includes(type)) {
            groups.push(name);
          }
        }
        proxyGroups = groups.sort();
      }
    } catch {
      // Fallback: extract groups from rules
      const set = new Set<string>();
      for (const r of kernelRules) {
        const p = (r.proxy || '').trim();
        if (p && !['DIRECT', 'REJECT', 'PASS'].includes(p.toUpperCase())) {
          set.add(p);
        }
      }
      proxyGroups = Array.from(set).sort();
    }
  }

  async function handleUpdateProvider(name: string) {
    try {
      await updateRuleProvider(name);
      showToast('success', $t('rules.update_success'));
      await loadProviders();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    }
  }

  async function handleUpdateAllProviders() {
    const failed: string[] = [];
    for (const provider of ruleProviders) {
      try {
        await updateRuleProvider(provider.name);
      } catch (e: any) {
        if (e?.status === 401) return;
        failed.push(provider.name);
      }
    }
    await loadProviders();
    if (failed.length === 0) {
      showToast('success', $t('rules.update_all_success'));
    } else {
      for (const name of failed) {
        showToast('error', $t('rules.update_provider_failed', { name }));
      }
    }
  }

  async function handleFlushFakeIP() {
    flushingFakeIP = true;
    try {
      await flushFakeIP();
      showToast('success', $t('rules.fakeip_flushed'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    } finally {
      flushingFakeIP = false;
    }
  }

  onMount(() => {
    loadCustomRules();
    loadProviders();
    loadKernelRules().then(() => loadProxyGroups());
  });
</script>

<div class="container rules-page">
  <PageHeader
    title={$t('rules.title')}
    subtitle={currentSubtitle}
    breadcrumbs={[{ label: $t('nav.group_routing') }, { label: $t('rules.title') }]}
    {onSwitchTab}
  >
    {#snippet actions()}
      <Button
        variant="secondary"
        class="btn-sm"
        loading={flushingFakeIP}
        disabled={flushingFakeIP}
        onclick={handleFlushFakeIP}
      >
        <Icon name="refresh" size={14} />
        <span>{flushingFakeIP ? $t('rules.flushing_fakeip') : $t('rules.flush_fakeip')}</span>
      </Button>
    {/snippet}
  </PageHeader>

  <div class="tabs-wrapper">
    <Tabs
      items={tabItems}
      value={activeTab}
      onchange={(val) => (activeTab = val as TabKey)}
      ariaLabel={$t('rules.title')}
    />
  </div>

  <div class="tab-content" role="tabpanel">
    {#if activeTab === 'exceptions'}
      <UserRulesTab
        rules={customRules}
        groups={proxyGroups}
        loading={loadingCustom}
        onSave={handleSaveCustomRules}
      />
    {:else if activeTab === 'providers'}
      <RuleProvidersTab
        providers={ruleProviders}
        loading={loadingProviders}
        onUpdate={handleUpdateProvider}
        onUpdateAll={handleUpdateAllProviders}
      />
    {:else if activeTab === 'diagnostic'}
      <RouteDiagnosticTab />
    {:else if activeTab === 'all_rules'}
      <AllKernelRulesTab rules={kernelRules} loading={loadingKernelRules} />
    {/if}
  </div>
</div>

<style>
  .rules-page {
    display: flex;
    flex-direction: column;
    gap: 20px;
    width: 100%;
  }

  .tabs-wrapper {
    margin-top: -4px;
  }

  .tab-content {
    min-height: 280px;
  }
</style>
