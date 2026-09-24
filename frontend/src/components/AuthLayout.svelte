<script lang="ts">
  import { onMount } from 'svelte';
  import type { Snippet } from 'svelte';
  import { apiFetch } from '../lib/api';

  // Общий каркас экранов входа и первичной настройки: фон, карточка,
  // фирменный блок и подвал с версией панели.
  let { children }: { children: Snippet } = $props();

  let version = $state('');

  onMount(async () => {
    try {
      const res = await apiFetch('/api/version', { skip401Redirect: true });
      const data = await res.json();
      version = data.panel_version || '';
    } catch {
      // версия необязательна
    }
  });
</script>

<div class="login-screen">
  <div class="login-card">
    <!-- Brand block — same visual DNA as sidebar -->
    <div class="login-brand">
      <span class="brand-mark" aria-hidden="true">
        <!-- Cross-routing X logo: 4 endpoint nodes + crossed paths + central hub -->
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <circle cx="4" cy="4" r="1.8" fill="currentColor" />
          <circle cx="20" cy="4" r="1.8" fill="currentColor" />
          <circle cx="4" cy="20" r="1.8" fill="currentColor" />
          <circle cx="20" cy="20" r="1.8" fill="currentColor" />
          <path
            d="M5.4 5.4 L18.6 18.6"
            stroke="currentColor"
            stroke-width="2.4"
            stroke-linecap="round"
          />
          <path
            d="M18.6 5.4 L5.4 18.6"
            stroke="currentColor"
            stroke-width="2.4"
            stroke-linecap="round"
          />
          <circle cx="12" cy="12" r="2.8" fill="currentColor" />
          <circle cx="12" cy="12" r="1.1" fill="var(--bg-deep, #07182a)" />
        </svg>
      </span>
      <div class="brand-names">
        <div class="b1"><span class="x">X</span>Keen</div>
        <div class="b2">Control&nbsp;Panel</div>
      </div>
    </div>

    {@render children()}

    <div class="login-footer">
      <span>{version}</span>
      <span>{window.location.hostname}</span>
    </div>
  </div>
</div>

<style>
  /* Кнопка отправки формы на всю ширину карточки */
  .login-card :global(.login-btn) {
    width: 100%;
    min-height: var(--btn-h);
  }

  /* Full-page centred layout */
  .login-screen {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-page);
  }

  /* Card */
  .login-card {
    width: 100%;
    max-width: 420px;
    background: linear-gradient(180deg, var(--surface-overlay-from), var(--surface-overlay-to));
    border: 1px solid rgba(41, 194, 240, 0.14);
    border-radius: var(--radius-lg);
    padding: 38px 36px 32px;
    box-shadow:
      0 48px 80px -32px rgba(0, 0, 0, 0.75),
      0 0 0 1px rgba(255, 255, 255, 0.025) inset,
      0 0 40px -20px rgba(41, 194, 240, 0.15);
  }

  /* Brand block */
  .login-brand {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    margin-bottom: 30px;
  }

  .login-brand .brand-mark {
    width: 58px;
    height: 58px;
    border-radius: 13px;
    display: grid;
    place-items: center;
    background: linear-gradient(
      135deg,
      var(--accent) 0%,
      var(--accent-2) 60%,
      color-mix(in srgb, var(--accent-2) 70%, black) 100%
    );
    box-shadow:
      0 0 0 1px color-mix(in srgb, var(--accent) 30%, transparent),
      0 14px 36px -12px color-mix(in srgb, var(--accent) 70%, transparent);
    color: var(--btn-primary-text);
  }

  .login-brand .brand-names {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
  }

  /* "XKeen" — mirrors sidebar .b1 */
  .login-brand .b1 {
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -0.01em;
    color: var(--fg-primary);
    line-height: 1;
  }
  .login-brand .b1 :global(.x),
  .login-brand .b1 .x {
    color: var(--accent);
    font-weight: 800;
    text-shadow: 0 0 16px color-mix(in srgb, var(--accent) 50%, transparent);
  }

  /* "Control Panel" — mirrors sidebar .b2 */
  .login-brand .b2 {
    font-size: var(--font-size-xs);
    letter-spacing: 0.05em;
    color: var(--fg-dim);
    font-weight: 600;
  }

  .login-footer {
    display: flex;
    justify-content: space-between;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    margin-top: 20px;
    padding-top: 18px;
    border-top: 1px solid var(--border);
  }
</style>
