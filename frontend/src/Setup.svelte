<script lang="ts">
  import { t } from './i18n';

  let password = '';
  let confirmPassword = '';
  let error = '';
  let loading = false;

  async function handleSetup() {
    error = '';

    if (!password || !confirmPassword) {
      error = $t('auth.fill_all');
      return;
    }

    if (password.length < 8) {
      error = $t('auth.password_short');
      return;
    }

    if (password !== confirmPassword) {
      error = $t('auth.password_mismatch');
      return;
    }

    loading = true;

    try {
      const res = await fetch('/api/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      });

      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || $t('auth.setup_error'));
      }

      // После успешной установки — автоматический вход
      const loginRes = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      });

      if (loginRes.ok) {
        const data = await loginRes.json();
        localStorage.setItem('csrf_token', data.csrf_token);
        window.location.href = '/';
      }
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }
</script>

<div class="center-container">
  <div class="card" style="width: 100%; max-width: 400px;">
    <h1 class="text-center">{$t('auth.setup_title')}</h1>
    <p class="text-center text-secondary mb-3">
      {$t('auth.setup_desc')}
    </p>

    {#if error}
      <div class="alert alert-error">{error}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="password">{$t('auth.password')}</label>
      <input
        id="password"
        type="password"
        class="input"
        bind:value={password}
        placeholder={$t('auth.password_min')}
        disabled={loading}
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="confirm">{$t('auth.confirm_password')}</label>
      <input
        id="confirm"
        type="password"
        class="input"
        bind:value={confirmPassword}
        placeholder={$t('auth.repeat_password')}
        disabled={loading}
      />
    </div>

    <button class="btn btn-primary" style="width: 100%;" on:click={handleSetup} disabled={loading}>
      {loading ? $t('auth.setting_up') : $t('auth.setup_btn')}
    </button>
  </div>
</div>
