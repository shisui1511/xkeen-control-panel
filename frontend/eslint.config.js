import js from '@eslint/js';
import ts from 'typescript-eslint';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';

export default ts.config(
  js.configs.recommended,
  ...ts.configs.recommended,
  ...svelte.configs['flat/recommended'],
  {
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node
      }
    }
  },
  {
    files: ['**/*.svelte'],
    languageOptions: {
      parserOptions: {
        parser: ts.parser
      }
    }
  },
  {
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-empty-object-type': 'off',
      'no-empty': 'off',
      'svelte/no-unused-svelte-ignore': 'off',
      'svelte/valid-compile': 'warn',
      'svelte/require-each-key': 'off',
      'svelte/prefer-svelte-reactivity': 'off',
      'svelte/no-immutable-reactive-statements': 'off',
      'no-useless-assignment': 'off'
    }
  },
  {
    files: ['src/**/*.{svelte,ts}'],
    ignores: ['src/lib/api.ts'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: "CallExpression[callee.name='fetch']",
          message: 'Use apiFetch/apiFetchJSON from lib/api.ts instead of bare fetch().'
        }
      ]
    }
  },
  {
    files: ['src/**/*.svelte'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: 'Literal[value=/[\\u0400-\\u04FF]/]',
          message:
            'Hardcoded Cyrillic strings are forbidden in Svelte components. Extract to i18n locales.'
        },
        {
          selector: 'SvelteText[value=/[\\u0400-\\u04FF]/]',
          message: 'Hardcoded Cyrillic text is forbidden in Svelte components. Use $t(...) instead.'
        }
      ]
    }
  },
  {
    ignores: ['build/', 'dist/', '.svelte-kit/', 'node_modules/']
  }
);
