import { defineConfig } from '@rstest/core'

export default defineConfig({
  testEnvironment: 'jsdom',
  globals: true,
  include: ['src/**/*.test.{ts,tsx}'],
  setupFiles: ['./src/setupTests.tsx'],
  tools: {
    swc: {
      jsc: {
        transform: {
          react: {
            runtime: 'automatic',
          },
        },
      },
    },
  },
  source: {
    tsconfigPath: './tsconfig.json',
  },
  resolve: {
    alias: {
      '@openshift-console/dynamic-plugin-sdk': './src/__mocks__/consoleSdkMock.tsx',
      '@stolostron/multicluster-sdk': './src/__mocks__/multiclusterSdkMock.tsx',
      'react-router-dom-v5-compat': './src/__mocks__/routerMock.tsx',
    },
  },
})
