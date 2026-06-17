import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      globals: globals.browser,
    },
  },
  // Enforce that MUI is only imported inside src/components.
  // All other code must consume the design-system wrappers from there.
  {
    files: ['**/*.{ts,tsx}'],
    ignores: ['src/components/**'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['@mui/*', '@mui/material', '@mui/x-data-grid', '@mui/icons-material'],
              message:
                "Do not import MUI directly. Use the design-system wrappers in 'src/components' instead.",
            },
          ],
        },
      ],
    },
  },
])
