<script lang="ts">
  let password = ''
  let error = ''
  let loading = false

  async function handleLogin() {
    if (!password) {
      error = 'Введите пароль'
      return
    }

    loading = true
    error = ''

    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      })

      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || 'Ошибка входа')
      }

      const data = await res.json()
      localStorage.setItem('csrf_token', data.csrf_token)
      
      // Redirect to dashboard
      window.location.href = '/'
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      handleLogin()
    }
  }
</script>

<div class="center-container">
  <div class="card" style="width: 100%; max-width: 400px;">
    <h1 class="text-center">⚡ XKeen Control Panel</h1>
    <p class="text-center text-secondary mb-3">Вход в панель управления</p>

    {#if error}
      <div class="alert alert-error">{error}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="password">Пароль</label>
      <input
        id="password"
        type="password"
        class="input"
        bind:value={password}
        on:keydown={handleKeydown}
        placeholder="Введите пароль"
        disabled={loading}
      />
    </div>

    <button
      class="btn btn-primary"
      style="width: 100%;"
      on:click={handleLogin}
      disabled={loading}
    >
      {loading ? 'Вход...' : 'Войти'}
    </button>
  </div>
</div>
