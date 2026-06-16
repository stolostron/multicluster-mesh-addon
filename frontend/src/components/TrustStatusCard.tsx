import * as React from 'react'
import {
  useK8sWatchResource,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Card,
  CardBody,
  CardTitle,
  EmptyState,
  EmptyStateBody,
  Label,
  Spinner,
  Title,
} from '@patternfly/react-core'
import type { ClusterMeshStatus } from '../types/multiClusterMesh'
import type { Certificate } from '../types/certManager'
import { certificateGroupVersionKind } from '../types/certManager'
import type { ManifestWork } from '../types/manifestWork'
import { manifestWorkGroupVersionKind } from '../types/manifestWork'
import type { K8sCondition } from '../types/common'

const CLUSTER_NAME_LABEL = 'mesh.open-cluster-management.io/cluster-name'
const MESH_NAME_LABEL = 'mesh.open-cluster-management.io/mesh-name'
const MESH_NAMESPACE_LABEL = 'mesh.open-cluster-management.io/mesh-namespace'

function findCondition(conditions: K8sCondition[] | undefined, type: string): K8sCondition | undefined {
  return conditions?.find((c) => c.type === type)
}

function certStatusLabel(cert: Certificate | undefined): React.ReactNode {
  if (!cert) return <Label color="grey" isCompact>Pending</Label>
  const ready = findCondition(cert.status?.conditions, 'Ready')
  if (!ready) return <Label color="grey" isCompact>Unknown</Label>
  if (ready.status === 'True') return <Label color="green" isCompact>Ready</Label>
  return <Label color="red" isCompact>{ready.reason ?? 'Not Ready'}</Label>
}

function distributionStatusLabel(mw: ManifestWork | undefined, mwError: unknown): React.ReactNode {
  if (mwError) return <Label color="grey" isCompact>Unavailable</Label>
  if (!mw) return <Label color="grey" isCompact>Pending</Label>
  const applied = findCondition(mw.status?.conditions, 'Applied')
  const available = findCondition(mw.status?.conditions, 'Available')
  if (applied?.status === 'True' && available?.status === 'True') {
    return <Label color="green" isCompact>Distributed</Label>
  }
  if (applied?.status === 'True') {
    return <Label color="orange" isCompact>Applied</Label>
  }
  const failed = mw.status?.conditions?.find((c) => c.status !== 'True')
  return <Label color="red" isCompact>{failed?.reason ?? 'Pending'}</Label>
}

interface TrustStatusCardProps {
  meshName: string
  meshNamespace: string
  issuerName: string
  clusterStatuses: ClusterMeshStatus[]
}

export const TrustStatusCard: React.FC<TrustStatusCardProps> = ({
  meshName,
  meshNamespace,
  issuerName,
  clusterStatuses,
}) => {
  const hasIssuer = !!issuerName

  const [certs, certsLoaded, certsError] = useK8sWatchResource<Certificate[]>(
    hasIssuer
      ? {
          groupVersionKind: certificateGroupVersionKind,
          isList: true,
          namespace: meshNamespace,
          selector: {
            matchLabels: { [MESH_NAME_LABEL]: meshName },
          },
        }
      : null,
  )

  const [manifestWorks, mwLoaded, mwError] = useK8sWatchResource<ManifestWork[]>(
    hasIssuer
      ? {
          groupVersionKind: manifestWorkGroupVersionKind,
          isList: true,
          selector: {
            matchLabels: {
              [MESH_NAME_LABEL]: meshName,
              [MESH_NAMESPACE_LABEL]: meshNamespace,
            },
          },
        }
      : null,
  )

  if (!hasIssuer) {
    return (
      <Card isCompact>
        <CardTitle>Trust Status</CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>
              Trust distribution is not configured. Set <code>spec.security.trust.certManager.issuerRef.name</code> on the mesh to enable it.
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  if (certsError) {
    return (
      <Card isCompact>
        <CardTitle>Trust Status</CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <Title headingLevel="h4" size="md">Unable to load certificate data</Title>
            <EmptyStateBody>
              {certsError instanceof Error ? certsError.message : String(certsError)}
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  if (!certsLoaded || (!mwLoaded && !mwError)) {
    return (
      <Card isCompact>
        <CardTitle>Trust Status</CardTitle>
        <CardBody>
          <Spinner aria-label="Loading trust status" size="lg" />
        </CardBody>
      </Card>
    )
  }

  if (clusterStatuses.length === 0) {
    return (
      <Card isCompact>
        <CardTitle>Trust Status</CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>No clusters are part of this mesh yet.</EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  const certsByCluster = new Map<string, Certificate>()
  for (const cert of certs ?? []) {
    const clusterName = cert.metadata?.labels?.[CLUSTER_NAME_LABEL]
    if (clusterName) certsByCluster.set(clusterName, cert)
  }

  const mwByCluster = new Map<string, ManifestWork>()
  for (const mw of manifestWorks ?? []) {
    const clusterName = mw.metadata?.labels?.[CLUSTER_NAME_LABEL]
    if (clusterName) mwByCluster.set(clusterName, mw)
  }

  const noCertsAtAll = certsByCluster.size === 0 && mwByCluster.size === 0
  if (noCertsAtAll) {
    return (
      <Card isCompact>
        <CardTitle>Trust Status</CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>No certificates have been created yet — the controller may still be reconciling.</EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  return (
    <Card isCompact>
      <CardTitle>Trust Status</CardTitle>
      <CardBody>
        <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
          <thead className="pf-v6-c-table__thead">
            <tr className="pf-v6-c-table__tr">
              <th className="pf-v6-c-table__th">Cluster</th>
              <th className="pf-v6-c-table__th">Certificate</th>
              <th className="pf-v6-c-table__th">Expires</th>
              <th className="pf-v6-c-table__th">Renews</th>
              <th className="pf-v6-c-table__th">Distribution</th>
            </tr>
          </thead>
          <tbody className="pf-v6-c-table__tbody">
            {clusterStatuses.map((cs) => {
              const cert = certsByCluster.get(cs.clusterName)
              const mw = mwByCluster.get(cs.clusterName)
              return (
                <tr className="pf-v6-c-table__tr" key={cs.clusterName}>
                  <td className="pf-v6-c-table__td">{cs.clusterName}</td>
                  <td className="pf-v6-c-table__td">{certStatusLabel(cert)}</td>
                  <td className="pf-v6-c-table__td">
                    {cert?.status?.notAfter ? <Timestamp timestamp={cert.status.notAfter} /> : '-'}
                  </td>
                  <td className="pf-v6-c-table__td">
                    {cert?.status?.renewalTime ? <Timestamp timestamp={cert.status.renewalTime} /> : '-'}
                  </td>
                  <td className="pf-v6-c-table__td">{distributionStatusLabel(mw, mwError)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </CardBody>
    </Card>
  )
}
