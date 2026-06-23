/** @type {import('jest').Config} */
const config = {
  testEnvironment: 'jsdom',
  transform: {
    '^.+\\.tsx?$': ['ts-jest', { tsconfig: 'tsconfig.jest.json' }],
  },
  roots: ['<rootDir>/src'],
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx', 'json'],
  moduleNameMapper: {
    '^@openshift-console/dynamic-plugin-sdk$': '<rootDir>/src/__mocks__/consoleSdkMock.tsx',
    '^@stolostron/multicluster-sdk$': '<rootDir>/src/__mocks__/multiclusterSdkMock.tsx',
    '^react-router-dom-v5-compat$': '<rootDir>/src/__mocks__/routerMock.tsx',
    '\\.(svg|png|jpg|gif)$': '<rootDir>/src/__mocks__/fileMock.ts',
    '\\.css$': '<rootDir>/src/__mocks__/styleMock.ts',
  },
  setupFilesAfterEnv: ['<rootDir>/src/setupTests.tsx'],
}

module.exports = config
