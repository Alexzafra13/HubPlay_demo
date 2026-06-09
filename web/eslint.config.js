import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import reactCompiler from 'eslint-plugin-react-compiler'
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
    plugins: {
      'react-compiler': reactCompiler,
    },
    rules: {
      // Verifica que cada componente sea compatible con el React
      // Compiler (la optimización automática de React 19). Si el
      // healthcheck reporta 533/533 compatibles, este plugin lo
      // garantiza a nivel de archivo en cada PR.
      'react-compiler/react-compiler': 'error',
    },
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
  },
  {
    // El subcomponente VirtualizedMediaGrid usa `useWindowVirtualizer`
    // (scroll de página) y necesita el directivo `"use no memo"`: el
    // babel-plugin-react-compiler@1.0 del build, si no, cachea
    // getVirtualItems() y el grid deja de reciclar al scrollear (verificado
    // en navegador, web/verify/). El eslint-plugin-react-compiler está en
    // rc2 (no hay 1.x) y NO reconoce que `useWindowVirtualizer` requiere
    // bailout — solo `useVirtualizer` dispara `incompatible-library` — así
    // que marca el directivo como "unused". Apagamos la regla SOLO en este
    // archivo hasta que el plugin de lint alcance al compiler.
    files: ['src/components/media/MediaGrid.tsx'],
    rules: {
      'react-compiler/react-compiler': 'off',
    },
  },
])
