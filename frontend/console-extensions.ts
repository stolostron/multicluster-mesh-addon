import { EncodedExtension } from '@openshift-console/dynamic-plugin-sdk-webpack'

const fleetServiceMeshPerspective: EncodedExtension = {
  type: 'console.perspective',
  properties: {
    id: 'fleet-service-mesh',
    name: 'Fleet Service Mesh',
    icon: { $codeRef: 'perspectiveIcon' },
    landingPageURL: { $codeRef: 'perspective.landingPageURL' },
    importRedirectURL: { $codeRef: 'perspective.importRedirectURL' },
    defaultPins: [
      { group: 'maistra.io', version: 'v2', kind: 'ServiceMeshControlPlane' },
    ],
  },
}

const fleetMeshNavSection: EncodedExtension = {
  type: 'console.navigation/section',
  properties: {
    perspective: 'fleet-service-mesh',
    id: 'fleet-service-mesh-main',
    name: 'Service Mesh',
  },
}

const fleetMeshesNavItem: EncodedExtension = {
  type: 'console.navigation/href',
  properties: {
    perspective: 'fleet-service-mesh',
    section: 'fleet-service-mesh-main',
    id: 'fleet-meshes',
    name: 'Meshes',
    href: '/service-mesh',
  },
}

const fleetMeshOverviewRoute: EncodedExtension = {
  type: 'console.page/route',
  properties: {
    perspective: 'fleet-service-mesh',
    path: '/service-mesh',
    component: { $codeRef: 'serviceMeshPage.default' },
  },
}

export const extensions: EncodedExtension[] = [
  fleetServiceMeshPerspective,
  fleetMeshNavSection,
  fleetMeshesNavItem,
  fleetMeshOverviewRoute,
]
