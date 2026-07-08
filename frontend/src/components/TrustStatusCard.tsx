import { useCallback, useEffect, useMemo } from 'react'
import type { FC, ReactNode } from 'react'
import { Link } from 'react-router-dom-v5-compat'
import { Trans } from 'react-i18next'
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
import { clusterDetailLink } from '../utils/linkUtils'
import { VirtualFilterTable } from './VirtualFilterTable'
import type { CategoryLabel, VirtualFilterColumn } from './VirtualFilterTable'
import { useMeshTranslation } from '../utils/i18nUtils'
import { clusterMeshStatusRowKey, clusterMeshStatusSearchMatch } from '../utils/tableCallbacks'

const TRUST_CATEGORY_LABELS: CategoryLabel[] = [
  { key: 'all', label: 'All ({{count}})' },
  { key: 'ready', label: 'Distributed ({{count}})' },
  { key: 'pending', label: 'Pending ({{count}})' },
  { key: 'failed', label: 'Failed ({{count}})' },
]

const CLUSTER_NAME_LABEL = 'mesh.open-cluster-management.io/cluster-name'
const MESH_NAME_LABEL = 'mesh.open-cluster-management.io/mesh-name'
const MESH_NAMESPACE_LABEL = 'mesh.open-cluster-management.io/mesh-namespace'

function findCondition(conditions: K8sCondition[] | undefined, type: string): K8sCondition | undefined {
  return conditions?.find((c) => c.type === type)
}

type TrustCategory = 'ready' | 'pending' | 'failed'

function categorizeTrust(cert: Certificate | undefined, mw: ManifestWork | undefined): TrustCategory {
  if (!cert) return 'pending'
  const ready = findCondition(cert.status?.conditions, 'Ready')
  if (!ready || ready.status !== 'True') return 'failed'
  if (!mw) return 'pending'
  const applied = findCondition(mw.status?.conditions, 'Applied')
  const available = findCondition(mw.status?.conditions, 'Available')
  if (applied?.status === 'True' && available?.status === 'True') return 'ready'
  if (applied?.status === 'True') return 'pending'
  return 'failed'
}

function certStatusLabel(cert: Certificate | undefined, t: (key: string) => string): ReactNode {
  if (!cert) return <Label color="grey" isCompact>{t('Pending')}</Label>
  const ready = findCondition(cert.status?.conditions, 'Ready')
  if (!ready) return <Label color="grey" isCompact>{t('Unknown')}</Label>
  if (ready.status === 'True') return <Label color="green" isCompact>{t('Ready')}</Label>
  return <Label color="red" isCompact>{ready.reason ?? t('Not Ready')}</Label>
}

function distributionStatusLabel(
  mw: ManifestWork | undefined,
  mwError: unknown,
  t: (key: string) => string,
): ReactNode {
  if (mwError) return <Label color="grey" isCompact>{t('Unavailable')}</Label>
  if (!mw) return <Label color="grey" isCompact>{t('Pending')}</Label>
  const applied = findCondition(mw.status?.conditions, 'Applied')
  const available = findCondition(mw.status?.conditions, 'Available')
  if (applied?.status === 'True' && available?.status === 'True') {
    return <Label color="green" isCompact>{t('Distributed')}</Label>
  }
  if (applied?.status === 'True') {
    return <Label color="orange" isCompact>{t('Applied')}</Label>
  }
  const failed = mw.status?.conditions?.find((c) => c.status !== 'True')
  return <Label color="red" isCompact>{failed?.reason ?? t('Pending')}</Label>
}

interface TrustStatusCardProps {
  clusterStatuses: ClusterMeshStatus[]
  issuerName: string
  meshName: string
  meshNamespace: string
}

/** Card displaying per-cluster certificate and ManifestWork trust distribution status for a mesh. */
export const TrustStatusCard: FC<TrustStatusCardProps> = ({
  clusterStatuses,
  issuerName,
  meshName,
  meshNamespace,
}) => {
  const { t } = useMeshTranslation()
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

  useEffect(() => {
    if (certsError) console.error('Failed to load certificate data:', certsError)
  }, [certsError])

  const certsByCluster = useMemo(() => {
    const map = new Map<string, Certificate>()
    for (const cert of certs ?? []) {
      const clusterName = cert.metadata?.labels?.[CLUSTER_NAME_LABEL]
      if (clusterName) map.set(clusterName, cert)
    }
    return map
  }, [certs])

  const mwByCluster = useMemo(() => {
    const map = new Map<string, ManifestWork>()
    for (const mw of manifestWorks ?? []) {
      const clusterName = mw.metadata?.labels?.[CLUSTER_NAME_LABEL]
      if (clusterName) map.set(clusterName, mw)
    }
    return map
  }, [manifestWorks])

  const trustCategorize = useCallback(
    (cs: ClusterMeshStatus) => categorizeTrust(certsByCluster.get(cs.clusterName), mwByCluster.get(cs.clusterName)),
    [certsByCluster, mwByCluster],
  )

  const columns = useMemo<VirtualFilterColumn<ClusterMeshStatus>[]>(() => [
    {
      key: 'cluster',
      label: 'Cluster',
      render: (cs) => (
        <Link to={clusterDetailLink(cs.clusterName)}>{cs.clusterName}</Link>
      ),
      width: '20%',
    },
    {
      key: 'certificate',
      label: 'Certificate',
      render: (cs) => certStatusLabel(certsByCluster.get(cs.clusterName), t),
      width: '20%',
    },
    {
      key: 'expires',
      label: 'Expires',
      render: (cs) => {
        const cert = certsByCluster.get(cs.clusterName)
        return cert?.status?.notAfter ? <Timestamp timestamp={cert.status.notAfter} /> : '-'
      },
      width: '20%',
    },
    {
      key: 'renews',
      label: 'Renews',
      render: (cs) => {
        const cert = certsByCluster.get(cs.clusterName)
        return cert?.status?.renewalTime ? <Timestamp timestamp={cert.status.renewalTime} /> : '-'
      },
      width: '20%',
    },
    {
      key: 'distribution',
      label: 'Distribution',
      render: (cs) => distributionStatusLabel(mwByCluster.get(cs.clusterName), mwError, t),
      width: '20%',
    },
  ], [certsByCluster, mwByCluster, mwError, t])

  if (!hasIssuer) {
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Trust Status')}</strong></CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>
              <Trans t={t} i18nKey="trustNotConfiguredMessage" components={{ code: <code /> }} />
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  if (certsError) {
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Trust Status')}</strong></CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <Title headingLevel="h4" size="md">{t('Unable to load certificate data')}</Title>
            <EmptyStateBody>
              {t('An unexpected error occurred. Check the browser console for details.')}
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  if (!certsLoaded || (!mwLoaded && !mwError)) {
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Trust Status')}</strong></CardTitle>
        <CardBody>
          <Spinner aria-label={t('Loading trust status')} size="lg" />
        </CardBody>
      </Card>
    )
  }

  if (clusterStatuses.length === 0) {
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Trust Status')}</strong></CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>{t('No clusters are part of this mesh yet.')}</EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  const noCertsAtAll = certsByCluster.size === 0 && mwByCluster.size === 0
  if (noCertsAtAll) {
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Trust Status')}</strong></CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>
              {t('No certificates have been created yet — the controller may still be reconciling.')}
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  return (
    <Card isCompact>
      <CardTitle><strong>{t('Trust Status ({{count}})', { count: clusterStatuses.length })}</strong></CardTitle>
      <CardBody>
        <VirtualFilterTable
          categorize={trustCategorize}
          categoryLabels={TRUST_CATEGORY_LABELS}
          columns={columns}
          emptyMessage="No clusters match the current filter."
          items={clusterStatuses}
          rowKey={clusterMeshStatusRowKey}
          searchMatch={clusterMeshStatusSearchMatch}
          searchPlaceholder="Filter by cluster name"
        />
      </CardBody>
    </Card>
  )
}
