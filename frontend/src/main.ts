import App from './App.svelte'

function initTheme() {
  const saved = localStorage.getItem('theme')
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const theme = saved || (prefersDark ? 'dark' : 'light')
  document.documentElement.setAttribute('data-theme', theme)
}
initTheme()

const app = new App({
  target: document.getElementById('app')!
})

export default app
