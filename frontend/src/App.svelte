<script lang="ts">
  import { onMount } from 'svelte';
  import { t, i18nReady } from './i18n';
  import { apiFetch } from './lib/api';
  import { authStatus } from './lib/authState';
  import Login from './Login.svelte';
  import Setup from './Setup.svelte';
  import Dashboard from './Dashboard.svelte';
  import Toast from './components/Toast.svelte';
  import './styles/global.css';

  let authError = $state('');

  async function checkAuth() {
    await i18nReady;
    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 10000);
      const res = await apiFetch('/api/auth/me', {
        signal: controller.signal,
        skip401Redirect: true
      });
      clearTimeout(timeoutId);

      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }

      const data = await res.json();

      // Сохранить CSRF-токен при автологине через checkAuth (при перезагрузке страницы)
      if (data.csrf_token) {
        localStorage.setItem('csrf_token', data.csrf_token);
      }

      if (data.setup_required) {
        authStatus.set('setup');
      } else if (data.authenticated) {
        authStatus.set('authenticated');
      } else {
        authStatus.set('login');
      }
    } catch (e: any) {
      authStatus.set('error');
      authError = e.name === 'AbortError' ? 'Request timeout' : e.message || 'Network error';
    }
  }

  onMount(() => {
    checkAuth();
  });
</script>

{#if $authStatus === 'loading'}
  <div class="center-container">
    <div class="card">
      <p>{$t('app.loading')}</p>
    </div>
  </div>
{:else if authError}
  <div class="center-container">
    <div class="card">
      <h1 class="text-center">{$t('app.conn_error')}</h1>
      <p class="text-center text-secondary mb-3">{authError}</p>
      <p class="text-center text-secondary">{$t('app.conn_error_desc')}</p>
      <button
        class="btn btn-primary"
        style="width: 100%; margin-top: 1rem;"
        onclick={() => location.reload()}
      >
        {$t('app.reload')}
      </button>
    </div>
  </div>
{:else if $authStatus === 'setup'}
  <Setup />
{:else if $authStatus === 'login'}
  <Login />
{:else}
  <Dashboard />
{/if}

<Toast />
