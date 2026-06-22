import { EncodedExtension } from '@openshift-console/dynamic-plugin-sdk-webpack'

// The Console resolves %plugin__ossm-acm~Title% markers using the plugin's locale bundle
// at dist/locales/{lang}/plugin__ossm-acm.json. This is the Console's own i18n system,
// separate from the react-i18next instance used inside plugin components.
const consoleName = (name: string) => `%plugin__ossm-acm~${name}%`

const fleetServiceMeshPerspective: EncodedExtension = {
  type: 'console.perspective',
  properties: {
    id: 'fleet-service-mesh',
    name: consoleName('Fleet Service Mesh'),
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
    name: consoleName('Service Mesh'),
  },
}

const fleetMeshesNavItem: EncodedExtension = {
  type: 'console.navigation/href',
  properties: {
    perspective: 'fleet-service-mesh',
    section: 'fleet-service-mesh-main',
    id: 'fleet-meshes',
    name: consoleName('Meshes'),
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
