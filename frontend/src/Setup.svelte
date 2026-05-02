<script lang="ts">
  let password = ''
  let confirmPassword = ''
  let error = ''
  let loading = false

  async function handleSetup() {
    error = ''

    if (!password || !confirmPassword) {
      error = 'Заполните все поля'
      return
    }

    if (password.length < 8) {
      error = 'Пароль должен быть не менее 8 символов'
      return
    }

    if (password !== confirmPassword) {
      error = 'Пароли не совпадают'
      return
    }

    loading = true

    try {
      const res = await fetch('/api/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      })

      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || 'Ошибка установки пароля')
      }

      // После успешной установки — автоматический вход
      const loginRes = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      })

      if (loginRes.ok) {
        const data = await loginRes.json()
        localStorage.setItem('csrf_token', data.csrf_token)
        window.location.href = '/'
      }
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }
</script>

<div class="center-container">
  <div class="card" style="width: 100%; max-width: 400px;">
    <h1 class="text-center">🔐 Первичная настройка</h1>
    <p class="text-center text-secondary mb-3">
      Установите пароль для доступа к панели управления
    </p>

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
        placeholder="Минимум 8 символов"
        disabled={loading}
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="confirm">Подтвердите пароль</label>
      <input
        id="confirm"
        type="password"
        class="input"
        bind:value={confirmPassword}
        placeholder="Повторите пароль"
        disabled={loading}
      />
    </div>

    <button
      class="btn btn-primary"
      style="width: 100%;"
      on:click={handleSetup}
      disabled={loading}
    >
      {loading ? 'Установка...' : 'Установить пароль'}
    </button>
  </div>
</div>
