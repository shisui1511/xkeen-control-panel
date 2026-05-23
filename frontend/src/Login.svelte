<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from './i18n';

  let password = '';
  let error = '';
  let loading = false;
  let version = '';

  async function fetchVersion() {
    try {
      const res = await fetch('/api/version');
      const data = await res.json();
      version = data.version || '';
    } catch (e) {
      // ignore
    }
  }

  async function handleLogin() {
    if (!password) {
      error = $t('auth.enter_password');
      return;
    }

    loading = true;
    error = '';

    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      });

      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || $t('auth.login_error'));
      }

      const data = await res.json();
      localStorage.setItem('csrf_token', data.csrf_token);

      // Redirect to dashboard
      window.location.href = '/';
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      handleLogin();
    }
  }

  onMount(fetchVersion);
</script>

<div class="login-screen">
  <div class="login-card">

    <!-- Brand block — same visual DNA as sidebar -->
    <div class="login-brand">
      <span class="brand-mark" aria-hidden="true">
        <!-- Cross-routing X logo: 4 endpoint nodes + crossed paths + central hub -->
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <circle cx="4"  cy="4"  r="1.8" fill="currentColor"/>
          <circle cx="20" cy="4"  r="1.8" fill="currentColor"/>
          <circle cx="4"  cy="20" r="1.8" fill="currentColor"/>
          <circle cx="20" cy="20" r="1.8" fill="currentColor"/>
          <path d="M5.4 5.4 L18.6 18.6" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>
          <path d="M18.6 5.4 L5.4 18.6" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>
          <circle cx="12" cy="12" r="2.8" fill="currentColor"/>
          <circle cx="12" cy="12" r="1.1" fill="var(--bg-deep, #07182a)"/>
        </svg>
      </span>
      <div class="brand-names">
        <div class="b1"><span class="x">X</span>Keen</div>
        <div class="b2">Control&nbsp;Panel</div>
      </div>
    </div>

    {#if error}
      <div class="alert alert-error" style="margin-bottom:18px;">{error}</div>
    {/if}

    <div class="form-group" style="margin-bottom:14px;">
      <label class="form-label" for="password">{$t('auth.password')}</label>
      <input
        id="password"
        type="password"
        class="input"
        bind:value={password}
        on:keydown={handleKeydown}
        placeholder={$t('auth.enter_password')}
        disabled={loading}
        autocomplete="current-password"
      />
    </div>

    <button
      class="btn btn-primary"
      style="width:100%;padding:11px 14px;font-size:13.5px;"
      on:click={handleLogin}
      disabled={loading}
    >
      {loading ? $t('auth.logging_in') : $t('auth.login_btn')}
    </button>

    <div class="login-footer">
      <span>{version}</span>
      <span>{window.location.hostname}</span>
    </div>

  </div>
</div>

<style>
  /* Full-page centred layout */
  .login-screen {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background:
      radial-gradient(ellipse 80% 55% at 50% 20%, rgba(41,194,240,.07), transparent 65%),
      var(--bg-page);
  }

  /* Card */
  .login-card {
    width: 100%;
    max-width: 420px;
    background: linear-gradient(180deg, #0d2438 0%, #09192b 100%);
    border: 1px solid rgba(41,194,240,.14);
    border-radius: var(--radius-lg);
    padding: 38px 36px 32px;
    box-shadow:
      0 48px 80px -32px rgba(0,0,0,.75),
      0 0 0 1px rgba(255,255,255,.025) inset,
      0 0 40px -20px rgba(41,194,240,.15);
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
    width: 58px; height: 58px;
    border-radius: 13px;
    display: grid; place-items: center;
    background: linear-gradient(135deg, var(--accent) 0%, var(--accent-2) 60%, #0e6f96 100%);
    box-shadow:
      0 0 0 1px rgba(41,194,240,.3),
      0 14px 36px -12px rgba(41,194,240,.7);
    color: #03182a;
  }

  .login-brand .brand-names {
    display: flex; flex-direction: column;
    align-items: center; gap: 4px;
  }

  /* "XKeen" — mirrors sidebar .b1 */
  .login-brand .b1 {
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -.01em;
    color: #fff;
    line-height: 1;
  }
  .login-brand .b1 :global(.x),
  .login-brand .b1 .x {
    color: var(--accent);
    font-weight: 800;
    text-shadow: 0 0 16px rgba(41,194,240,.5);
  }

  /* "Control Panel" — mirrors sidebar .b2 */
  .login-brand .b2 {
    font-size: 10px;
    letter-spacing: .22em;
    text-transform: uppercase;
    color: var(--fg-dim);
    font-weight: 600;
  }

  .login-footer {
    display: flex;
    justify-content: space-between;
    font-size: 11px;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    margin-top: 20px;
    padding-top: 18px;
    border-top: 1px solid var(--border);
  }
</style>
