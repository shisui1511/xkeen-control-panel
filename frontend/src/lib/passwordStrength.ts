// D-18: индикатор надёжности пароля на zxcvbn (@zxcvbn-ts). Библиотека и все
// её словари грузятся только внутри loadScorer() — никогда на верхнем уровне
// модуля, чтобы не попасть в бюджет первого экрана (frontend/scripts/check-
// bundle-size.cjs учитывает только статические импорты entry-чанка).

export type PasswordStrengthScore = 0 | 1 | 2 | 3 | 4;

export type PasswordStrengthTone = 'danger' | 'warning' | 'accent' | 'success';

export interface StrengthPresentation {
  segments: number;
  tone: PasswordStrengthTone;
  labelKey: string;
}

// typeof import(...) — чисто типовая конструкция, стирается при компиляции и
// не создаёт статического импорта модуля (в отличие от `import ... from`).
type ZxcvbnCoreModule = typeof import('@zxcvbn-ts/core');
type ZxcvbnScorer = InstanceType<ZxcvbnCoreModule['ZxcvbnFactory']>;

let scorerPromise: Promise<ZxcvbnScorer> | null = null;

function loadScorer(): Promise<ZxcvbnScorer> {
  if (!scorerPromise) {
    scorerPromise = (async () => {
      const [core, common, en, ru] = await Promise.all([
        import('@zxcvbn-ts/core'),
        import('@zxcvbn-ts/language-common'),
        import('@zxcvbn-ts/language-en'),
        import('@zxcvbn-ts/language-ru')
      ]);

      return new core.ZxcvbnFactory({
        dictionary: {
          ...common.dictionary,
          ...en.dictionary,
          ...ru.dictionary
        },
        graphs: common.adjacencyGraphs,
        translations: en.translations
      });
    })();
  }
  return scorerPromise;
}

/**
 * Оценивает надёжность пароля (0-4) через zxcvbn. Пустой пароль возвращает 0
 * немедленно, без загрузки библиотеки. userInputs (например, код настройки)
 * учитываются как контекстные слова, снижающие оценку при совпадении.
 */
export async function scorePassword(
  password: string,
  userInputs: (string | number)[] = []
): Promise<PasswordStrengthScore> {
  if (!password) {
    return 0;
  }
  const scorer = await loadScorer();
  const result = scorer.check(password, userInputs);
  return result.score as PasswordStrengthScore;
}

/** Отображение score в число заполненных сегментов, цветовой тон и ключ i18n (UI-SPEC §2). */
export function strengthPresentation(score: PasswordStrengthScore): StrengthPresentation {
  switch (score) {
    case 0:
      return { segments: 1, tone: 'danger', labelKey: 'auth.strength_very_weak' };
    case 1:
      return { segments: 2, tone: 'danger', labelKey: 'auth.strength_weak' };
    case 2:
      return { segments: 3, tone: 'warning', labelKey: 'auth.strength_fair' };
    case 3:
      return { segments: 4, tone: 'accent', labelKey: 'auth.strength_good' };
    default:
      return { segments: 4, tone: 'success', labelKey: 'auth.strength_strong' };
  }
}
