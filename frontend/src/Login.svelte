<script lang="ts">
  import { t } from './i18n';
  import { apiFetch } from './lib/api';
  import Button from './components/Button.svelte';
  import AuthLayout from './components/AuthLayout.svelte';

  let password = $state('');
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
        body: JSON.stringify({ password }),
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
  <div class="form-group" style="margin-bottom:14px;">
    <label class="form-label" for="password">{$t('auth.password')}</label>
    <input
      id="password"
      type="password"
      class="input"
      bind:value={password}
      onkeydown={handleKeydown}
      placeholder={$t('auth.enter_password')}
      disabled={loading}
      autocomplete="current-password"
      {@attach (node) => node.focus()}
    />
    {#if error}
      <div class="alert alert-error" style="margin-top:10px;margin-bottom:0;">{error}</div>
    {/if}
  </div>

  <Button variant="primary" class="login-btn" onclick={handleLogin} {loading}>
    {loading ? $t('auth.logging_in') : $t('auth.login_btn')}
  </Button>
</AuthLayout>
