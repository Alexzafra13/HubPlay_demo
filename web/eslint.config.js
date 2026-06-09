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
    // MediaGrid usa @tanstack/react-virtual, un store externo mutable que
    // fuerza re-renders por su cuenta. El `babel-plugin-react-compiler@1.0`
    // del build sobre-memoiza la lectura de getVirtualItems() y el grid deja
    // de reciclar al scrollear, así que el componente lleva el directivo
    // oficial `"use no memo"`. Pero el `eslint-plugin-react-compiler` está en
    // rc2 (no hay release 1.x) y marca ese directivo como "unused" por
    // desfase de versiones. Apagamos la regla SOLO en este archivo hasta que
    // el plugin de lint alcance al compiler. Verificado en navegador real
    // que el reciclado funciona (ver web/verify/).
    files: ['src/components/media/MediaGrid.tsx'],
    rules: {
      'react-compiler/react-compiler': 'off',
    },
  },
])
