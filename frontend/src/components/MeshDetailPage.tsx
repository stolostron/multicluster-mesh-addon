import { useEffect, useMemo, useState } from 'react'
import type { FC } from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import {
  useK8sWatchResource,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  Card,
  CardBody,
  CardTitle,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Divider,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Grid,
  GridItem,
  Label,
  PageSection,
  SearchInput,
  Spinner,
  Title,
  ToggleGroup,
  ToggleGroupItem,
} from '@patternfly/react-core'
import type { MultiClusterMesh, K8sCondition, ClusterMeshStatus } from '../types/multiClusterMesh'
import { multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'
import type { EnrichedControlPlane } from '../types/istio'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../hooks/useEnrichedControlPlanes'
import { useManagedClusters } from '../hooks/useManagedClusters'
import type { ManagedCluster } from '../types/managedCluster'
import { getClusterAvailability, availabilityColor, availabilityLabelKey } from '../types/managedCluster'
import { ControlPlanesCard } from './ControlPlanesCard'
import { MeshStatus, statusIcon } from './MeshStatus'
import { TrustStatusCard } from './TrustStatusCard'
import { useMeshTranslation } from '../utils/i18nUtils'

function conditionMessage(condition: K8sCondition): string {
  if (condition.message) return condition.message
  if (condition.reason) return condition.reason
  return condition.status
}

type ClusterStatusCategory = 'all' | 'ready' | 'notReady' | 'unknown'

function categorizeCluster(cs: ClusterMeshStatus): ClusterStatusCategory {
  const op = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
  if (!op) return 'unknown'
  if (op.status === 'True') return 'ready'
  if (op.status === 'Unknown') return 'unknown'
  return 'notReady'
}

const CONFLICT_REASONS = ['OperatorConfigConflict', 'NamespaceConflict']

/** Per-cluster operator status table with filter toggles and search for a single mesh. */
export const ClusterStatusSection: FC<{
  clusterStatuses: ClusterMeshStatus[]
  managedClusterMap?: Map<string, ManagedCluster>
  managedClustersLoaded?: boolean
  meshConditions?: K8sCondition[]
}> = ({
  clusterStatuses,
  managedClusterMap,
  managedClustersLoaded = true,
  meshConditions,
}) => {
  const { t } = useMeshTranslation()
  const [filter, setFilter] = useState<ClusterStatusCategory>('all')
  const [search, setSearch] = useState('')

  const categoryMap = useMemo(() => {
    const map = new Map<string, ClusterStatusCategory>()
    clusterStatuses.forEach((cs) => map.set(cs.clusterName, categorizeCluster(cs)))
    return map
  }, [clusterStatuses])

  const counts = useMemo(() => {
    const result = { ready: 0, notReady: 0, unknown: 0 }
    categoryMap.forEach((cat) => { if (cat !== 'all') result[cat]++ })
    return result
  }, [categoryMap])

  const filtered = useMemo(() => {
    return clusterStatuses.filter((cs) => {
      if (filter !== 'all' && categoryMap.get(cs.clusterName) !== filter) return false
      if (search && !cs.clusterName.toLowerCase().includes(search.toLowerCase())) return false
      return true
    })
  }, [clusterStatuses, categoryMap, filter, search])

  if (clusterStatuses.length === 0) {
    const readyCondition = meshConditions?.find((c) => c.type === 'Ready')
    const isConflict = readyCondition && CONFLICT_REASONS.includes(readyCondition.reason ?? '')
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Clusters (0)')}</strong></CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>
              {isConflict
                ? t('This mesh is blocked: {{reason}}. Resolve the conflict to allow reconciliation.', {
                    reason: readyCondition?.message || readyCondition?.reason,
                  })
                : t('No clusters are part of this mesh yet.')}
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  return (
    <Card isCompact>
      <CardTitle><strong>{t('Clusters ({{count}})', { count: clusterStatuses.length })}</strong></CardTitle>
      <CardBody>
        <Grid hasGutter>
          <GridItem span={12}>
            <Flex style={{ marginBottom: '1rem' }}>
              <FlexItem>
                <ToggleGroup>
                  <ToggleGroupItem
                    text={t('All ({{count}})', { count: clusterStatuses.length })}
                    isSelected={filter === 'all'}
                    onChange={() => setFilter('all')}
                  />
                  <ToggleGroupItem
                    text={t('Ready ({{count}})', { count: counts.ready })}
                    isSelected={filter === 'ready'}
                    onChange={() => setFilter('ready')}
                  />
                  <ToggleGroupItem
                    text={t('Not Ready ({{count}})', { count: counts.notReady })}
                    isSelected={filter === 'notReady'}
                    onChange={() => setFilter('notReady')}
                  />
                  <ToggleGroupItem
                    text={t('Unknown ({{count}})', { count: counts.unknown })}
                    isSelected={filter === 'unknown'}
                    onChange={() => setFilter('unknown')}
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
                    <th className="pf-v6-c-table__th" scope="col">{t('Cluster Status')}</th>
                    <th className="pf-v6-c-table__th" scope="col">{t('Operator Status')}</th>
                    <th className="pf-v6-c-table__th" scope="col">{t('Message')}</th>
                  </tr>
                </thead>
                <tbody className="pf-v6-c-table__tbody">
                  {filtered.length === 0 ? (
                    <tr className="pf-v6-c-table__tr">
                      <td className="pf-v6-c-table__td" colSpan={4} style={{ textAlign: 'center' }}>
                        {t('No clusters match the current filter.')}
                      </td>
                    </tr>
                  ) : (
                    filtered.map((cs) => {
                      const operatorCondition = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
                      return (
                        <tr className="pf-v6-c-table__tr" key={cs.clusterName}>
                          <td className="pf-v6-c-table__td">
                            <Link to={`/multicloud/infrastructure/clusters/details/${cs.clusterName}/${cs.clusterName}/overview`}>
                              {cs.clusterName}
                            </Link>
                          </td>
                          <td className="pf-v6-c-table__td">
                            {managedClustersLoaded ? (() => {
                              const availability = getClusterAvailability(managedClusterMap?.get(cs.clusterName))
                              return <Label color={availabilityColor(availability)} isCompact>{t(availabilityLabelKey(availability))}</Label>
                            })() : '-'}
                          </td>
                          <td className="pf-v6-c-table__td">
                            <MeshStatus conditions={cs.conditions} conditionType="OperatorInstalled" isCompact />
                          </td>
                          <td className="pf-v6-c-table__td">
                            {operatorCondition ? conditionMessage(operatorCondition) : '-'}
                          </td>
                        </tr>
                      )
                    })
                  )}
                </tbody>
              </table>
            </div>
          </GridItem>
        </Grid>
      </CardBody>
    </Card>
  )
}

const MeshDetailContent: FC<{ ns: string; name: string }> = ({ ns, name }) => {
  const { t } = useMeshTranslation()
  const [mesh, loaded, loadError] = useK8sWatchResource<MultiClusterMesh>({
    groupVersionKind: multiClusterMeshGroupVersionKind,
    name,
    namespace: ns,
  })
  const [mcms] = useMultiClusterMeshes()
  const [managedClusters, managedClustersLoaded] = useManagedClusters()
  const { results: searchResults } = useDiscoveredControlPlanes()
  const [enrichedPlanes, , , enrichmentError] = useEnrichedControlPlanes(searchResults, mcms ?? [])
  const managedClusterMap = useMemo(() => {
    const map = new Map<string, ManagedCluster>()
    for (const mc of managedClusters ?? []) {
      if (mc.metadata?.name) map.set(mc.metadata.name, mc)
    }
    return map
  }, [managedClusters])
  const managedPlanes = useMemo(
    () => enrichedPlanes.filter((cp) => cp.managedBy?.name === name && cp.managedBy?.namespace === ns),
    [enrichedPlanes, name, ns],
  )

  useEffect(() => {
    if (loadError) console.error('Failed to load mesh:', loadError)
  }, [loadError])

  if (loadError) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Error loading mesh')}</Title>
          <EmptyStateBody>
            {t('An unexpected error occurred. Check the browser console for details.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  if (!loaded) {
    return (
      <PageSection>
        <Spinner aria-label={t('Loading mesh details')} />
      </PageSection>
    )
  }

  if (!mesh) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Mesh not found')}</Title>
          <EmptyStateBody>
            {t('MultiClusterMesh "{{name}}" was not found in namespace "{{ns}}".', { name, ns })}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  const spec = mesh.spec
  const status = mesh.status
  const clusterStatuses = status?.clusterStatus ?? []
  const conditions = status?.conditions ?? []
  const issuerRef = spec.security?.trust?.certManager?.issuerRef
  const issuerName = issuerRef?.name

  return (
    <>
      <PageSection>
        <Breadcrumb>
          <BreadcrumbItem>
            <Link to="/fleet-mesh/meshes">{t('Meshes')}</Link>
          </BreadcrumbItem>
          <BreadcrumbItem>{t('Managed')}</BreadcrumbItem>
          <BreadcrumbItem isActive>{mesh.metadata?.name}</BreadcrumbItem>
        </Breadcrumb>
        <Flex alignItems={{ default: 'alignItemsCenter' }} style={{ marginTop: '1rem' }}>
          <FlexItem>
            <Title headingLevel="h1">{mesh.metadata?.name}</Title>
          </FlexItem>
          <FlexItem>
            <MeshStatus conditions={conditions} conditionType="Ready" />
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <Grid hasGutter>
          {!!enrichmentError && (
            <GridItem span={12}>
              <Alert
                variant="warning"
                isInline
                title={t('Unable to load control plane data. Some information may be incomplete.')}
              />
            </GridItem>
          )}

          <GridItem span={12}>
            <Card isCompact>
              <CardBody>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Cluster Set')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Link to={`/multicloud/infrastructure/clusters/sets/details/${encodeURIComponent(spec.clusterSet)}/overview`}>
                        {spec.clusterSet}
                      </Link>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Control Plane Namespace')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.controlPlane?.namespace || 'istio-system'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('cert-manager Issuer')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {issuerName
                        ? `${issuerName} (${issuerRef?.kind || 'Issuer'})`
                        : t('Not configured')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Created')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={mesh.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
                <Divider style={{ margin: '0.75rem 0' }} />
                <Title headingLevel="h4" size="md" style={{ marginBottom: '0.5rem' }}>{t('OSSM Operator')}</Title>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Namespace')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.namespace || t('(platform default)')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Channel')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.channel || 'stable'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Source')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.source || t('(platform default)')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Install Plan Approval')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.installPlanApproval || 'Automatic'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={12}>
            <ClusterStatusSection clusterStatuses={clusterStatuses} managedClusterMap={managedClusterMap} managedClustersLoaded={managedClustersLoaded} meshConditions={conditions} />
          </GridItem>

          <GridItem span={12}>
            <ControlPlanesCard planes={managedPlanes} />
          </GridItem>

          <GridItem span={12}>
            <TrustStatusCard
              clusterStatuses={clusterStatuses}
              issuerName={issuerName ?? ''}
              meshName={mesh.metadata?.name ?? ''}
              meshNamespace={ns}
            />
          </GridItem>

          {conditions.length > 0 && (
            <GridItem span={12}>
              <Card isCompact>
                <CardTitle><strong>{t('Conditions')}</strong></CardTitle>
                <CardBody>
                  <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
                    <thead className="pf-v6-c-table__thead">
                      <tr className="pf-v6-c-table__tr">
                        <th className="pf-v6-c-table__th" scope="col">{t('Type')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Status')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Reason')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Message')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Last Transition')}</th>
                      </tr>
                    </thead>
                    <tbody className="pf-v6-c-table__tbody">
                      {conditions.map((c, i) => (
                        <tr className="pf-v6-c-table__tr" key={`${c.type}-${i}`}>
                          <td className="pf-v6-c-table__td">{c.type}</td>
                          <td className="pf-v6-c-table__td">{statusIcon(c.status)}</td>
                          <td className="pf-v6-c-table__td">{c.reason ?? '-'}</td>
                          <td className="pf-v6-c-table__td">{c.message ?? '-'}</td>
                          <td className="pf-v6-c-table__td">
                            {c.lastTransitionTime ? <Timestamp timestamp={c.lastTransitionTime} /> : '-'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </CardBody>
              </Card>
            </GridItem>
          )}
        </Grid>
      </PageSection>
    </>
  )
}

const MeshDetailPage: FC = () => {
  const { t } = useMeshTranslation()
  const { ns, name } = useParams<{ ns: string; name: string }>()

  if (!ns || !name) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Not Found')}</Title>
          <EmptyStateBody>
            {t('Invalid mesh URL. Expected /fleet-mesh/meshes/managed/:namespace/:name.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <MeshDetailContent ns={ns} name={name} />
}

/** Detail page for a single MultiClusterMesh, reached via /fleet-mesh/meshes/managed/:ns/:name. */
export default MeshDetailPage
