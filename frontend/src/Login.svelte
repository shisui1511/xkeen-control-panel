<script lang="ts">
  import { t } from './i18n';
  import { apiFetch } from './lib/api';
  import { markAuthenticated } from './lib/authState';
  import { showToast } from './stores';
  import Button from './components/Button.svelte';
  import AuthLayout from './components/AuthLayout.svelte';
  import PasswordField from './components/PasswordField.svelte';
  import Icon from './lib/components/Icon.svelte';

  const RESET_PASSWORD_COMMAND = 'xcp --reset-password';

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

      // Switch to the dashboard in place (D-19) — no navigation, so the
      // current #/route and any restored drafts survive the re-login.
      markAuthenticated();
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

  async function copyResetCommand() {
    try {
      await navigator.clipboard.writeText(RESET_PASSWORD_COMMAND);
      showToast('success', $t('auth.forgot_password_copied'));
    } catch {
      showToast('error', $t('auth.forgot_password_copy_failed'));
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

  <details class="forgot-password">
    <summary>
      {$t('auth.forgot_password')}
      <Icon name="chevron-down" size={14} class="chevron" />
    </summary>
    <div class="forgot-password-content">
      <p class="forgot-password-instructions">{$t('auth.forgot_password_instructions')}</p>
      <div class="forgot-password-cmd">
        <code>{RESET_PASSWORD_COMMAND}</code>
        <button type="button" class="btn btn-secondary btn-sm" onclick={copyResetCommand}>
          <Icon name="copy" size={14} />
          {$t('auth.forgot_password_copy')}
        </button>
      </div>
    </div>
  </details>
</AuthLayout>

<style>
  .forgot-password {
    margin-top: 16px;
    text-align: center;
  }

  .forgot-password summary {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    list-style: none;
    cursor: pointer;
    font-size: var(--font-size-sm);
    color: var(--accent);
  }

  .forgot-password summary::-webkit-details-marker {
    display: none;
  }

  .forgot-password summary :global(.chevron) {
    transition: transform var(--transition-fast);
  }

  .forgot-password[open] summary :global(.chevron) {
    transform: rotate(180deg);
  }

  .forgot-password-content {
    margin-top: 10px;
    text-align: left;
  }

  .forgot-password-instructions {
    margin: 0 0 8px;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .forgot-password-cmd {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
  }

  .forgot-password-cmd code {
    font-family: var(--font-family-mono);
    font-size: 12px;
    padding: 0 4px;
    border-radius: var(--radius-xs);
    background: var(--surface-tint);
  }

  .btn-sm {
    padding: 6px 12px;
    font-size: 12px;
  }

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
