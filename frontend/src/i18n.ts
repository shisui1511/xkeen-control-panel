import { writable, derived } from 'svelte/store';

// Available languages
export type Lang = 'ru' | 'en';

// Minimal built-in dictionary for load and auth screens
const baseTranslations: Record<Lang, Record<string, string>> = {
  ru: {
    'app.name': 'XKeen Control Panel',
    'app.loading': 'Загрузка...',
    'app.conn_error': 'Ошибка подключения',
    'app.conn_error_desc': 'Проверьте подключение к сети или обновите страницу.',
    'app.reload': 'Обновить',
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
    'auth.logging_out': 'Выход...'
  },
  en: {
    'app.name': 'XKeen Control Panel',
    'app.loading': 'Loading...',
    'app.conn_error': 'Connection error',
    'app.conn_error_desc': 'Check your network connection or refresh the page.',
    'app.reload': 'Reload',
    'auth.login': 'Control Panel Login',
    'auth.password': 'Password',
    'auth.enter_password': 'Enter password',
    'auth.login_btn': 'Login',
    'auth.logging_in': 'Logging in...',
    'auth.login_error': 'Login error',
    'auth.setup_title': 'Initial Setup',
    'auth.setup_desc': 'Set a password to access the control panel',
    'auth.password_min': 'Minimum 8 characters',
    'auth.confirm_password': 'Confirm password',
    'auth.repeat_password': 'Repeat password',
    'auth.setup_btn': 'Set Password',
    'auth.setting_up': 'Setting up...',
    'auth.setup_error': 'Setup error',
    'auth.fill_all': 'Fill all fields',
    'auth.password_short': 'Password must be at least 8 characters',
    'auth.password_mismatch': 'Passwords do not match',
    'auth.logout': 'Logout',
    'auth.logging_out': 'Logging out...'
  }
};

// Store containing all loaded/merged translations
export const translationsStore = writable<Record<Lang, Record<string, string>>>({
  ru: { ...baseTranslations.ru },
  en: { ...baseTranslations.en }
});

// Detect browser language
function detectLanguage(): Lang {
  let saved: Lang | null = null;
  try {
    saved = localStorage.getItem('lang') as Lang;
  } catch (e) {
    // localStorage may be unavailable
  }
  if (saved && (saved === 'ru' || saved === 'en')) return saved;

  const browserLang = navigator.language.split('-')[0];
  if (browserLang === 'ru') return 'ru';
  return 'en';
}

const initialLang = detectLanguage();
export const currentLang = writable<Lang>(initialLang);

// Async function to load locale dictionary and merge it with built-in base keys
export async function loadLanguage(lang: Lang): Promise<void> {
  try {
    let dict;
    if (lang === 'ru') {
      dict = await import('./locales/ru.json');
    } else {
      dict = await import('./locales/en.json');
    }

    const moduleData = dict.default || dict;

    translationsStore.update((current) => {
      current[lang] = {
        ...baseTranslations[lang],
        ...moduleData
      };
      return current;
    });
  } catch (err) {
    console.error(`Failed to load translation for ${lang}:`, err);
  }
}

// Kick off loading for initial language immediately
export const i18nReady = loadLanguage(initialLang);

// Derived store for translations
export const t = derived([currentLang, translationsStore], ([$lang, $translations]) => {
  return (key: string, params?: Record<string, string | number>): string => {
    const dict = $translations[$lang] || $translations.en || {};
    let text = dict[key];
    if (text === undefined) {
      // Fallback to base translation or key
      const baseDict = baseTranslations[$lang] || baseTranslations.en;
      text = baseDict[key] || key;
    }

    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        text = text.replace(new RegExp(`{${k}}`, 'g'), String(v));
      });
    }

    return text;
  };
});

/**
 * Хелпер для выбора правильной формы числительного.
 * По умолчанию применяет правила русского языка (CLDR Russian rules).
 * Для lang === 'en' применяет двухформенное правило английского (singular / plural).
 *
 * @param n - число для проверки
 * @param one - форма для единственного числа (рус: 1, 21, 101…; англ: 1)
 * @param few - форма для малого числа (рус: 2–4, 22–24…; для англ: не используется)
 * @param many - форма для множественного числа (рус: 0, 5–20…; англ: 0, 2, 3…)
 * @param lang - язык для применения правил ('ru' по умолчанию, поддерживает 'en')
 */
export function pluralize(
  n: number,
  one: string,
  few: string,
  many: string,
  lang: Lang = 'ru'
): string {
  if (lang === 'en') {
    return n === 1 ? one : many;
  }
  // Русские правила CLDR
  const abs = Math.abs(Math.floor(n));
  if (abs % 10 === 1 && abs % 100 !== 11) return one;
  if (abs % 10 >= 2 && abs % 10 <= 4 && (abs % 100 < 10 || abs % 100 >= 20)) return few;
  return many;
}

// Switch language
export async function setLang(lang: Lang) {
  await loadLanguage(lang);
  currentLang.set(lang);
  try {
    localStorage.setItem('lang', lang);
  } catch (e) {
    // localStorage may be unavailable
  }
}

// Get available languages
export function getAvailableLangs(): { code: Lang; name: string }[] {
  return [
    { code: 'ru', name: 'Русский' },
    { code: 'en', name: 'English' }
  ];
}
