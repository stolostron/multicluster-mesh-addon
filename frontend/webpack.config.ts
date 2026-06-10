import { ConsoleRemotePlugin } from '@openshift-console/dynamic-plugin-sdk-webpack'
import { extensions } from './console-extensions'
import { pluginMetadata } from './console-plugin-metadata'

export default function (_env: unknown, _argv: unknown) {
  return {
    entry: {},
    resolve: {
      extensions: ['.ts', '.tsx', '.js', '.jsx'],
    },
    module: {
      rules: [
        {
          test: /\.(ts|tsx)$/,
          exclude: /node_modules/,
          loader: 'ts-loader',
          options: { transpileOnly: true },
        },
      ],
    },
    plugins: [
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
    },
  }
}
