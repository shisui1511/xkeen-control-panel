<script lang="ts">
  import { t } from '../i18n';
  import {
    scorePassword,
    strengthPresentation,
    type PasswordStrengthScore,
    type PasswordStrengthTone
  } from '../lib/passwordStrength';

  // Индикатор надёжности пароля (D-18): чисто информационный, никогда не
  // блокирует отправку формы. zxcvbn грузится лениво внутри passwordStrength.ts.
  interface Props {
    password: string;
    userInputs?: (string | number)[];
  }

  let { password, userInputs = [] }: Props = $props();

  let score = $state<PasswordStrengthScore | null>(null);
  let requestId = 0;

  $effect(() => {
    const currentPassword = password;
    const currentInputs = userInputs;
    const myRequestId = ++requestId;

    if (!currentPassword) {
      score = null;
      return;
    }

    scorePassword(currentPassword, currentInputs).then((result) => {
      // Защита от устаревшего ответа: пароль мог уже измениться, пока
      // асинхронная оценка (с ленивой загрузкой библиотеки) была в полёте.
      if (myRequestId === requestId) {
        score = result;
      }
    });
  });

  let presentation = $derived(score === null ? null : strengthPresentation(score));

  const toneVar: Record<PasswordStrengthTone, string> = {
    danger: 'var(--danger)',
    warning: 'var(--warning)',
    accent: 'var(--accent)',
    success: 'var(--success)'
  };
</script>

{#if presentation}
  <div class="strength-meter" aria-label={$t('auth.strength_label')}>
    <div class="strength-segments">
      {#each { length: 4 } as _, i (i)}
        <span
          class="strength-segment"
          style:background={i < presentation.segments
            ? toneVar[presentation.tone]
            : 'var(--border)'}
        ></span>
      {/each}
    </div>
    <span class="strength-label" aria-live="polite">{$t(presentation.labelKey)}</span>
  </div>
{/if}

<style>
  .strength-meter {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin: 6px 0 0;
  }

  .strength-segments {
    display: flex;
    gap: 4px;
    flex: 1;
  }

  .strength-segment {
    height: 4px;
    flex: 1;
    border-radius: 2px;
    background: var(--border);
  }

  .strength-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-dim);
    white-space: nowrap;
  }
</style>
