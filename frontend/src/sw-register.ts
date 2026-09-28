// Регистрация service worker вынесена из inline-<script> в index.html
// (134-REVIEW WR-01): CSP script-src допускал 'unsafe-inline' только ради
// этого сниппета, что отключает главную анти-XSS защиту CSP для всего
// приложения. Как обычный ES-модуль, подключённый через
// <script type="module" src="/src/main.ts">, этот код уже покрывается
// script-src 'self' и не требует послаблений политики.
export function registerServiceWorker(): void {
  if ('serviceWorker' in navigator && navigator.serviceWorker) {
    navigator.serviceWorker.register('./sw.js').catch(() => {});
  }
}
