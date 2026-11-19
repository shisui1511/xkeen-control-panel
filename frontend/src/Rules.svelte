<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { t } from './i18n';
  import {
    showToast,
    mihomoApiReady,
    mihomoApiState,
    mihomoOfflineReason,
    activeKernelName,
    isConflict
  } from './stores';
  import {
    apiFetch,
    flushFakeIP,
    flushDNSCache,
    fetchRuleProviders,
    updateRuleProvider,
    fetchRuleProviderDetails,
    type RuleProviderDetails,
    type RuleProvider
  } from './lib/api';
  import PageHeader from './PageHeader.svelte';
  import Tabs, { type TabItem } from './components/Tabs.svelte';
  import Button from './components/Button.svelte';
  import Skeleton from './components/Skeleton.svelte';
  import Icon from './lib/components/Icon.svelte';
  import RuleProvidersTab from './components/rules/RuleProvidersTab.svelte';
  import RouteDiagnosticTab from './components/rules/RouteDiagnosticTab.svelte';
  import AllKernelRulesTab, { type KernelRule } from './components/rules/AllKernelRulesTab.svelte';

  interface Props {
    onSwitchTab?: (tab: string) => void;
  }

  let { onSwitchTab = () => {} }: Props = $props();

  // Active tab state
  type TabKey = 'providers' | 'diagnostic' | 'all_rules';

  // Адрес #/rules?tab=providers (ссылка из «Профилей») открывает «Провайдеры» при любом ядре.
  function hashWantsProviders(): boolean {
    const query = window.location.hash.split('?')[1] ?? '';
    return new URLSearchParams(query).get('tab') === 'providers';
  }

  let activeTab = $state<TabKey>('providers');
  // Вкладку выбрали руками или адресом: самовыбор «Провайдеров» по состоянию ядра её не меняет.
  let tabChosen = $state(hashWantsProviders());

  // Страница без API Mihomo (D-19): ядро остановлено, API не отвечает или работает Xray.
  // До первого ответа capabilities (unknown) причины нет — показывается скелетон.
  type OfflineReason = 'xray' | 'not_running' | 'api_down';
  const offlineReason = $derived.by<OfflineReason | null>(() => {
    if ($mihomoApiState === 'unknown' || $isConflict || $mihomoApiReady) return null;
    if ($activeKernelName === 'xray') return 'xray';
    return $mihomoOfflineReason;
  });
  const stateKnown = $derived($mihomoApiState !== 'unknown');
  const offline = $derived(offlineReason !== null);
  const needApiText = $derived(
    $t(offlineReason === 'api_down' ? 'rules.tabs_need_api_down' : 'rules.tabs_need_api', {
      diag: $t('rules.tab_diagnostic'),
      all: $t('rules.tab_all_rules')
    })
  );

  // Kernel Rules State
  let kernelRules = $state<KernelRule[]>([]);
  let loadingKernelRules = $state(false);

  // Rule Providers State
  let ruleProviders = $state<RuleProvider[]>([]);
  let ruleProviderDetails = $state<Record<string, RuleProviderDetails>>({});
  let loadingProviders = $state(false);

  // Fake-IP flushing state
  let flushingFakeIP = $state(false);
  let flushingDNS = $state(false);

  // Subtitle mapping based on active tab
  let currentSubtitle = $derived.by(() => {
    switch (activeTab) {
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
    { value: 'providers', label: $t('rules.tab_providers'), testId: 'tab-providers' },
    {
      value: 'diagnostic',
      label: $t('rules.tab_diagnostic'),
      testId: 'tab-diagnostic',
      disabled: offline
    },
    {
      value: 'all_rules',
      label: $t('rules.tab_all_rules'),
      testId: 'tab-all_rules',
      disabled: offline
    }
  ]);

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
      const [providers, details] = await Promise.all([
        fetchRuleProviders(),
        fetchRuleProviderDetails().catch(() => [])
      ]);
      ruleProviders = providers;
      ruleProviderDetails = Object.fromEntries(details.map((d) => [d.name, d]));
    } catch (e: any) {
      if (e?.status === 401) return;
    } finally {
      loadingProviders = false;
    }
  }

  // Без API ядра список строится по файлам профиля: типа, поведения и размера файла достаточно,
  // числа записей и времени обновления у панели без ядра нет. Прочие запросы ядра не идут.
  async function loadProvidersOffline() {
    loadingProviders = true;
    try {
      const details = await fetchRuleProviderDetails();
      ruleProviders = details.map((d) => ({
        name: d.name,
        behavior: d.behavior,
        type: d.type,
        vehicleType: d.type,
        ruleCount: 0,
        updatedAt: ''
      }));
      ruleProviderDetails = Object.fromEntries(details.map((d) => [d.name, d]));
    } catch (e: any) {
      if (e?.status === 401) return;
      ruleProviders = [];
      ruleProviderDetails = {};
    } finally {
      loadingProviders = false;
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
        failed.push(e?.message || provider.name);
      }
    }
    await loadProviders();
    if (failed.length === 0) {
      showToast('success', $t('rules.update_all_success'));
    } else {
      showToast(
        'error',
        $t('rules.update_providers_failed', {
          n: String(failed.length),
          list: failed.join('; ')
        }),
        8000
      );
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

  async function handleFlushDNS() {
    flushingDNS = true;
    try {
      await flushDNSCache();
      showToast('success', $t('rules.dns_flushed'));
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    } finally {
      flushingDNS = false;
    }
  }

  // Данные ядра (правила, провайдеры, группы) читаются, когда API Mihomo отвечает:
  // при Xray, остановленном ядре или недоступном API запросы давали 409/502 и
  // ошибки в консоли браузера. Возврат API в строй перечитывает данные.
  $effect(() => {
    if (!$mihomoApiReady) return;
    untrack(() => {
      loadProviders();
      loadKernelRules();
    });
  });

  // Без API Mihomo «Провайдеры» читаются из файлов профиля (D-19, D-21).
  $effect(() => {
    if (!offline) return;
    untrack(() => {
      loadProvidersOffline();
    });
  });

  // Первой открывается «Провайдеры»; если ядро остановилось при открытой «Диагностике» или
  // «Всех правилах», страница сама возвращается на «Провайдеры».
  $effect(() => {
    if (!offline) return;
    if (!tabChosen || activeTab === 'diagnostic' || activeTab === 'all_rules') {
      activeTab = 'providers';
    }
  });

  onMount(() => {
    const onHash = () => {
      if (!hashWantsProviders()) return;
      tabChosen = true;
      activeTab = 'providers';
    };
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  });
</script>

<div class="container rules-page">
  <PageHeader title={$t('rules.title')} subtitle={currentSubtitle}>
    {#snippet actions()}
      <Button
        variant="secondary"
        class="btn-sm"
        loading={flushingFakeIP}
        disabled={flushingFakeIP || offline}
        title={offline ? needApiText : undefined}
        onclick={handleFlushFakeIP}
      >
        <Icon name="refresh" size={14} />
        <span>{flushingFakeIP ? $t('rules.flushing_fakeip') : $t('rules.flush_fakeip')}</span>
      </Button>
      <Button
        variant="secondary"
        class="btn-sm"
        loading={flushingDNS}
        disabled={flushingDNS || offline}
        title={offline ? needApiText : $t('rules.flush_dns_title')}
        onclick={handleFlushDNS}
      >
        <Icon name="refresh" size={14} />
        <span>{flushingDNS ? $t('rules.flushing_fakeip') : $t('rules.flush_dns')}</span>
      </Button>
    {/snippet}
  </PageHeader>

  <div class="tabs-wrapper">
    <Tabs
      items={tabItems}
      value={activeTab}
      onchange={(val) => {
        tabChosen = true;
        activeTab = val as TabKey;
      }}
      ariaLabel={$t('rules.title')}
    />
    {#if offline}
      <p class="tabs-note" role="status" data-testid="rules-tabs-note">{needApiText}</p>
    {/if}
  </div>

  <div class="tab-content" role="tabpanel">
    {#if !stateKnown}
      <Skeleton type="card" height="280px" />
    {:else if activeTab === 'providers'}
      <RuleProvidersTab
        providers={ruleProviders}
        details={ruleProviderDetails}
        loading={loadingProviders}
        offline={offlineReason}
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
    gap: var(--spacing-5);
    width: 100%;
  }

  .tabs-wrapper {
    margin-top: calc(-1 * var(--spacing-1));
  }

  .tabs-note {
    margin: var(--spacing-2) 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .tab-content {
    min-height: 280px;
  }
</style>
