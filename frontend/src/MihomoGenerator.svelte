<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Modal from './components/Modal.svelte';
  import DraftRestoreBanner from './components/DraftRestoreBanner.svelte';
  import Select from './components/Select.svelte';
  import Button from './components/Button.svelte';
  import ConstructorPreview from './components/ConstructorPreview.svelte';
  import { registerDirtySource, getDraft, clearDraft, type DraftRecord } from './lib/dirtyRegistry';
  import { currentLang, t } from './i18n';
  import { capabilities, showToast, fetchCapabilities, showConfirm } from './stores';
  import { apiFetch } from './lib/api';
  import { parseValidationError } from './lib/errorParser';
  import {
    applyMihomoConfig,
    undoMihomoConfig,
    mergeMihomoProviders,
    applyMihomoPreset
  } from './lib/constructors/mihomoApply';
  import {
    populateMihomoFromYAML as populateMihomoFromYAML_raw,
    ZKEEN_RULE_PROVIDERS,
    type ProxyGroup
  } from './lib/mihomoYaml';
  import {
    MihomoContext,
    setMihomoContext,
    type ActiveMihomoSection
  } from './components/mihomo/MihomoContext.svelte';
  import MihomoSectionProxies from './components/mihomo/MihomoSectionProxies.svelte';
  import MihomoSectionGroups from './components/mihomo/MihomoSectionGroups.svelte';
  import MihomoSectionRulesets from './components/mihomo/MihomoSectionRulesets.svelte';
  import MihomoSectionRules from './components/mihomo/MihomoSectionRules.svelte';
  import MihomoSectionDns from './components/mihomo/MihomoSectionDns.svelte';
  import MihomoSectionTun from './components/mihomo/MihomoSectionTun.svelte';
  import MihomoSectionListeners from './components/mihomo/MihomoSectionListeners.svelte';
  import PreflightWarnings, {
    type PreflightWarning
  } from './components/editor/PreflightWarnings.svelte';

  let {
    onSwitchTab = () => {},
    selectedFile = '',
    onInsertIntoEditor = () => {},
    embedded = false,
    invalidateCache = false
  }: {
    onSwitchTab?: (tab: string) => void;
    selectedFile?: string;
    onInsertIntoEditor?: (content: string) => void;
    embedded?: boolean;
    initialPreset?: string;
    invalidateCache?: boolean;
  } = $props();

  // Instantiate reactive context
  const ctx = new MihomoContext();
  setMihomoContext(ctx);

  // Preset Tracking
  let lastAppliedPreset = $state('');
  let presetBaseline = $state('');

  let canUndo = $state(false);
  function checkUndo() {
    canUndo = !!localStorage.getItem('xcp_prev_mihomo_yaml');
  }

  // Preflight warnings & validation
  let saveWarnings = $state<PreflightWarning[]>([]);
  let warningsTitle = $state<string | undefined>(undefined);
  let validationError = $state('');
  let schema: any = $state(null);
  let schemaLoading = $state(true);
  let schemaError = $state('');
  let showApplyConfirm = $state(false);
  let applyLoading = $state(false);

  let dismissZkeenGeodataWarning = $state(false);
  let lastActivePreset = '';
  $effect(() => {
    if (ctx.activePreset !== lastActivePreset) {
      if (lastActivePreset && ctx.activePreset !== lastActivePreset) {
        localStorage.removeItem('xcp:dismissed_warning:zkeen_geodata');
      }
      lastActivePreset = ctx.activePreset;
      const dismissed = localStorage.getItem('xcp:dismissed_warning:zkeen_geodata');
      dismissZkeenGeodataWarning = dismissed === ctx.activePreset;
    }
  });

  const blockingValidationMsg = $derived(
    schemaError
      ? schemaError
      : validationError
        ? parseValidationError(validationError, $currentLang)
        : ctx.proxies.length === 0 && ctx.groups.length === 0
          ? $t('mihomo.blocking_validation_error')
          : ''
  );

  // Preset Application
  function applyPreset(id: string, silent = false) {
    validationError = '';
    const res = applyMihomoPreset(ctx, id, schema, silent);
    lastAppliedPreset = res.lastAppliedPreset;
    presetBaseline = res.presetBaseline;
  }

  const isPresetModified = $derived.by(() => {
    if (!lastAppliedPreset || !presetBaseline) return false;
    const current = JSON.stringify({
      activeRuleProvider: ctx.activeRuleProvider,
      groups: ctx.groups.map((g) => ({ name: g.name, type: g.type, enabled: g.enabled })),
      rules: ctx.rules.map((r) => ({ type: r.type, value: r.value, outbound: r.outbound }))
    });
    return current !== presetBaseline;
  });

  async function loadSubscriptions() {
    try {
      const res = await apiFetch('/api/subscriptions');
      if (!res.ok) return;
      const subs = await res.json();
      if (Array.isArray(subs)) {
        ctx.subscriptions = subs.filter((s) => s.enabled);
        const dbMihomo = subs.filter((s) => s.enabled && s.enable_mihomo);
        ctx.mihomoProviders = mergeMihomoProviders(dbMihomo, ctx.lastParsedProviders);
      } else {
        ctx.subscriptions = [];
        ctx.mihomoProviders = mergeMihomoProviders([], ctx.lastParsedProviders);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      console.error(e);
    }
  }

  function populateMihomoFromYAML(text: string) {
    saveWarnings = [];
    warningsTitle = undefined;
    if (!text || text.trim() === '') {
      applyPreset('zkeen-selective', true);
      ctx.lastParsedProviders = [];
      ctx.mihomoProviders = mergeMihomoProviders(
        ctx.subscriptions.filter((s) => s.enable_mihomo),
        []
      );
      return;
    }
    try {
      const res = populateMihomoFromYAML_raw(text) as any;
      ctx.proxies = res.proxies || [];
      ctx.groups = res.groups || [];
      ctx.rules = res.rules || [];
      ctx.dns = res.dns || ctx.dns;
      ctx.tun = res.tun || ctx.tun;
      ctx.sniffer = res.sniffer || ctx.sniffer;
      ctx.activeRuleProvider = res.activeRuleProvider as any;
      ctx.selectedMetaRuleSets = res.selectedMetaRuleSets || new Map();
      ctx.preservedKeys = res.preservedKeys || [];
      ctx.existingTproxyPort = res.existingTproxyPort;
      ctx.existingRedirPort = res.existingRedirPort;
      ctx.externalControllerType = res.externalControllerType || 'unix';
      ctx.externalControllerTarget = res.externalControllerTarget || '127.0.0.1:9090';
      ctx.listeners = res.listeners || [];
      ctx.listenersRaw = res.listenersRaw || null;
      ctx.listenersReadOnly = res.listenersReadOnly || false;

      saveWarnings = Array.isArray(res.warnings)
        ? res.warnings.map((w: any) =>
            typeof w === 'string' ? { message: w } : { code: w.code, params: w.params }
          )
        : [];
      if (saveWarnings.length > 0) {
        warningsTitle = $t('editor.config_warnings_title');
      }

      ctx.lastParsedProviders = res.mihomoProviders || [];
      ctx.mihomoProviders = mergeMihomoProviders(
        ctx.subscriptions.filter((s) => s.enable_mihomo),
        ctx.lastParsedProviders
      );

      if (res.groups.length === 0 && res.proxies.length === 0) {
        applyPreset('zkeen-selective', true);
      }
    } catch {
      showToast('warning', $t('mihomo.read_config_fallback'));
      applyPreset('zkeen-selective', true);
    }
  }

  let configLoadedForPath = '';

  async function loadConfig(path: string, force = false) {
    if (!path) return;
    if (configLoadedForPath === path && !force) return;
    configLoadedForPath = path;
    try {
      const res = await apiFetch(`/api/config/read?path=${encodeURIComponent(path)}`);
      if (res.status === 404) {
        populateMihomoFromYAML('');
        return;
      }
      if (!res.ok) {
        const errText = await res.text();
        throw new Error(errText || `HTTP ${res.status}`);
      }
      const text = await res.text();
      populateMihomoFromYAML(text);
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', $t('mihomo.config_load_error', { err: e.message }));
    }
    await loadSubscriptions();
  }

  async function checkZkeenGeodata() {
    try {
      const res = await apiFetch('/api/dat/tags?name=geosite.dat');
      if (res.ok) {
        const json = await res.json();
        const tags = json.tags || [];
        const tagNames = tags.map((t: any) => t.tag.toLowerCase());
        ctx.hasZkeenGeodata =
          tagNames.includes('domains') &&
          tagNames.includes('other') &&
          tagNames.includes('politic');
      }
    } catch (e) {
      console.error('Failed to load geosite.dat tags:', e);
      ctx.hasZkeenGeodata = false;
    }
  }

  let detectedDraft = $state<DraftRecord | null>(null);
  let unregisterDirty: (() => void) | null = null;

  function handleRestoreDraft() {
    if (detectedDraft?.data?.yaml) {
      populateMihomoFromYAML(detectedDraft.data.yaml);
      ctx.markDirty();
      clearDraft('mihomo_generator');
      detectedDraft = null;
      showToast('success', $t('draft.restored_toast'));
    }
  }

  function handleDiscardDraft() {
    clearDraft('mihomo_generator');
    detectedDraft = null;
    showToast('info', $t('draft.discarded_toast'));
  }

  async function loadSchema() {
    schemaLoading = true;
    schemaError = '';
    try {
      const res = await apiFetch('/api/assets/definition');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      schema = await res.json();
    } catch (e: any) {
      if (e?.status === 401) return;
      schemaError = e.message || 'Unknown error';
    } finally {
      schemaLoading = false;
    }
  }

  onMount(async () => {
    await loadSchema();
    await loadConfig(selectedFile || '/opt/etc/mihomo/config.yaml', true);
    await checkZkeenGeodata();
    checkUndo();

    const draft = getDraft('mihomo_generator');
    if (draft) {
      detectedDraft = draft;
    }

    unregisterDirty = registerDirtySource('mihomo_generator', {
      name: $t('editor.tab_constructor') || 'Mihomo Generator',
      isDirty: () => ctx.isDirty,
      onSave: async () => {
        await handleApplyMihomo(true);
        return !ctx.isDirty;
      },
      getDraft: () => ({
        yaml: ctx.generateYaml($capabilities)
      }),
      restoreDraft: (draftRecord) => {
        if (draftRecord?.data?.yaml) {
          detectedDraft = draftRecord;
          handleRestoreDraft();
        }
      }
    });
  });

  onDestroy(() => {
    if (unregisterDirty) {
      unregisterDirty();
      unregisterDirty = null;
    }
  });

  $effect(() => {
    if (selectedFile) {
      loadConfig(selectedFile);
    }
  });

  let prevInvalidateCache = false;
  $effect(() => {
    if (invalidateCache && !prevInvalidateCache) {
      prevInvalidateCache = true;
      configLoadedForPath = '';
      loadConfig(selectedFile || '/opt/etc/mihomo/config.yaml', true);
    } else if (!invalidateCache) {
      prevInvalidateCache = false;
    }
  });

  let yaml = $derived.by(() => {
    void ctx.proxies;
    void ctx.groups;
    void ctx.rules;
    void ctx.listeners;
    void ctx.listenersRaw;
    void ctx.listenersReadOnly;
    void ctx.activeRuleProvider;
    void ctx.selectedMetaRuleSets;
    void ctx.subscriptions;
    void ctx.mihomoProviders;
    void ctx.externalControllerType;
    void ctx.externalControllerTarget;
    void ctx.dns.enabled;
    void ctx.dns.nameservers;
    void ctx.dns.fallback;
    void ctx.tun.enabled;
    void ctx.tun.stack;
    void ctx.hasZkeenGeodata;
    void ctx.sniffer.enabled;
    void ctx.sniffer.sniffHttp;
    void ctx.sniffer.sniffTls;
    void ctx.sniffer.sniffQuic;
    void ctx.safeMergeEnabled;
    return ctx.generateYaml($capabilities);
  });

  function togglePreviewPane() {
    ctx.showPreviewPane = !ctx.showPreviewPane;
  }

  function openInEditor() {
    if (onInsertIntoEditor) {
      onInsertIntoEditor(yaml);
    } else {
      onSwitchTab('editor');
    }
  }

  async function handleApplyMihomo(skipConfirm: boolean | unknown = false) {
    const shouldSkipConfirm = skipConfirm === true;
    if (!shouldSkipConfirm && !showApplyConfirm && ctx.proxies.length === 0) {
      if (
        !(await showConfirm({
          title: $t('editor.empty_proxies_title'),
          consequence: $t('editor.empty_proxies_warning'),
          variant: 'warning',
          confirmLabel: $t('app.continue')
        }))
      ) {
        return;
      }
    }
    if (!shouldSkipConfirm && !showApplyConfirm) {
      showApplyConfirm = true;
      return;
    }
    showApplyConfirm = false;
    applyLoading = true;

    try {
      await applyMihomoConfig({
        ctx,
        selectedFile,
        capabilitiesKernel: $capabilities,
        onSetValidationError: (err: string) => {
          validationError = err;
        },
        onSetSaveWarnings: (warnings: PreflightWarning[], title?: string) => {
          saveWarnings = warnings;
          warningsTitle = title || '';
        }
      });
      checkUndo();
    } finally {
      applyLoading = false;
    }
  }

  async function handleUndo() {
    applyLoading = true;
    try {
      const ok = await undoMihomoConfig(selectedFile, $capabilities, (prevYaml: string) =>
        populateMihomoFromYAML(prevYaml)
      );
      if (ok) {
        ctx.resetDirty();
        checkUndo();
      }
    } finally {
      applyLoading = false;
    }
  }

  let tabs = $derived([
    ['proxies', $t('mihomo.tab_proxies')],
    ['groups', $t('mihomo.tab_groups')],
    ...(ctx.activeRuleProvider === 'metacubex' ? [['rulesets', $t('mihomo.tab_rulesets')]] : []),
    ['rules', $t('mihomo.tab_rules')],
    ['dns', 'DNS'],
    ['tun', 'TUN'],
    ['listeners', $t('mihomo.tab_listeners')]
  ]);

  $effect(() => {
    if (
      ctx.activeRuleProvider === 'metacubex' &&
      ctx.activeSection !== 'rulesets' &&
      ctx.activeSection !== 'proxies' &&
      ctx.activeSection !== 'groups' &&
      ctx.activeSection !== 'rules' &&
      ctx.activeSection !== 'dns' &&
      ctx.activeSection !== 'tun' &&
      ctx.activeSection !== 'listeners'
    ) {
      ctx.activeSection = 'rulesets';
    }
  });
</script>

<div class="container">
  {#if detectedDraft}
    <DraftRestoreBanner
      timestamp={detectedDraft.timestamp}
      onRestore={handleRestoreDraft}
      onDiscard={handleDiscardDraft}
    />
  {/if}

  {#if schemaLoading}
    <div
      class="loading-state-block"
      style="padding: 48px; text-align: center; color: var(--fg-secondary);"
    >
      <div class="spinner" style="--spinner-size: 24px; margin: 0 auto 12px;"></div>
      <p>{$t('editor.loading_definition')}</p>
    </div>
  {:else if schemaError}
    <div class="error-state-block" style="padding: 48px; text-align: center;">
      <div class="error-icon" style="color: var(--danger); font-size: 24px; margin-bottom: 12px;">
        ⚠
      </div>
      <p style="color: var(--danger); margin-bottom: 16px;">
        {$t('editor.definition_load_error', { error: schemaError })}
      </p>
      <button class="btn btn-secondary" onclick={loadSchema}>{$t('app.retry')}</button>
    </div>
  {:else}
    {#if !embedded}
      <div class="constructor-header">
        <div class="constructor-header-content">
          <h2 class="constructor-title">{$t('mihomo.h1')}</h2>
          <p class="constructor-sub">
            {$t('mihomo.h1_sub')}
          </p>
        </div>
        <div class="constructor-header-actions">
          <Button
            type="button"
            variant="secondary"
            onclick={togglePreviewPane}
            title={ctx.showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
              <line x1="15" y1="3" x2="15" y2="21" />
            </svg>
            <span
              >{ctx.showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}</span
            >
          </Button>
          <Button variant="secondary" onclick={openInEditor}>
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="margin-right:5px"
              ><path d="M12 20h9" /><path
                d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
              /></svg
            >
            {#if selectedFile}
              {$t('mihomo.insert_editor')}
            {:else}
              {$t('mihomo.open_editor')}
            {/if}
          </Button>
        </div>
      </div>
    {:else}
      <div class="embedded-head-toolbar">
        <div class="embedded-title-tag">
          <span style="color: var(--fg-secondary);">{$t('editor.title')} › </span>
          <strong>{$t('mihomo.breadcrumb_generator')}</strong>
        </div>
        <div class="constructor-header-actions">
          <Button
            type="button"
            variant="secondary"
            onclick={togglePreviewPane}
            title={ctx.showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}
          >
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
              <line x1="15" y1="3" x2="15" y2="21" />
            </svg>
            <span
              >{ctx.showPreviewPane ? $t('mihomo.hide_preview') : $t('mihomo.show_preview')}</span
            >
          </Button>
        </div>
      </div>
    {/if}

    {#if ctx.preservedKeys.length > 0}
      <div class="card safe-merge-card alert-warning" data-testid="safe-merge-card">
        <div class="safe-merge-head">
          <div class="safe-merge-title-group">
            <div class="safe-merge-icon-wrap">
              <svg
                width="15"
                height="15"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
              </svg>
            </div>
            <div>
              <div class="safe-merge-title">{$t('mihomo.safe_merge_title')}</div>
              <div class="safe-merge-desc">
                {$t('mihomo.safe_merge_desc')}
                <span class="sr-only">({ctx.preservedKeys.join(', ')})</span>
              </div>
            </div>
          </div>
          <label class="switch safe-merge-switch" title={$t('mihomo.safe_merge_toggle')}>
            <input type="checkbox" bind:checked={ctx.safeMergeEnabled} />
            <span class="slider round"></span>
          </label>
        </div>

        {#if ctx.safeMergeEnabled}
          <div class="safe-merge-tags">
            {#each ctx.safeMergeExpanded ? ctx.preservedKeys : ctx.preservedKeys.slice(0, 6) as key}
              <span class="directive-tag"><code>{key}</code></span>
            {/each}
            {#if ctx.preservedKeys.length > 6}
              <button
                type="button"
                class="btn-tag-expand"
                onclick={() => (ctx.safeMergeExpanded = !ctx.safeMergeExpanded)}
              >
                {ctx.safeMergeExpanded
                  ? $t('mihomo.safe_merge_tags_less')
                  : $t('mihomo.safe_merge_tags_more', { count: ctx.preservedKeys.length - 6 })}
              </button>
            {/if}
          </div>
        {/if}
      </div>
    {/if}

    <div class="gen-layout">
      <!-- Left: sections -->
      <div class="gen-left">
        <!-- Scenario selection -->
        <div class="constructor-scenario-bar">
          <div class="scenario-select-wrap">
            <label for="preset-select" class="form-label"
              >{$t('editor.constructor_scenario')}:</label
            >
            <Select
              id="preset-select"
              class="form-select preset-select"
              value={ctx.activePreset}
              onchange={(e) => {
                const val = e.currentTarget.value;
                applyPreset(val);
                if (val === 'rule-based') {
                  ctx.activeSection = 'rulesets';
                } else if (val === 'zkeen-selective') {
                  ctx.activeSection = 'groups';
                }
              }}
            >
              <option value="">-- {$t('editor.constructor_scenario')} --</option>
              {#if schema && schema.mihomo && schema.mihomo.presets}
                {#each schema.mihomo.presets as p}
                  <option value={p.id}>{$t(p.name)}</option>
                {/each}
              {:else}
                <option value="rule-based">{$t('editor.scenario_rule_based')}</option>
                <option value="global-proxy">{$t('editor.scenario_global_proxy')}</option>
                <option value="zkeen-selective">{$t('editor.scenario_zkeen_selective')}</option>
                <option value="only-blocked">{$t('preset.only-blocked')}</option>
              {/if}
            </Select>
          </div>
          {#if isPresetModified}
            <span class="preset-modified-chip">{$t('xray.preset_modified')}</span>
          {/if}
        </div>

        <PreflightWarnings
          warnings={saveWarnings}
          title={warningsTitle}
          onDismiss={() => {
            saveWarnings = [];
            warningsTitle = undefined;
          }}
        />

        {#if ctx.activePreset === 'zkeen-selective' && !ctx.hasZkeenGeodata && !dismissZkeenGeodataWarning}
          <div
            class="alert alert-warning alert-dismissible"
            style="margin-bottom: 16px; padding: 8px 12px; font-size: 13px; display: flex; align-items: center; gap: 8px; border-radius: var(--radius-sm);"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              style="flex-shrink: 0;"
              ><path
                d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
              /><line x1="12" y1="9" x2="12" y2="13" /><line
                x1="12"
                y1="17"
                x2="12.01"
                y2="17"
              /></svg
            >
            <span>{$t('editor.requires_zkeen_geodata')}</span>
            <button
              type="button"
              class="alert-close-btn"
              style="top: 50%; transform: translateY(-50%);"
              onclick={() => {
                dismissZkeenGeodataWarning = true;
                localStorage.setItem('xcp:dismissed_warning:zkeen_geodata', ctx.activePreset);
              }}
              aria-label={$t('app.close')}>&times;</button
            >
          </div>
        {/if}

        <!-- Rule providers -->
        <div class="rule-providers-row">
          <label class="form-label" for="rp-select">{$t('mihomo.rule_provider_label')}</label>
          <Select
            id="rp-select"
            class="form-select rp-select"
            bind:value={ctx.activeRuleProvider}
            onchange={(e) => {
              if (e.currentTarget.value === 'metacubex') {
                ctx.activeSection = 'rulesets';
              }
            }}
          >
            <option value="none">{$t('editor.rp_none')}</option>
            <option value="zkeen">{$t('editor.rp_zkeen')}</option>
            <option value="metacubex">{$t('editor.rp_metacubex')}</option>
          </Select>
        </div>

        <!-- Section tabs -->
        <div class="sec-tabs">
          {#each tabs as [id, label]}
            <button
              class="sec-tab"
              class:active={ctx.activeSection === id}
              onclick={() => {
                ctx.activeSection = id as ActiveMihomoSection;
              }}
            >
              {label}
              {#if id === 'proxies' && ctx.proxies.length > 0}
                <span class="sec-count">{ctx.proxies.length}</span>
              {/if}
              {#if id === 'groups' && ctx.groups.length > 0}
                <span class="sec-count">{ctx.groups.length}</span>
              {/if}
              {#if id === 'rulesets' && ctx.selectedMetaRuleSets.size > 0}
                <span class="sec-count">{ctx.selectedMetaRuleSets.size}</span>
              {/if}
              {#if id === 'rules' && ctx.rules.length > 0}
                <span class="sec-count">{ctx.rules.length}</span>
              {/if}
              {#if id === 'dns'}
                <span
                  class="tab-status-badge"
                  class:status-on={ctx.dns.enabled}
                  class:status-off={!ctx.dns.enabled}
                >
                  {ctx.dns.enabled ? $t('mihomo.tab_status_on') : $t('mihomo.tab_status_off')}
                </span>
              {/if}
              {#if id === 'tun'}
                <span
                  class="tab-status-badge"
                  class:status-on={ctx.tun.enabled}
                  class:status-off={!ctx.tun.enabled}
                >
                  {ctx.tun.enabled ? $t('mihomo.tab_status_on') : $t('mihomo.tab_status_off')}
                </span>
              {/if}
              {#if id === 'listeners' && ctx.listeners.length > 0}
                <span class="sec-count">{ctx.listeners.length}</span>
              {/if}
            </button>
          {/each}
        </div>

        <!-- Subcomponents -->
        {#if ctx.activeSection === 'proxies'}
          <MihomoSectionProxies />
        {:else if ctx.activeSection === 'groups'}
          <MihomoSectionGroups />
        {:else if ctx.activeSection === 'rulesets'}
          <MihomoSectionRulesets />
        {:else if ctx.activeSection === 'rules'}
          <MihomoSectionRules />
        {:else if ctx.activeSection === 'dns'}
          <MihomoSectionDns />
        {:else if ctx.activeSection === 'tun'}
          <MihomoSectionTun />
        {:else if ctx.activeSection === 'listeners'}
          <MihomoSectionListeners />
        {/if}
      </div>

      <!-- Preview pane with unified ConstructorPreview -->
      {#if ctx.showPreviewPane}
        <ConstructorPreview
          content={yaml}
          language="yaml"
          storageKey="mihomo_builder_preview_width"
          isOpen={ctx.showPreviewPane}
          onClose={() => (ctx.showPreviewPane = false)}
          title="YAML {$t('mihomo.preview')}"
          fileName="config.yaml"
          testId="mihomo-yaml-preview"
        >
          {#if validationError}
            <div
              class="validation-error-block"
              role="alert"
              aria-live="assertive"
              style="margin: 12px; padding: 12px; background: rgba(239, 91, 107, 0.1); border: 1px solid var(--danger); border-radius: var(--radius-md); color: var(--danger); font-size: 13px;"
            >
              <div style="font-weight: bold; margin-bottom: 6px;">
                {$t('editor.validation_failed')}
              </div>
              <div
                style="white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 13px; margin-bottom: 8px;"
              >
                {parseValidationError(validationError, $currentLang)}
              </div>
              <details>
                <summary style="cursor: pointer; font-size: 12px; opacity: 0.8; user-select: none;"
                  >{$t('editor.validation_details')}</summary
                >
                <pre
                  style="margin: 6px 0 0 0; white-space: pre-wrap; font-family: var(--font-family-mono); font-size: 12px; opacity: 0.9; max-height: 200px; overflow-y: auto;">{validationError}</pre>
              </details>
            </div>
          {/if}

          {#if blockingValidationMsg}
            <div
              class="constructor-validation-bar"
              role="status"
              aria-live="polite"
              style="margin: 0 12px 10px 12px; padding: 8px 12px; background: rgba(239, 91, 107, 0.12); border: 1px solid var(--danger); border-radius: var(--radius-sm); color: var(--danger); font-size: 12px; display: flex; align-items: center; gap: 8px;"
            >
              <span style="flex-shrink: 0;">⚠️</span>
              <span>{blockingValidationMsg}</span>
            </div>
          {/if}

          <div style="margin: 12px; display: flex; flex-direction: column; gap: 8px;">
            <button
              type="button"
              class="btn btn-primary"
              data-testid="apply-changes-btn"
              onclick={() => handleApplyMihomo()}
              disabled={applyLoading || !yaml || !!blockingValidationMsg}
            >
              {applyLoading ? $t('editor.saving') : $t('mihomo.apply_and_restart')}
            </button>
            <button type="button" class="btn btn-secondary" onclick={openInEditor}>
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                style="margin-right:5px"
                ><path d="M12 20h9" /><path
                  d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"
                /></svg
              >
              {#if selectedFile}
                {$t('mihomo.insert_editor')}
              {:else}
                {$t('mihomo.open_editor')}
              {/if}
            </button>

            {#if canUndo}
              <button
                type="button"
                class="btn btn-secondary"
                onclick={handleUndo}
                disabled={applyLoading}
              >
                {$t('editor.undo')}
              </button>
            {/if}
          </div>
        </ConstructorPreview>
      {/if}
    </div>
  {/if}
</div>

<!-- Modal Confirm Apply -->
<Modal
  isOpen={showApplyConfirm}
  title={$t('editor.apply_confirm_title')}
  dataTestid="apply-confirm-dialog"
  onclose={() => (showApplyConfirm = false)}
>
  <p>{$t('editor.apply_confirm_body')}</p>
  <div class="changed-files-list" style="margin-top: 12px;">
    <strong>{$t('mihomo.sections_to_update')}</strong>
    <div style="margin: 8px 0; font-family: monospace; font-size: 13px;">
      <code>{selectedFile || '/opt/etc/mihomo/config.yaml'}</code>
    </div>
    <ul style="margin: 8px 0 0 0; padding-left: 20px;">
      <li><code>proxy-groups</code></li>
      <li><code>rule-providers</code></li>
      <li><code>rules</code></li>
    </ul>
    <p style="margin-top: 12px; font-size: 0.8125rem; color: var(--fg-secondary);">
      {$t('mihomo.backup_notice')}
    </p>
  </div>
  <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
    <button class="btn btn-secondary" onclick={() => (showApplyConfirm = false)}>
      {$t('app.cancel')}
    </button>
    <button class="btn btn-primary" onclick={() => handleApplyMihomo(true)} disabled={applyLoading}>
      {applyLoading ? $t('editor.saving') : $t('editor.apply_and_restart')}
    </button>
  </div>
</Modal>

<style>
  .container {
    max-width: var(--container-max-width, 1400px);
    margin: 0 auto;
    padding: 16px;
  }

  .constructor-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 20px;
    gap: 16px;
  }

  .constructor-title {
    font-size: 1.5rem;
    font-weight: 700;
    margin: 0 0 4px 0;
    color: var(--fg-primary);
  }

  .constructor-sub {
    margin: 0;
    font-size: 13px;
    color: var(--fg-secondary);
  }

  .constructor-header-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .embedded-head-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid var(--border);
  }

  .safe-merge-card {
    margin-bottom: 16px;
    padding: 12px 16px;
  }

  .safe-merge-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .safe-merge-title-group {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .safe-merge-icon-wrap {
    color: var(--warning);
    display: flex;
    align-items: center;
  }

  .safe-merge-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .safe-merge-desc {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }

  .safe-merge-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 10px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  .directive-tag {
    background: var(--bg-surface);
    padding: 2px 6px;
    border-radius: 4px;
    font-size: var(--font-size-xs);
  }

  .btn-tag-expand {
    background: none;
    border: none;
    color: var(--primary);
    font-size: var(--font-size-xs);
    cursor: pointer;
    padding: 2px 4px;
  }

  .gen-layout {
    display: flex;
    gap: 16px;
    align-items: stretch;
  }

  @media (max-width: 1024px) {
    .gen-layout {
      flex-direction: column;
    }
  }

  .gen-left {
    flex: 1;
    min-width: 0;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .constructor-scenario-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
  }

  .scenario-select-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .preset-modified-chip {
    background: rgba(240, 180, 80, 0.15);
    color: var(--warning);
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 4px;
    font-weight: 500;
  }

  .rule-providers-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
  }

  .sec-tabs {
    display: flex;
    flex-wrap: wrap;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
  }

  .sec-tab {
    padding: 10px 16px;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-secondary);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    transition: all var(--transition-fast);
  }

  .sec-tab:hover {
    color: var(--fg-primary);
    background: var(--bg-hover);
  }

  .sec-tab.active {
    color: var(--primary);
    border-bottom-color: var(--primary);
    background: var(--bg-card);
  }

  .sec-count {
    background: var(--bg-surface);
    color: var(--fg-secondary);
    font-size: var(--font-size-xs);
    padding: 1px 6px;
    border-radius: 10px;
  }

  .tab-status-badge {
    font-size: var(--font-size-xs);
    padding: 1px 5px;
    border-radius: 4px;
    font-weight: 600;
  }

  .tab-status-badge.status-on {
    background: rgba(16, 185, 129, 0.15);
    color: var(--success);
  }

  .tab-status-badge.status-off {
    background: rgba(255, 255, 255, 0.05);
    color: var(--fg-dim);
  }

  .alert {
    padding: 10px 12px;
    border-radius: var(--radius-sm);
  }

  .alert-warning {
    background: rgba(240, 180, 80, 0.1);
    border: 1px solid rgba(240, 180, 80, 0.3);
    color: var(--warning);
  }

  .alert-dismissible {
    position: relative;
    padding-right: 32px;
  }

  .alert-close-btn {
    position: absolute;
    right: 8px;
    background: none;
    border: none;
    font-size: 16px;
    color: inherit;
    cursor: pointer;
    line-height: 1;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border-width: 0;
  }
</style>
