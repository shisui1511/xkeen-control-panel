<script lang="ts">
  import { t, currentLang } from '../../i18n';
  import { pluralize } from '../../i18n';
  import NodeList from './NodeList.svelte';

  interface Subscription {
    id: string;
    name: string;
    profile_title?: string;
    enabled: boolean;
    enable_xray: boolean;
    enable_mihomo: boolean;
    mihomo_integrated: boolean;
    hwid_locked: boolean;
    last_update: string;
    last_error?: string;
    proxy_count?: number;
    upload?: number;
    download?: number;
    total?: number;
    expire?: number;
    support_url?: string;
    announcement?: string;
  }

  interface Node {
    tag: string;
    name?: string;
    country?: string;
    flag?: string;
    active: boolean;
    use_case?: string;
    speed?: string;
    protocol?: string;
    transport?: string;
    security?: string;
    is_new?: boolean;
  }

  interface NodeHealth {
    alive: boolean;
    delay?: number;
    http_code?: number;
  }

  interface AnnouncementLine {
    isWarn: boolean;
    text: string;
  }

  interface Token {
    type: 'text' | 'bold' | 'italic' | 'link';
    value?: string;
    text?: string;
    url?: string;
  }

  interface ExpireDaysInfo {
    text: string;
    days: number | null;
    expired: boolean;
  }

  let {
    subscriptions = [],
    expandedSubs = {},
    refreshLoading = {},
    activeDropdownId = null,
    subNodesLoading = {},
    subNodes = {},
    subHealth = {},
    checkingNodes = {},
    devMode = false,
    stats,
    onToggleExpand,
    onRefreshSub,
    onEditSub,
    onDeleteSub,
    onOpenDiagnostic,
    onSetActiveNode,
    onCheckNodeHealth,
    onToggleDropdown
  }: {
    subscriptions: Subscription[];
    expandedSubs: Record<string, boolean>;
    refreshLoading: Record<string, boolean>;
    activeDropdownId: string | null;
    subNodesLoading: Record<string, boolean>;
    subNodes: Record<string, Node[]>;
    subHealth: Record<string, Record<string, NodeHealth>>;
    checkingNodes: Record<string, Record<string, boolean>>;
    devMode: boolean;
    stats: { total: number; nodes: number; next: string };
    onToggleExpand: (subId: string) => void;
    onRefreshSub: (subId: string) => void;
    onEditSub: (sub: Subscription) => void;
    onDeleteSub: (subId: string) => void;
    onOpenDiagnostic: (sub: Subscription) => void;
    onSetActiveNode: (subId: string, tag: string) => void;
    onCheckNodeHealth: (subId: string, tag: string) => void;
    onToggleDropdown: (subId: string) => void;
  } = $props();

  function isFormatError(err?: string): boolean {
    if (!err) return false;
    const lower = err.toLowerCase();
    return (
      lower.includes('format') ||
      lower.includes('invalid character') ||
      lower.includes('unexpected') ||
      lower.includes('yaml') ||
      lower.includes('json') ||
      lower.includes('base64')
    );
  }

  function getExpireDays(expire?: number): ExpireDaysInfo | null {
    if (!expire || expire <= 0) return null;
    const diff = expire * 1000 - Date.now();
    const isRu = $currentLang === 'ru';
    if (diff <= 0) {
      return {
        text: isRu ? 'Срок действия истек' : 'Expired',
        days: 0,
        expired: true
      };
    }
    const days = Math.ceil(diff / (1000 * 3600 * 24));
    if (isRu) {
      return {
        text: `Осталось ${days} ${pluralize(days, 'день', 'дня', 'дней')}`,
        days,
        expired: false
      };
    }
    return {
      text: `${days} ${days === 1 ? 'day' : 'days'} left`,
      days,
      expired: false
    };
  }

  function formatTraffic(bytes: number): string {
    if (bytes <= 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function formatDate(dateStr: string): string {
    if (!dateStr || dateStr.startsWith('0001')) return '—';
    try {
      const d = new Date(dateStr);
      return d.toLocaleString();
    } catch {
      return dateStr;
    }
  }

  function formatUpdateDate(dateStr: string): string {
    if (!dateStr || dateStr.startsWith('0001')) {
      return $currentLang === 'ru' ? 'Не обновлялось' : 'Never updated';
    }
    try {
      const d = new Date(dateStr);
      const now = new Date();
      const diffMs = now.getTime() - d.getTime();
      const diffMin = Math.floor(diffMs / 60000);
      const isRu = $currentLang === 'ru';

      if (diffMin < 1) return isRu ? 'только что' : 'just now';
      if (diffMin < 60) {
        return isRu
          ? `${diffMin} ${pluralize(diffMin, 'минуту', 'минуты', 'минут')} назад`
          : `${diffMin} ${diffMin === 1 ? 'min' : 'mins'} ago`;
      }
      const diffHours = Math.floor(diffMin / 60);
      if (diffHours < 24) {
        return isRu
          ? `${diffHours} ${pluralize(diffHours, 'час', 'часа', 'часов')} назад`
          : `${diffHours} ${diffHours === 1 ? 'hour' : 'hours'} ago`;
      }
      const diffDays = Math.floor(diffHours / 24);
      if (diffDays < 7) {
        return isRu
          ? `${diffDays} ${pluralize(diffDays, 'день', 'дня', 'дней')} назад`
          : `${diffDays} ${diffDays === 1 ? 'day' : 'days'} ago`;
      }

      return d.toLocaleDateString();
    } catch {
      return dateStr;
    }
  }

  function parseAnnouncementLines(text: string): AnnouncementLine[] {
    if (!text) return [];
    return text.split('\n').map((line) => {
      let trimmed = line.trim();
      let isWarn = false;
      if (trimmed.startsWith('!')) {
        isWarn = true;
        trimmed = trimmed.substring(1).trim();
      }
      return { isWarn, text: trimmed };
    });
  }

  function parseSimpleMarkdown(text: string): Token[] {
    const tokens: Token[] = [];
    let current = text;

    const boldRegex = /\*\*(.*?)\*\*/;
    const italicRegex = /\*(.*?)\*/;
    const linkRegex = /\[(.*?)\]\((.*?)\)/;

    while (current.length > 0) {
      let nearestIndex = Infinity;
      let nearestType: 'bold' | 'italic' | 'link' | null = null;
      let nearestMatch: RegExpExecArray | null = null;

      const boldMatch = boldRegex.exec(current);
      if (boldMatch && boldMatch.index < nearestIndex) {
        nearestIndex = boldMatch.index;
        nearestType = 'bold';
        nearestMatch = boldMatch;
      }

      const italicMatch = italicRegex.exec(current);
      if (italicMatch && italicMatch.index < nearestIndex) {
        nearestIndex = italicMatch.index;
        nearestType = 'italic';
        nearestMatch = italicMatch;
      }

      const linkMatch = linkRegex.exec(current);
      if (linkMatch && linkMatch.index < nearestIndex) {
        nearestIndex = linkMatch.index;
        nearestType = 'link';
        nearestMatch = linkMatch;
      }

      if (nearestType && nearestMatch) {
        if (nearestIndex > 0) {
          tokens.push({
            type: 'text',
            value: current.substring(0, nearestIndex)
          });
        }

        if (nearestType === 'bold') {
          tokens.push({ type: 'bold', value: nearestMatch[1] });
        } else if (nearestType === 'italic') {
          tokens.push({ type: 'italic', value: nearestMatch[1] });
        } else if (nearestType === 'link') {
          tokens.push({
            type: 'link',
            text: nearestMatch[1],
            url: nearestMatch[2]
          });
        }

        current = current.substring(nearestIndex + nearestMatch[0].length);
      } else {
        tokens.push({ type: 'text', value: current });
        break;
      }
    }

    return tokens;
  }
</script>

<div class="stats-chips-row mb-3">
  <span class="chip chip-default">
    {pluralize(
      stats.total,
      $t('subscr.total_one', { count: String(stats.total) }),
      $t('subscr.total_few', { count: String(stats.total) }),
      $t('subscr.total_many', { count: String(stats.total) })
    )}
  </span>
  <span class="chip chip-default">
    <b>{stats.nodes}</b>
    {$currentLang === 'ru' ? 'узлов суммарно' : 'nodes total'}
  </span>
  {#if stats.next !== '—'}
    <span class="chip chip-default chip--icon">
      <svg
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.5"
        class="timer-icon"
      >
        <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
      </svg>
      <span>
        {$currentLang === 'ru' ? 'след. обновление через' : 'next update in'}
        <b>{stats.next}</b>
      </span>
    </span>
  {/if}
</div>

<div class="subscriptions-list">
  {#each subscriptions as sub}
    {@const exp = getExpireDays(sub.expire)}
    <div class="card sub-card" id="sub-card-{sub.id}">
      <!-- Sub header row -->
      <div class="sub-header-row">
        <div class="sub-header-left">
          <button
            class="collapse-toggle"
            class:expanded={expandedSubs[sub.id]}
            onclick={() => onToggleExpand(sub.id)}
            aria-label="Toggle node list"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.5"
            >
              <polyline points="9 18 15 12 9 6" />
            </svg>
          </button>

          <div
            class="type-dot"
            class:mihomo={!sub.enable_xray && sub.enable_mihomo}
            class:both={sub.enable_xray && sub.enable_mihomo}
            class:disabled={!sub.enabled}
            class:has-error={!!sub.last_error}
            title={sub.last_error || (sub.enabled ? $t('app.active') : $t('app.disabled'))}
          ></div>

          <h2 class="sub-name" onclick={() => onToggleExpand(sub.id)}>
            {sub.profile_title || sub.name}
          </h2>
          {#if isFormatError(sub.last_error)}
            <span class="badge badge-error" style="margin-left: 8px;">
              {$currentLang === 'ru' ? 'Ошибка формата' : 'Format Error'}
            </span>
          {/if}
        </div>

        <div class="sub-header-right">
          <span
            class="sub-update-time"
            title={$t('subscr.updated_at').replace('{date}', formatDate(sub.last_update))}
          >
            {formatUpdateDate(sub.last_update)}
          </span>

          <span
            class="nodes-count-badge"
            onclick={() => onToggleExpand(sub.id)}
            title={$t('subscr.nodes_count').replace('{count}', String(sub.proxy_count || 0))}
          >
            {sub.proxy_count || 0}
          </span>

          <button
            class="action-icon-btn"
            onclick={() => onRefreshSub(sub.id)}
            disabled={refreshLoading[sub.id]}
            title={$t('subscr.refresh')}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.5"
              class:spinning={refreshLoading[sub.id]}
            >
              <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67" />
            </svg>
          </button>

          <button
            class="action-icon-btn"
            onclick={() => onEditSub(sub)}
            title={$t('app.edit')}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.5"
            >
              <circle cx="12" cy="12" r="3" /><path
                d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
              />
            </svg>
          </button>

          <div class="dropdown-container">
            <button
              class="action-icon-btn dots-btn"
              onclick={() => onToggleDropdown(sub.id)}
              aria-label="More actions"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
              >
                <circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="5" r="1.5" /><circle
                  cx="12"
                  cy="19"
                  r="1.5"
                />
              </svg>
            </button>
            {#if activeDropdownId === sub.id}
              <div class="dropdown-menu">
                {#if devMode}
                  <button
                    onclick={() => {
                      onOpenDiagnostic(sub);
                    }}>🔍 {$t('subscr.diag_btn')}</button
                  >
                {/if}
                <button
                  onclick={() => {
                    onDeleteSub(sub.id);
                  }}
                  class="delete-action">{$t('app.delete')}</button
                >
              </div>
            {/if}
          </div>
        </div>
      </div>

      {#if sub.last_error}
        <div class="sub-error-details" style="font-size: 12.5px; color: var(--danger); margin: -4px 0 8px 34px; line-height: 1.4; font-family: var(--font-family-sans);">
          {sub.last_error}
        </div>
      {/if}

      <!-- Meta Row -->
      <div class="sub-meta-row">
        <div class="sub-meta-left">
          {#if exp}
            <span
              class="expire-text"
              class:expired={exp.expired}
              class:warning={exp.days !== null && exp.days <= 5}
            >
              {exp.text}
            </span>
            <span class="meta-divider">|</span>
          {/if}

          <span class="sub-type-label">
            {#if sub.enable_xray && sub.enable_mihomo}
              XRay · Mihomo
            {:else if sub.enable_mihomo}
              Mihomo
            {:else}
              XRay
            {/if}
          </span>

          <span class="meta-divider">|</span>
          {#if sub.mihomo_integrated}
            <span
              class="mihomo-integrated-badge active"
              title="Интегрировано в Mihomo config.yaml">Mihomo ✓</span
            >
          {:else}
            <span class="mihomo-integrated-badge" title="Не интегрировано в Mihomo config.yaml"
              >Mihomo —</span
            >
          {/if}

          {#if sub.hwid_locked}
            <span class="meta-divider">|</span>
            <span class="hwid-locked-badge">⚠ HWID Locked</span>
          {/if}
        </div>

        <div class="sub-meta-right">
          <span class="traffic-text">
            {formatTraffic((sub.upload || 0) + (sub.download || 0))} / {sub.total &&
            sub.total > 0
              ? formatTraffic(sub.total)
              : '∞'}
          </span>
        </div>
      </div>

      <!-- Support / Announcement Row -->
      {#if sub.support_url || sub.announcement}
        <div class="sub-actions-row">
          {#if sub.support_url}
            <a
              href={sub.support_url}
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-support"
            >
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                class="support-icon"
              >
                <line x1="22" y1="2" x2="11" y2="13"></line>
                <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
              </svg>
              <span>{$currentLang === 'ru' ? 'Поддержка' : 'Support'}</span>
            </a>
          {/if}

          {#if sub.announcement}
            <div class="announcement-wrapper">
              <button class="btn btn-announcement">
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  class="announce-icon"
                >
                  <path
                    d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 0 1-3.46 0"
                  />
                </svg>
                <span>{$currentLang === 'ru' ? 'Объявление' : 'Announcement'}</span>
              </button>

              <div class="announcement-popover">
                {#each parseAnnouncementLines(sub.announcement) as line}
                  {#if line.isWarn}
                    <div class="inline-announcement-warn">
                      <span class="inline-warn-icon">!</span>
                      <span class="inline-warn-text">
                        {#each parseSimpleMarkdown(line.text) as token}
                          {#if token.type === 'text'}
                            {token.value}
                          {:else if token.type === 'bold'}
                            <strong>{token.value}</strong>
                          {:else if token.type === 'italic'}
                            <em>{token.value}</em>
                          {:else if token.type === 'link'}
                            <a href={token.url} target="_blank" rel="noopener noreferrer"
                              >{token.text}</a
                            >
                          {/if}
                        {/each}
                      </span>
                    </div>
                  {:else}
                    <div class="announcement-line">
                      {#each parseSimpleMarkdown(line.text) as token}
                        {#if token.type === 'text'}
                          {token.value}
                        {:else if token.type === 'bold'}
                          <strong>{token.value}</strong>
                        {:else if token.type === 'italic'}
                          <em>{token.value}</em>
                        {:else if token.type === 'link'}
                          <a href={token.url} target="_blank" rel="noopener noreferrer"
                            >{token.text}</a
                          >
                        {/if}
                      {/each}
                    </div>
                  {/if}
                {/each}
              </div>
            </div>
          {/if}
        </div>
      {/if}

      <!-- Node preview wrapper -->
      {#if expandedSubs[sub.id]}
        <div class="nodes-preview-content">
          {#if subNodesLoading[sub.id]}
            <div class="loading-nodes">
              <span class="spinner-xs"></span>
              <span style="margin-left: 8px;">{$t('app.loading')}</span>
            </div>
          {:else}
            {#if !subNodes[sub.id] || subNodes[sub.id].length === 0}
              <div class="empty-nodes">
                {$t('subscr.detail.no_nodes') || 'Нет узлов'}
              </div>
            {:else}
              <NodeList
                subId={sub.id}
                enableXray={sub.enable_xray}
                enableMihomo={sub.enable_mihomo}
                nodes={subNodes[sub.id]}
                health={subHealth[sub.id] || {}}
                checkingNodes={checkingNodes[sub.id] || {}}
                onSetActiveNode={onSetActiveNode}
                onCheckNodeHealth={onCheckNodeHealth}
              />
            {/if}
          {/if}
        </div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .subscriptions-list {
    display: grid;
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .sub-card {
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    position: relative;
  }

  /* Хедер карточки */
  .sub-header-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
  }

  .sub-header-left {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 1;
    min-width: 0;
  }

  /* Стрелочка */
  .collapse-toggle {
    background: transparent;
    border: none;
    padding: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    display: grid;
    place-items: center;
    border-radius: 4px;
    transition:
      color var(--transition-fast),
      background var(--transition-fast);
  }
  .collapse-toggle:hover {
    color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }
  .collapse-toggle svg {
    transition: transform var(--transition-fast);
  }
  .collapse-toggle.expanded svg {
    transform: rotate(90deg);
  }

  /* LED светодиод */
  .type-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 8px var(--accent);
    flex-shrink: 0;
    transition: all var(--transition-fast);
  }
  .type-dot.mihomo {
    background: #8b5cf6;
    box-shadow: 0 0 8px #8b5cf6;
  }
  .type-dot.both {
    background: linear-gradient(135deg, var(--accent), #8b5cf6);
    box-shadow: 0 0 8px #8b5cf6;
  }
  .type-dot.disabled {
    background: var(--fg-faint);
    box-shadow: none;
  }
  .type-dot.has-error {
    background: var(--danger);
    box-shadow: 0 0 8px var(--danger);
  }

  /* Имя */
  .sub-name {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--fg-primary);
    cursor: pointer;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family:
      var(--font-family-sans), 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol',
      'Noto Color Emoji', 'Android Emoji', EmojiSymbols, sans-serif;
  }
  .sub-name:hover {
    color: var(--accent);
  }

  /* Быстрая кнопка карандаша */
  .edit-icon-btn {
    background: transparent;
    border: none;
    padding: 4px;
    color: var(--fg-dim);
    cursor: pointer;
    border-radius: 4px;
    display: grid;
    place-items: center;
    opacity: 0;
    transition:
      opacity var(--transition-fast),
      color var(--transition-fast),
      background var(--transition-fast);
  }
  .sub-header-left:hover .edit-icon-btn,
  .edit-icon-btn:focus {
    opacity: 1;
  }
  .edit-icon-btn:hover {
    color: var(--accent);
    background: rgba(255, 255, 255, 0.04);
  }

  .sub-header-right {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }

  .sub-update-time {
    font-size: 12px;
    color: var(--fg-dim);
  }

  /* Синий чип количества нод */
  .nodes-count-badge {
    background: rgba(41, 194, 240, 0.1);
    border: 1px solid rgba(41, 194, 240, 0.25);
    color: var(--accent);
    padding: 2px 10px;
    border-radius: 12px;
    font-size: 11.5px;
    font-weight: 700;
    cursor: pointer;
    transition: all var(--transition-fast);
  }
  .nodes-count-badge:hover {
    background: rgba(41, 194, 240, 0.18);
    border-color: rgba(41, 194, 240, 0.45);
    box-shadow: 0 0 10px rgba(41, 194, 240, 0.2);
  }

  /* action кнопки-иконки */
  .action-icon-btn {
    background: transparent;
    border: none;
    padding: 6px;
    color: var(--fg-dim);
    cursor: pointer;
    border-radius: 6px;
    display: grid;
    place-items: center;
    transition: all var(--transition-fast);
  }
  .action-icon-btn:hover {
    color: var(--accent);
    background: var(--hover);
  }
  .action-icon-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .action-icon-btn svg.spinning {
    animation: rotate 1.5s linear infinite;
  }

  @keyframes rotate {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  /* Метаданные (Строка под заголовком) */
  .sub-meta-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    color: var(--fg-secondary);
    padding-bottom: 2px;
  }

  .sub-meta-left {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .meta-divider {
    color: var(--fg-faint);
    user-select: none;
  }

  .expire-text.warning {
    color: var(--warning);
  }
  .expire-text.expired {
    color: var(--danger);
  }

  .hwid-locked-badge {
    color: var(--warning);
    font-weight: 600;
  }

  .sub-type-label {
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.08em;
    font-weight: 700;
    color: var(--fg-dim);
  }

  .mihomo-integrated-badge {
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.08em;
    font-weight: 700;
    color: var(--fg-faint);
  }

  .mihomo-integrated-badge.active {
    color: var(--success);
  }

  .sub-meta-right {
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
  }

  /* Кнопки поддержки и объявления */
  .sub-actions-row {
    display: flex;
    gap: 10px;
    margin-top: 4px;
    align-items: center;
  }

  .btn-support {
    background: rgba(139, 92, 246, 0.12);
    border: 1px solid rgba(139, 92, 246, 0.25);
    color: #a78bfa;
    padding: 6px 14px;
    border-radius: 20px;
    font-size: 12px;
    font-weight: 600;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    transition: all var(--transition-fast);
  }
  .btn-support:hover {
    background: rgba(139, 92, 246, 0.22);
    border-color: rgba(139, 92, 246, 0.45);
    color: #c4b5fd;
    box-shadow: 0 0 10px rgba(139, 92, 246, 0.2);
  }

  .announcement-wrapper {
    position: relative;
    display: inline-block;
  }

  .btn-announcement {
    background: rgba(240, 180, 80, 0.1);
    border: 1px solid rgba(240, 180, 80, 0.25);
    color: #f3d9a6;
    padding: 6px 14px;
    border-radius: 20px;
    font-size: 12px;
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    cursor: pointer;
    transition: all var(--transition-fast);
  }
  .btn-announcement:hover {
    background: rgba(240, 180, 80, 0.2);
    border-color: rgba(240, 180, 80, 0.45);
    color: #fff;
    box-shadow: 0 0 10px rgba(240, 180, 80, 0.2);
  }

  /* Popover при ховере на объявление */
  .announcement-popover {
    position: absolute;
    top: calc(100% + 8px);
    left: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.6);
    padding: 16px;
    width: 380px;
    z-index: 250;
    opacity: 0;
    pointer-events: none;
    transform: translateY(6px);
    transition:
      opacity var(--transition-fast) ease,
      transform var(--transition-fast) ease;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .announcement-popover::before {
    content: '';
    position: absolute;
    bottom: 100%;
    left: 24px;
    border-width: 6px;
    border-style: solid;
    border-color: transparent transparent var(--border-strong) transparent;
  }
  .announcement-wrapper:hover .announcement-popover {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }

  .announcement-line {
    font-size: 12px;
    line-height: 1.5;
    color: var(--fg-primary);
  }
  .announcement-line a {
    color: var(--accent);
    text-decoration: none;
  }
  .announcement-line a:hover {
    text-decoration: underline;
  }

  .inline-announcement-warn {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    background: rgba(239, 68, 68, 0.05);
    border: 1px solid rgba(239, 68, 68, 0.15);
    border-radius: var(--radius-sm, 4px);
    padding: 8px 12px;
    margin-bottom: 12px;
    text-align: left;
  }

  .inline-warn-icon {
    color: var(--danger);
    font-weight: bold;
    font-size: 14px;
    line-height: 1;
    margin-top: 1px;
  }

  .inline-warn-text {
    font-size: 11.5px;
    color: var(--fg-secondary);
    line-height: 1.4;
    white-space: pre-wrap;
  }

  .inline-warn-text a {
    color: var(--accent);
    text-decoration: underline;
  }

  .inline-warn-text a:hover {
    text-decoration: none;
  }

  /* Раздел предпросмотра нод (Компактный инлайн-вид) */
  .nodes-preview-content {
    border-top: 1px solid var(--border);
    margin-top: 8px;
    padding-top: 16px;
    background: transparent;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .timer-icon {
    color: var(--fg-dim);
  }

  .timer-icon :global(polyline) {
    animation: clockRotate 4s linear infinite;
    transform-origin: 12px 12px;
  }

  @keyframes clockRotate {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  /* Dropdown Styles */
  .dropdown-container {
    position: relative;
    display: inline-block;
  }

  .action-btn-dots {
    height: 32px;
    width: 32px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
    background: transparent !important;
    border: none !important;
    color: var(--fg-secondary) !important;
    font-size: 14px;
    cursor: pointer;
  }
  .action-btn-dots:hover {
    color: var(--accent) !important;
    background: var(--hover) !important;
  }

  .dropdown-menu {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.3);
    z-index: 100;
    min-width: 140px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .dropdown-menu button {
    background: none;
    border: none;
    padding: 10px 14px;
    text-align: left;
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    width: 100%;
    transition: background var(--transition-fast);
  }

  .dropdown-menu button:hover {
    background: var(--hover);
  }

  .dropdown-menu button.delete-action {
    color: var(--danger);
  }

  .dropdown-menu button.delete-action:hover {
    background: rgba(235, 94, 85, 0.1);
  }

  @media (max-width: 768px) {
    .sub-header-row {
      flex-wrap: wrap;
      gap: 12px;
    }
    .sub-header-left {
      width: 100%;
    }
    .sub-header-right {
      width: 100%;
      justify-content: flex-end;
    }
    .sub-meta-row {
      flex-wrap: wrap;
      gap: 8px;
    }
    .announcement-popover {
      width: calc(100vw - 64px);
      max-width: 340px;
      left: -20px;
    }
    .announcement-popover::before {
      left: 50px;
    }
  }
</style>
