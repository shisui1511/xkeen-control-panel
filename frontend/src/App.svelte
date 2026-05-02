<script lang="ts">
  let version = 'loading...'
  
  async function fetchVersion() {
    try {
      const res = await fetch('/api/version')
      const data = await res.json()
      version = data.version
    } catch (e) {
      version = 'error'
    }
  }
  
  fetchVersion()
</script>

<main>
  <h1>⚡ XKeen Control Panel</h1>
  <p>Version: {version}</p>
  <p>Vite + Svelte + TypeScript работает!</p>
</main>

<style>
  main {
    text-align: center;
    padding: 2rem;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  }
  
  h1 {
    color: #333;
    margin-bottom: 1rem;
  }
</style>
