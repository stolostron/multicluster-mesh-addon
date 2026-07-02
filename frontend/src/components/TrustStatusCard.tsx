import { useEffect, useMemo, useState } from 'react'
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
  Flex,
  FlexItem,
  Label,
  SearchInput,
  Spinner,
  Title,
  ToggleGroup,
  ToggleGroupItem,
} from '@patternfly/react-core'
import type { ClusterMeshStatus } from '../types/multiClusterMesh'
import type { Certificate } from '../types/certManager'
import { certificateGroupVersionKind } from '../types/certManager'
import type { ManifestWork } from '../types/manifestWork'
import { manifestWorkGroupVersionKind } from '../types/manifestWork'
import type { K8sCondition } from '../types/common'
import { useMeshTranslation } from '../utils/i18nUtils'

const CLUSTER_NAME_LABEL = 'mesh.open-cluster-management.io/cluster-name'
const MESH_NAME_LABEL = 'mesh.open-cluster-management.io/mesh-name'
const MESH_NAMESPACE_LABEL = 'mesh.open-cluster-management.io/mesh-namespace'

function findCondition(conditions: K8sCondition[] | undefined, type: string): K8sCondition | undefined {
  return conditions?.find((c) => c.type === type)
}

type TrustCategory = 'all' | 'ready' | 'pending' | 'failed'

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
  const [filter, setFilter] = useState<TrustCategory>('all')
  const [search, setSearch] = useState('')

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

  const { categoryByCluster, counts } = useMemo(() => {
    const catMap = new Map<string, TrustCategory>()
    const c = { ready: 0, pending: 0, failed: 0 }
    clusterStatuses.forEach((cs) => {
      const cat = categorizeTrust(certsByCluster.get(cs.clusterName), mwByCluster.get(cs.clusterName))
      catMap.set(cs.clusterName, cat)
      if (cat !== 'all') c[cat]++
    })
    return { categoryByCluster: catMap, counts: c }
  }, [clusterStatuses, certsByCluster, mwByCluster])

  const filtered = useMemo(() => {
    return clusterStatuses.filter((cs) => {
      if (filter !== 'all' && categoryByCluster.get(cs.clusterName) !== filter) return false
      if (search && !cs.clusterName.toLowerCase().includes(search.toLowerCase())) return false
      return true
    })
  }, [clusterStatuses, categoryByCluster, filter, search])

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
        <Flex style={{ marginBottom: '1rem' }} spaceItems={{ default: 'spaceItemsMd' }}>
          <FlexItem>
            <Label color="green" isCompact>{t('{{count}} Distributed', { count: counts.ready })}</Label>
          </FlexItem>
          <FlexItem>
            <Label color="grey" isCompact>{t('{{count}} Pending', { count: counts.pending })}</Label>
          </FlexItem>
          <FlexItem>
            <Label color="red" isCompact>{t('{{count}} Failed', { count: counts.failed })}</Label>
          </FlexItem>
        </Flex>

        <Flex style={{ marginBottom: '1rem' }}>
          <FlexItem>
            <ToggleGroup>
              <ToggleGroupItem
                text={t('All ({{count}})', { count: clusterStatuses.length })}
                isSelected={filter === 'all'}
                onChange={() => setFilter('all')}
              />
              <ToggleGroupItem
                text={t('Distributed ({{count}})', { count: counts.ready })}
                isSelected={filter === 'ready'}
                onChange={() => setFilter('ready')}
              />
              <ToggleGroupItem
                text={t('Pending ({{count}})', { count: counts.pending })}
                isSelected={filter === 'pending'}
                onChange={() => setFilter('pending')}
              />
              <ToggleGroupItem
                text={t('Failed ({{count}})', { count: counts.failed })}
                isSelected={filter === 'failed'}
                onChange={() => setFilter('failed')}
              />
            </ToggleGroup>
          </FlexItem>
          <FlexItem grow={{ default: 'grow' }}>
            <SearchInput
              placeholder={t('Filter by cluster name')}
              value={search}
              onChange={(_event, value) => setSearch(value)}
              onClear={() => setSearch('')}
            />
          </FlexItem>
        </Flex>

        <div style={{ maxHeight: '400px', overflowY: 'auto' }}>
          <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
            <thead className="pf-v6-c-table__thead" style={{ position: 'sticky', top: 0, zIndex: 1 }}>
              <tr className="pf-v6-c-table__tr">
                <th className="pf-v6-c-table__th" scope="col">{t('Cluster')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Certificate')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Expires')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Renews')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Distribution')}</th>
              </tr>
            </thead>
            <tbody className="pf-v6-c-table__tbody">
              {filtered.length === 0 ? (
                <tr className="pf-v6-c-table__tr">
                  <td className="pf-v6-c-table__td" colSpan={5} style={{ textAlign: 'center' }}>
                    {t('No clusters match the current filter.')}
                  </td>
                </tr>
              ) : (
                filtered.map((cs) => {
                  const cert = certsByCluster.get(cs.clusterName)
                  const mw = mwByCluster.get(cs.clusterName)
                  return (
                    <tr className="pf-v6-c-table__tr" key={cs.clusterName}>
                      <td className="pf-v6-c-table__td">
                        <Link to={`/multicloud/infrastructure/clusters/details/${cs.clusterName}/${cs.clusterName}/overview`}>
                          {cs.clusterName}
                        </Link>
                      </td>
                      <td className="pf-v6-c-table__td">{certStatusLabel(cert, t)}</td>
                      <td className="pf-v6-c-table__td">
                        {cert?.status?.notAfter ? <Timestamp timestamp={cert.status.notAfter} /> : '-'}
                      </td>
                      <td className="pf-v6-c-table__td">
                        {cert?.status?.renewalTime ? <Timestamp timestamp={cert.status.renewalTime} /> : '-'}
                      </td>
                      <td className="pf-v6-c-table__td">{distributionStatusLabel(mw, mwError, t)}</td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </CardBody>
    </Card>
  )
}
