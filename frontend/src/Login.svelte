<script lang="ts">
  import { t } from './i18n';
  import { apiFetch } from './lib/api';
  import Button from './components/Button.svelte';
  import AuthLayout from './components/AuthLayout.svelte';
  import PasswordField from './components/PasswordField.svelte';

  let password = $state('');
  let rememberMe = $state(false);
  let error = $state('');
  let loading = $state(false);
  async function handleLogin() {
    if (!password) {
      error = $t('auth.enter_password');
      return;
    }

    loading = true;
    error = '';

    try {
      const res = await apiFetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password, remember_me: rememberMe }),
        skip401Redirect: true
      });

      if (!res.ok) {
        let payload: any = null;
        try {
          payload = await res.json();
        } catch (e) {
          // response body wasn't JSON
        }

        if (res.status === 429) {
          const seconds = payload?.retry_after;
          throw new Error(
            seconds
              ? $t('auth.too_many_attempts', { seconds: String(seconds) })
              : $t('auth.login_error')
          );
        }

        throw new Error($t('auth.login_error'));
      }

      const data = await res.json();
      localStorage.setItem('csrf_token', data.csrf_token);

      // Redirect to dashboard
      window.location.href = '/';
    } catch (e: any) {
      if (e?.status === 401) {
        error = $t('auth.invalid_password');
      } else {
        error = e.message;
      }
    } finally {
      loading = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      handleLogin();
    }
  }
</script>

<AuthLayout>
  <div class="form-group">
    <label class="form-label" for="password">{$t('auth.password')}</label>
    <PasswordField
      id="password"
      bind:value={password}
      onkeydown={handleKeydown}
      placeholder={$t('auth.enter_password')}
      disabled={loading}
      autocomplete="current-password"
      autofocus
    />
    <label class="remember-me">
      <input type="checkbox" bind:checked={rememberMe} />
      {$t('auth.remember_me')}
    </label>
    {#if error}
      <div class="alert alert-error">{error}</div>
    {/if}
  </div>

  <Button variant="primary" class="login-btn" onclick={handleLogin} {loading} disabled={!password}>
    {loading ? $t('auth.logging_in') : $t('auth.login_btn')}
  </Button>
</AuthLayout>

<style>
  .remember-me {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .remember-me input[type='checkbox'] {
    accent-color: var(--accent);
    cursor: pointer;
  }

  .alert-error {
    margin-top: 10px;
    margin-bottom: 0;
  }
</style>
