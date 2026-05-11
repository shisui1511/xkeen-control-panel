import { mount } from 'svelte'
import App from './App.svelte'

function initTheme() {
  let saved = ''
  try {
    saved = localStorage.getItem('theme') || ''
  } catch (e) {
    // localStorage may be unavailable in private mode or with blocked cookies
  }
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const theme = saved || (prefersDark ? 'dark' : 'light')
  document.documentElement.setAttribute('data-theme', theme)
}
initTheme()

const app = mount(App, {
  target: document.getElementById('app')!
})

export default app
