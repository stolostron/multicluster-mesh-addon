import { ConsolePluginBuildMetadata } from '@openshift-console/dynamic-plugin-sdk-webpack'

export const pluginMetadata: ConsolePluginBuildMetadata = {
  name: 'ossm-acm',
  version: '0.1.0',
  displayName: 'Fleet Service Mesh',
  description: 'Adds a Fleet Service Mesh perspective for fleet-wide service mesh visibility across managed clusters',
  exposedModules: {
    controlPlaneDetailPage: './src/components/ControlPlaneDetailPage',
    controlPlanesPage: './src/components/ControlPlanesPage',
    discoveredMeshDetailPage: './src/components/DiscoveredMeshDetailPage',
    meshDetailPage: './src/components/MeshDetailPage',
    overviewPage: './src/components/OverviewPage',
    serviceMeshPage: './src/components/ServiceMeshPage',
    perspective: './src/perspective',
    perspectiveIcon: './src/perspectiveIcon',
  },
  dependencies: {
    '@console/pluginAPI': '>=4.19.0',
  },
}
