<script lang="ts">
  import { onMount } from 'svelte'
  import Login from './Login.svelte'
  import Setup from './Setup.svelte'
  import Dashboard from './Dashboard.svelte'
  import './styles/global.css'

  let authenticated = false
  let setupRequired = false
  let loading = true

  async function checkAuth() {
    try {
      const res = await fetch('/api/auth/me')
      const data = await res.json()
      
      authenticated = data.authenticated || false
      setupRequired = data.setup_required || false
    } catch (e) {
      authenticated = false
      setupRequired = false
    } finally {
      loading = false
    }
  }

  onMount(() => {
    checkAuth()
  })
</script>

{#if loading}
  <div class="center-container">
    <div class="card">
      <p>Загрузка...</p>
    </div>
  </div>
{:else if setupRequired}
  <Setup />
{:else if !authenticated}
  <Login />
{:else}
  <Dashboard />
{/if}
