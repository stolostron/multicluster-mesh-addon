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

const fleetMeshDetailRoute: EncodedExtension = {
  type: 'console.page/route',
  properties: {
    perspective: 'fleet-service-mesh',
    path: '/service-mesh/:ns/:name',
    component: { $codeRef: 'meshDetailPage.default' },
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

// Detail route must be registered before the list route because React Router v5
// matches the first route whose path prefix matches the URL.
export const extensions: EncodedExtension[] = [
  fleetServiceMeshPerspective,
  fleetMeshNavSection,
  fleetMeshesNavItem,
  fleetMeshDetailRoute,
  fleetMeshOverviewRoute,
]
