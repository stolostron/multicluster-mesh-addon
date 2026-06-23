import { ConsolePluginBuildMetadata } from '@openshift-console/dynamic-plugin-sdk-webpack'

export const pluginMetadata: ConsolePluginBuildMetadata = {
  name: 'ossm-acm',
  version: '0.1.0',
  displayName: 'OpenShift Service Mesh — ACM Integration',
  description: 'Adds Service Mesh visibility to the ACM Fleet Management console',
  exposedModules: {
    meshDetailPage: './src/components/MeshDetailPage',
    serviceMeshPage: './src/components/ServiceMeshPage',
    perspective: './src/perspective',
    perspectiveIcon: './src/perspectiveIcon',
  },
  dependencies: {
    '@console/pluginAPI': '>=4.19.0',
  },
}
