import { ConsoleRemotePlugin } from '@openshift-console/dynamic-plugin-sdk-webpack'
import CopyPlugin from 'copy-webpack-plugin'
import path from 'path'
import { extensions } from './console-extensions'
import { pluginMetadata } from './console-plugin-metadata'

export default function (_env: unknown, argv: { mode?: string }) {
  const isProduction = argv.mode === 'production'

  return {
    entry: {},
    output: {
      path: path.resolve(__dirname, 'dist'),
    },
    resolve: {
      extensions: ['.ts', '.tsx', '.js', '.jsx'],
    },
    module: {
      rules: [
        {
          test: /\.(ts|tsx)$/,
          exclude: /node_modules/,
          loader: 'swc-loader',
          options: {
            jsc: {
              parser: { syntax: 'typescript', tsx: true },
              transform: { react: { runtime: 'automatic' } },
              target: 'es2022',
            },
          },
        },
      ],
    },
    plugins: [
      // Copy locale files to dist/ so the Console can load translations at runtime.
      // The Console loads: {pluginBaseURL}/locales/{lang}/{namespace}.json
      new CopyPlugin({
        patterns: [{ from: 'src/locales', to: 'locales' }],
      }),
      new ConsoleRemotePlugin({
        pluginMetadata,
        extensions,
        validateSharedModules: false,
        validateExtensionIntegrity: false,
      }),
    ],
    devServer: {
      port: 9001,
      static: './dist',
      allowedHosts: 'all' as const,
      headers: {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, PATCH, OPTIONS',
        'Access-Control-Allow-Headers': 'X-Requested-With, Content-Type, Authorization',
      },
      devMiddleware: {
        writeToDisk: true,
      },
    },
    ...(isProduction
      ? {}
      : {
          devtool: 'source-map',
          optimization: { minimize: false },
        }),
  }
}
