<script lang="ts">
  import { t } from './i18n';
  import { apiFetch } from './lib/api';
  import Button from './components/Button.svelte';
  import AuthLayout from './components/AuthLayout.svelte';
  import PasswordField from './components/PasswordField.svelte';
  import { validatePasswordPolicy, policyErrorKey } from './lib/passwordPolicy';

  const SETUP_CODE_COMMAND = 'xcp --setup-code';
  const RESET_PASSWORD_COMMAND = 'xcp --reset-password';

  let setupCode = $state('');
  let password = $state('');
  let confirmPassword = $state('');
  let error = $state('');
  let loading = $state(false);

  async function handleSetup(e: SubmitEvent) {
    e.preventDefault();
    error = '';

    if (!setupCode || !password || !confirmPassword) {
      error = $t('auth.fill_all');
      return;
    }

    const policyCode = validatePasswordPolicy(password);
    if (policyCode) {
      error = $t(policyErrorKey(policyCode) ?? 'auth.setup_error');
      return;
    }

    if (password !== confirmPassword) {
      error = $t('auth.password_mismatch');
      return;
    }

    loading = true;

    try {
      const res = await apiFetch('/api/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password, setup_code: setupCode.trim() }),
        skip401Redirect: true
      });

      if (!res.ok) {
        // Пароль уже задан (другая вкладка или повторная отправка) —
        // корень покажет форму входа.
        if (res.status === 403) {
          window.location.href = '/';
          return;
        }
        let payload: any = null;
        try {
          payload = await res.json();
        } catch {
          // тело ответа не JSON
        }
        if (res.status === 429 && payload?.retry_after) {
          throw new Error($t('auth.too_many_attempts', { seconds: String(payload.retry_after) }));
        }
        const key = policyErrorKey(payload?.code);
        if (key) {
          throw new Error($t(key));
        }
        throw new Error(payload?.error || $t('auth.setup_error'));
      }

      // После успешной установки — автоматический вход. Если он не удался,
      // пароль всё равно задан: корень покажет форму входа.
      const loginRes = await apiFetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
        skip401Redirect: true
      });

      if (loginRes.ok) {
        const data = await loginRes.json();
        localStorage.setItem('csrf_token', data.csrf_token);
      }
      window.location.href = '/';
    } catch (e: any) {
      error = e?.message || $t('auth.setup_error');
    } finally {
      loading = false;
    }
  }
</script>

<AuthLayout>
  <form onsubmit={handleSetup} novalidate>
    <h1 class="setup-title">{$t('auth.setup_title')}</h1>
    <p class="setup-desc">{$t('auth.setup_desc')}</p>

    <div class="form-group">
      <label class="form-label" for="setup-code">{$t('auth.setup_code_label')}</label>
      <input
        id="setup-code"
        type="text"
        class="input setup-code-input"
        bind:value={setupCode}
        placeholder={$t('auth.setup_code_placeholder')}
        disabled={loading}
        autocomplete="one-time-code"
        {@attach (node) => node.focus()}
      />
      <p class="setup-desc setup-hint">
        {$t('auth.setup_code_hint').split('{command}')[0]}<code>{SETUP_CODE_COMMAND}</code>{$t(
          'auth.setup_code_hint'
        ).split('{command}')[1]}
      </p>
      <p class="setup-desc setup-hint">
        {$t('auth.setup_reset_hint').split('{command}')[0]}<code>{RESET_PASSWORD_COMMAND}</code>{$t(
          'auth.setup_reset_hint'
        ).split('{command}')[1]}
      </p>
    </div>

    <div class="form-group">
      <label class="form-label" for="password">{$t('auth.password')}</label>
      <PasswordField
        id="password"
        bind:value={password}
        placeholder={$t('auth.password_min')}
        disabled={loading}
        autocomplete="new-password"
      />
      <p class="setup-desc setup-hint">{$t('auth.password_policy_hint')}</p>
    </div>

    <div class="form-group">
      <label class="form-label" for="confirm">{$t('auth.confirm_password')}</label>
      <PasswordField
        id="confirm"
        bind:value={confirmPassword}
        placeholder={$t('auth.repeat_password')}
        disabled={loading}
        autocomplete="new-password"
      />
    </div>

    {#if error}
      <div class="alert alert-error" role="alert">{error}</div>
    {/if}

    <Button type="submit" variant="primary" class="login-btn" {loading}>
      {loading ? $t('auth.setting_up') : $t('auth.setup_btn')}
    </Button>
  </form>
</AuthLayout>

<style>
  .setup-title {
    margin: 0 0 6px;
    font-size: var(--font-size-lg);
    font-weight: 600;
    text-align: center;
    color: var(--fg-primary);
  }

  .setup-desc {
    margin: 0 0 22px;
    font-size: var(--font-size-sm);
    text-align: center;
    color: var(--fg-dim);
  }

  .setup-code-input {
    font-family: var(--font-family-mono);
    letter-spacing: 0.08em;
  }

  .setup-hint {
    margin: 6px 0 0;
    font-size: var(--font-size-xs);
    text-align: left;
    overflow-wrap: anywhere;
  }

  .setup-hint code {
    font-family: var(--font-family-mono);
    font-size: 12px;
    padding: 0 4px;
    border-radius: var(--radius-xs);
    background: var(--surface-tint);
    overflow-wrap: anywhere;
  }
</style>
