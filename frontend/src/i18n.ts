import { writable, derived } from 'svelte/store'

// Available languages
export type Lang = 'ru' | 'en'

// Translation dictionaries
const translations: Record<Lang, Record<string, string>> = {
  ru: {
    // Common
    'app.name': 'XKeen Control Panel',
    'app.loading': 'Загрузка...',
    'app.version': 'Версия',
    'app.status': 'Статус',
    'app.running': 'Работает',
    'app.error': 'Ошибка',
    'app.unavailable': 'Недоступно',
    'app.save': 'Сохранить',
    'app.cancel': 'Отмена',
    'app.create': 'Создать',
    'app.delete': 'Удалить',
    'app.rename': 'Переименовать',
    'app.refresh': 'Обновить',
    'app.start': 'Запустить',
    'app.stop': 'Остановить',
    'app.restart': 'Перезапустить',
    'app.search': 'Поиск',
    'app.close': 'Закрыть',
    'app.apply': 'Применить',
    'app.confirm': 'Подтвердить',
    'app.back': 'Назад',
    'app.continue': 'Продолжить',
    'app.yes': 'Да',
    'app.no': 'Нет',

    // Auth
    'auth.login': 'Вход в панель управления',
    'auth.password': 'Пароль',
    'auth.enter_password': 'Введите пароль',
    'auth.login_btn': 'Войти',
    'auth.logging_in': 'Вход...',
    'auth.login_error': 'Ошибка входа',
    'auth.setup_title': 'Первичная настройка',
    'auth.setup_desc': 'Установите пароль для доступа к панели управления',
    'auth.password_min': 'Минимум 8 символов',
    'auth.confirm_password': 'Подтвердите пароль',
    'auth.repeat_password': 'Повторите пароль',
    'auth.setup_btn': 'Установить пароль',
    'auth.setting_up': 'Установка...',
    'auth.setup_error': 'Ошибка установки пароля',
    'auth.fill_all': 'Заполните все поля',
    'auth.password_short': 'Пароль должен быть не менее 8 символов',
    'auth.password_mismatch': 'Пароли не совпадают',
    'auth.logout': 'Выйти',
    'auth.logging_out': 'Выход...',

    // Navigation
    'nav.dashboard': 'Dashboard',
    'nav.editor': 'Редактор',
    'nav.logs': 'Логи',
    'nav.proxies': 'Прокси',
    'nav.connections': 'Подключения',
    'nav.rules': 'Правила',
    'nav.traffic': 'Трафик',
    'nav.services': 'Сервисы',
    'nav.settings': 'Настройки',
    'nav.theme_light': 'Светлая тема',
    'nav.theme_dark': 'Тёмная тема',

    // Dashboard
    'dash.title': 'Dashboard',
    'dash.welcome': 'Добро пожаловать в панель управления XKeen',
    'dash.system_info': 'Информация о системе',
    'dash.system_stats': 'Системные ресурсы',
    'dash.releases': 'Релизы',
    'dash.ram': 'RAM',
    'dash.load': 'Load Average',
    'dash.uptime': 'Uptime',
    'dash.goroutines': 'Go Goroutines',

    // Editor
    'editor.title': 'Управление сервисами',
    'editor.subtitle': 'Запуск, остановка и перезапуск XKeen и Mihomo',
    'editor.configs': 'Конфиги',
    'editor.backups': 'Backups',
    'editor.select_file': 'Выберите файл для редактирования',
    'editor.file_saved': '✓ Файл сохранён',
    'editor.backup_restored': '✓ Backup восстановлен (не забудьте сохранить)',
    'editor.create_file': 'Создать файл',
    'editor.delete_file': 'Удалить файл',
    'editor.rename_file': 'Переименовать файл',
    'editor.file_name': 'имя_файла.json',
    'editor.new_name': 'новое_имя.json',
    'editor.load_error': 'Ошибка загрузки списка файлов',
    'editor.file_load_error': 'Ошибка загрузки файла',
    'editor.save_error': 'Ошибка сохранения',
    'editor.create_error': 'Ошибка создания',
    'editor.delete_error': 'Ошибка удаления',
    'editor.rename_error': 'Ошибка переименования',
    'editor.restore_error': 'Ошибка восстановления',

    // Services
    'svc.title': 'Управление сервисами',
    'svc.subtitle': 'Запуск, остановка и перезапуск XKeen и Mihomo',
    'svc.xkeen': 'XKeen (Xray)',
    'svc.xkeen_desc': 'Основной прокси-сервис на базе Xray-core',
    'svc.mihomo': 'Mihomo (Clash)',
    'svc.mihomo_desc': 'Альтернативный прокси на базе Mihomo (Clash.Meta)',
    'svc.starting': 'Запуск...',
    'svc.stopping': 'Остановка...',
    'svc.restarting': 'Перезапуск...',
    'svc.refresh_status': 'Обновить статус',
    'svc.action_error': 'Ошибка',

    // Settings
    'settings.title': 'Настройки',
    'settings.subtitle': 'Информация о панели управления',
    'settings.about': 'О системе',
    'settings.version': 'Версия',
    'settings.frontend': 'Frontend',
    'settings.backend': 'Backend',
    'settings.security': 'Безопасность',
    'settings.auth_bcrypt': 'Авторизация с bcrypt и HMAC-сессиями',
    'settings.csrf': 'CSRF-защита для изменяющих запросов',
    'settings.rate_limit': 'Rate limiting (5 попыток входа)',
    'settings.security_headers': 'Security headers (CSP, XSS, Clickjacking)',
    'settings.roadmap': 'Roadmap',
    'settings.language': 'Язык',
    'settings.lang_ru': 'Русский',
    'settings.lang_en': 'English',

    // Proxies
    'proxies.title': 'Прокси',
    'proxies.select': 'Выбрать',
    'proxies.latency': 'Задержка',
    'proxies.test_latency': 'Тест задержки',
    'proxies.testing': 'Тестирование...',
    'proxies.no_proxies': 'Прокси не найдены',

    // Connections
    'conn.title': 'Подключения',
    'conn.active': 'Активные подключения',
    'conn.source': 'Источник',
    'conn.destination': 'Назначение',
    'conn.rule': 'Правило',
    'conn.proxy': 'Прокси',
    'conn.traffic': 'Трафик',
    'conn.no_connections': 'Нет активных подключений',

    // Rules
    'rules.title': 'Правила',
    'rules.search': 'Поиск правил...',
    'rules.no_rules': 'Правила не найдены',

    // Traffic
    'traffic.title': 'Трафик',
    'traffic.realtime': 'Real-time трафик',
    'traffic.upload': 'Upload',
    'traffic.download': 'Download',

    // Logs
    'logs.title': 'Логи',
    'logs.unified': 'Unified логи',
    'logs.filter': 'Фильтр',
    'logs.level': 'Уровень',
    'logs.source': 'Источник',
    'logs.pause': 'Пауза',
    'logs.resume': 'Возобновить',
    'logs.clear': 'Очистить',

    // Setup script
    'setup.install': 'Установка',
    'setup.update': 'Обновление',
    'setup.uninstall': 'Удаление',
    'setup.choose': 'Выберите действие',
    'setup.install_reinstall': 'Установить / Переустановить',
    'setup.beta': 'Beta версия',
    'setup.channel': 'Канал',
    'setup.stable': 'Stable',
    'setup.current_version': 'Текущая версия',
    'setup.from': 'из',
    'setup.updated': 'обновлен',
    'setup.installed': 'установлен',
    'setup.uninstalled': 'удалён',
    'setup.start': 'Запуск',
    'setup.stop': 'Остановка',
    'setup.status': 'Статус',
    'setup.web_ui': 'Веб-интерфейс',
    'setup.config': 'Конфиг',
    'setup.back': 'Назад',
    'setup.next': 'Далее',
    'setup.finish': 'Готово',
    'setup.downloading': 'Загрузка',
    'setup.installing': 'Установка',
    'setup.success': 'Успешно',
    'setup.failed': 'Ошибка',
    'setup.not_installed': 'Не установлен',
    'setup.architecture': 'Архитектура',
    'setup.backup_created': 'Backup создан',
    'setup.remove_config': 'Удалить конфиги?',
    'setup.config_kept': 'Конфиги сохранены',
    'setup.cancelled': 'Отменено',
  },
  en: {
    // Common
    'app.name': 'XKeen Control Panel',
    'app.loading': 'Loading...',
    'app.version': 'Version',
    'app.status': 'Status',
    'app.running': 'Running',
    'app.error': 'Error',
    'app.unavailable': 'Unavailable',
    'app.save': 'Save',
    'app.cancel': 'Cancel',
    'app.create': 'Create',
    'app.delete': 'Delete',
    'app.rename': 'Rename',
    'app.refresh': 'Refresh',
    'app.start': 'Start',
    'app.stop': 'Stop',
    'app.restart': 'Restart',
    'app.search': 'Search',
    'app.close': 'Close',
    'app.apply': 'Apply',
    'app.confirm': 'Confirm',
    'app.back': 'Back',
    'app.continue': 'Continue',
    'app.yes': 'Yes',
    'app.no': 'No',

    // Auth
    'auth.login': 'Control Panel Login',
    'auth.password': 'Password',
    'auth.enter_password': 'Enter password',
    'auth.login_btn': 'Login',
    'auth.logging_in': 'Logging in...',
    'auth.login_error': 'Login error',
    'auth.setup_title': 'Initial Setup',
    'auth.setup_desc': 'Set password to access the control panel',
    'auth.password_min': 'Minimum 8 characters',
    'auth.confirm_password': 'Confirm password',
    'auth.repeat_password': 'Repeat password',
    'auth.setup_btn': 'Set Password',
    'auth.setting_up': 'Setting up...',
    'auth.setup_error': 'Password setup error',
    'auth.fill_all': 'Fill in all fields',
    'auth.password_short': 'Password must be at least 8 characters',
    'auth.password_mismatch': 'Passwords do not match',
    'auth.logout': 'Logout',
    'auth.logging_out': 'Logging out...',

    // Navigation
    'nav.dashboard': 'Dashboard',
    'nav.editor': 'Editor',
    'nav.logs': 'Logs',
    'nav.proxies': 'Proxies',
    'nav.connections': 'Connections',
    'nav.rules': 'Rules',
    'nav.traffic': 'Traffic',
    'nav.services': 'Services',
    'nav.settings': 'Settings',
    'nav.theme_light': 'Light theme',
    'nav.theme_dark': 'Dark theme',

    // Dashboard
    'dash.title': 'Dashboard',
    'dash.welcome': 'Welcome to XKeen Control Panel',
    'dash.system_info': 'System Information',
    'dash.system_stats': 'System Resources',
    'dash.releases': 'Releases',
    'dash.ram': 'RAM',
    'dash.load': 'Load Average',
    'dash.uptime': 'Uptime',
    'dash.goroutines': 'Go Goroutines',

    // Editor
    'editor.title': 'Service Management',
    'editor.subtitle': 'Start, stop and restart XKeen and Mihomo',
    'editor.configs': 'Configs',
    'editor.backups': 'Backups',
    'editor.select_file': 'Select a file to edit',
    'editor.file_saved': '✓ File saved',
    'editor.backup_restored': '✓ Backup restored (don\'t forget to save)',
    'editor.create_file': 'Create file',
    'editor.delete_file': 'Delete file',
    'editor.rename_file': 'Rename file',
    'editor.file_name': 'file_name.json',
    'editor.new_name': 'new_name.json',
    'editor.load_error': 'Error loading file list',
    'editor.file_load_error': 'Error loading file',
    'editor.save_error': 'Save error',
    'editor.create_error': 'Create error',
    'editor.delete_error': 'Delete error',
    'editor.rename_error': 'Rename error',
    'editor.restore_error': 'Restore error',

    // Services
    'svc.title': 'Service Management',
    'svc.subtitle': 'Start, stop and restart XKeen and Mihomo',
    'svc.xkeen': 'XKeen (Xray)',
    'svc.xkeen_desc': 'Main proxy service based on Xray-core',
    'svc.mihomo': 'Mihomo (Clash)',
    'svc.mihomo_desc': 'Alternative proxy based on Mihomo (Clash.Meta)',
    'svc.starting': 'Starting...',
    'svc.stopping': 'Stopping...',
    'svc.restarting': 'Restarting...',
    'svc.refresh_status': 'Refresh status',
    'svc.action_error': 'Error',

    // Settings
    'settings.title': 'Settings',
    'settings.subtitle': 'Control panel information',
    'settings.about': 'About',
    'settings.version': 'Version',
    'settings.frontend': 'Frontend',
    'settings.backend': 'Backend',
    'settings.security': 'Security',
    'settings.auth_bcrypt': 'Auth with bcrypt and HMAC sessions',
    'settings.csrf': 'CSRF protection for mutating requests',
    'settings.rate_limit': 'Rate limiting (5 login attempts)',
    'settings.security_headers': 'Security headers (CSP, XSS, Clickjacking)',
    'settings.roadmap': 'Roadmap',
    'settings.language': 'Language',
    'settings.lang_ru': 'Русский',
    'settings.lang_en': 'English',

    // Proxies
    'proxies.title': 'Proxies',
    'proxies.select': 'Select',
    'proxies.latency': 'Latency',
    'proxies.test_latency': 'Test latency',
    'proxies.testing': 'Testing...',
    'proxies.no_proxies': 'No proxies found',

    // Connections
    'conn.title': 'Connections',
    'conn.active': 'Active connections',
    'conn.source': 'Source',
    'conn.destination': 'Destination',
    'conn.rule': 'Rule',
    'conn.proxy': 'Proxy',
    'conn.traffic': 'Traffic',
    'conn.no_connections': 'No active connections',

    // Rules
    'rules.title': 'Rules',
    'rules.search': 'Search rules...',
    'rules.no_rules': 'No rules found',

    // Traffic
    'traffic.title': 'Traffic',
    'traffic.realtime': 'Real-time traffic',
    'traffic.upload': 'Upload',
    'traffic.download': 'Download',

    // Logs
    'logs.title': 'Logs',
    'logs.unified': 'Unified logs',
    'logs.filter': 'Filter',
    'logs.level': 'Level',
    'logs.source': 'Source',
    'logs.pause': 'Pause',
    'logs.resume': 'Resume',
    'logs.clear': 'Clear',

    // Setup script
    'setup.install': 'Install',
    'setup.update': 'Update',
    'setup.uninstall': 'Uninstall',
    'setup.choose': 'Choose action',
    'setup.install_reinstall': 'Install / Reinstall',
    'setup.beta': 'Beta version',
    'setup.channel': 'Channel',
    'setup.stable': 'Stable',
    'setup.current_version': 'Current version',
    'setup.from': 'from',
    'setup.updated': 'updated',
    'setup.installed': 'installed',
    'setup.uninstalled': 'uninstalled',
    'setup.start': 'Start',
    'setup.stop': 'Stop',
    'setup.status': 'Status',
    'setup.web_ui': 'Web UI',
    'setup.config': 'Config',
    'setup.back': 'Back',
    'setup.next': 'Next',
    'setup.finish': 'Finish',
    'setup.downloading': 'Downloading',
    'setup.installing': 'Installing',
    'setup.success': 'Success',
    'setup.failed': 'Failed',
    'setup.not_installed': 'Not installed',
    'setup.architecture': 'Architecture',
    'setup.backup_created': 'Backup created',
    'setup.remove_config': 'Remove config?',
    'setup.config_kept': 'Config kept',
    'setup.cancelled': 'Cancelled',
  }
}

// Detect browser language
function detectLanguage(): Lang {
  const saved = localStorage.getItem('lang') as Lang
  if (saved && translations[saved]) return saved
  
  const browserLang = navigator.language.split('-')[0]
  if (browserLang === 'ru') return 'ru'
  return 'en'
}

// Create stores
export const currentLang = writable<Lang>(detectLanguage())

// Derived store for translations
export const t = derived(currentLang, $lang => {
  return (key: string, params?: Record<string, string | number>): string => {
    const dict = translations[$lang] || translations.en
    let text = dict[key] || key
    
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        text = text.replace(new RegExp(`{${k}}`, 'g'), String(v))
      })
    }
    
    return text
  }
})

// Switch language
export function setLang(lang: Lang) {
  currentLang.set(lang)
  localStorage.setItem('lang', lang)
}

// Get available languages
export function getAvailableLangs(): { code: Lang; name: string }[] {
  return [
    { code: 'ru', name: 'Русский' },
    { code: 'en', name: 'English' }
  ]
}
