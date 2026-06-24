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

const overviewNavItem: EncodedExtension = {
  type: 'console.navigation/href',
  properties: {
    perspective: 'fleet-service-mesh',
    id: 'fleet-mesh-overview',
    name: consoleName('Overview'),
    href: '/fleet-mesh-overview',
  },
}

const fleetMeshesNavItem: EncodedExtension = {
  type: 'console.navigation/href',
  properties: {
    perspective: 'fleet-service-mesh',
    id: 'fleet-meshes',
    name: consoleName('Fleet Meshes'),
    href: '/service-mesh',
  },
}

const controlPlanesNavItem: EncodedExtension = {
  type: 'console.navigation/href',
  properties: {
    perspective: 'fleet-service-mesh',
    id: 'fleet-control-planes',
    name: consoleName('Control Planes'),
    href: '/mesh-control-planes',
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

const controlPlaneDetailRoute: EncodedExtension = {
  type: 'console.page/route',
  properties: {
    perspective: 'fleet-service-mesh',
    path: '/mesh-control-planes/:cluster/:name',
    component: { $codeRef: 'controlPlaneDetailPage.default' },
  },
}

const controlPlanesRoute: EncodedExtension = {
  type: 'console.page/route',
  properties: {
    perspective: 'fleet-service-mesh',
    path: '/mesh-control-planes',
    component: { $codeRef: 'controlPlanesPage.default' },
  },
}

const overviewRoute: EncodedExtension = {
  type: 'console.page/route',
  properties: {
    perspective: 'fleet-service-mesh',
    path: '/fleet-mesh-overview',
    component: { $codeRef: 'overviewPage.default' },
  },
}

// Detail routes must be registered before their list routes because React Router v5
// matches the first route whose path prefix matches the URL.
export const extensions: EncodedExtension[] = [
  fleetServiceMeshPerspective,
  overviewNavItem,
  fleetMeshesNavItem,
  controlPlanesNavItem,
  overviewRoute,
  fleetMeshDetailRoute,
  fleetMeshOverviewRoute,
  controlPlaneDetailRoute,
  controlPlanesRoute,
]
