// @ts-check
import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['**/node_modules/**', '**/dist/**', '**/generated/**', '**/.next/**', '**/next-env.d.ts', 'apps/web/public/**', '**/test-results/**'] },
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    rules: {
      // Ported C code uses bit-twiddling, labelled loops and constant conditions (while (1)).
      'no-constant-condition': ['error', { checkLoops: false }],
      'no-labels': 'off',
      'prefer-const': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      '@typescript-eslint/no-non-null-assertion': 'off',
      'no-control-regex': 'off',
    },
  },
);
