import { useMemo, useState } from 'react'
import type { FC } from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import {
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  Button,
  Card,
  CardBody,
  CardTitle,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
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
  Tooltip,
} from '@patternfly/react-core'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../hooks/useEnrichedControlPlanes'
import { useManagedClusters } from '../hooks/useManagedClusters'
import type { EnrichedControlPlane } from '../types/istio'
import type { K8sCondition } from '../types/common'
import type { ClusterAvailability, ManagedCluster } from '../types/managedCluster'
import { getClusterAvailability, availabilityColor, availabilityLabelKey } from '../types/managedCluster'
import { ControlPlanesCard } from './ControlPlanesCard'
import { MeshStatus, getStatusRank, statusIcon } from './MeshStatus'
import { useVirtualRows } from '../hooks/useVirtualRows'
import { useMeshTranslation } from '../utils/i18nUtils'

const CLUSTER_COL_WIDTHS = ['60%', '40%']
const CONDITION_COL_WIDTHS = ['12%', '12%', '14%', '10%', '12%', '25%', '15%']

function aggregateStatus(planes: EnrichedControlPlane[]): K8sCondition[] | undefined {
  let worstRank = -1
  let worstConditions: K8sCondition[] | undefined
  for (const cp of planes) {
    const rank = getStatusRank(cp.status?.conditions)
    if (rank > worstRank) {
      worstRank = rank
      worstConditions = cp.status?.conditions
    }
  }
  return worstConditions
}

function uniqueNetworks(planes: EnrichedControlPlane[]): string[] {
  const networks = new Set<string>()
  for (const cp of planes) {
    if (cp.network) networks.add(cp.network)
  }
  return [...networks].sort()
}

function oldestTimestamp(planes: EnrichedControlPlane[]): string | undefined {
  let oldest: string | undefined
  for (const cp of planes) {
    const ts = cp.metadata.creationTimestamp
    if (ts && (!oldest || ts < oldest)) oldest = ts
  }
  return oldest
}

type ClusterAvailabilityCategory = 'all' | 'available' | 'unavailable' | 'unreachable'

const DiscoveredMeshDetailContent: FC<{ meshID: string }> = ({ meshID }) => {
  const { t } = useMeshTranslation()
  const [showAllConditions, setShowAllConditions] = useState(false)
  const [clusterFilter, setClusterFilter] = useState<ClusterAvailabilityCategory>('all')
  const [clusterSearch, setClusterSearch] = useState('')
  const [mcms] = useMultiClusterMeshes()
  const [managedClusters] = useManagedClusters()
  const { results: searchResults, loaded: searchLoaded, error: searchError } = useDiscoveredControlPlanes()
  const [enrichedPlanes, , enrichmentLoaded, enrichmentError] = useEnrichedControlPlanes(searchResults, mcms ?? [])

  const matchingPlanes = useMemo(
    () => enrichedPlanes.filter((cp) => !cp.managedBy && cp.meshID === meshID),
    [enrichedPlanes, meshID],
  )

  const managedClusterMap = useMemo(() => {
    const map = new Map<string, ManagedCluster>()
    for (const mc of managedClusters ?? []) {
      if (mc.metadata?.name) map.set(mc.metadata.name, mc)
    }
    return map
  }, [managedClusters])

  const uniqueClusterNames = useMemo(() => {
    const names = new Set<string>()
    for (const cp of matchingPlanes) names.add(cp.clusterName)
    return [...names].sort()
  }, [matchingPlanes])

  const clusterAvailabilityMap = useMemo(() => {
    const map = new Map<string, ClusterAvailability>()
    for (const name of uniqueClusterNames) {
      map.set(name, getClusterAvailability(managedClusterMap.get(name)))
    }
    return map
  }, [uniqueClusterNames, managedClusterMap])

  const clusterCounts = useMemo(() => {
    const result = { available: 0, unavailable: 0, unreachable: 0 }
    clusterAvailabilityMap.forEach((cat) => { result[cat]++ })
    return result
  }, [clusterAvailabilityMap])

  const filteredClusters = useMemo(() => {
    return uniqueClusterNames.filter((name) => {
      if (clusterFilter !== 'all' && clusterAvailabilityMap.get(name) !== clusterFilter) return false
      if (clusterSearch && !name.toLowerCase().includes(clusterSearch.toLowerCase())) return false
      return true
    })
  }, [uniqueClusterNames, clusterAvailabilityMap, clusterFilter, clusterSearch])

  const { visibleItems: visibleClusters, topSpacer: clusterTopSpacer, bottomSpacer: clusterBottomSpacer, containerRef: clusterContainerRef } = useVirtualRows(filteredClusters)

  const worstConditions = useMemo(() => aggregateStatus(matchingPlanes), [matchingPlanes])
  const networks = useMemo(() => uniqueNetworks(matchingPlanes), [matchingPlanes])
  const created = useMemo(() => oldestTimestamp(matchingPlanes), [matchingPlanes])

  const hasConflict = useMemo(
    () => enrichedPlanes.some((cp) => cp.managedBy && cp.meshID === meshID),
    [enrichedPlanes, meshID],
  )

  const visibleConditions = useMemo(() => {
    const all: { clusterName: string; cpName: string; condition: K8sCondition }[] = []
    for (const cp of matchingPlanes) {
      for (const c of cp.status?.conditions ?? []) {
        all.push({ clusterName: cp.clusterName, cpName: cp.metadata.name, condition: c })
      }
    }
    return showAllConditions ? all : all.filter((entry) => entry.condition.status !== 'True')
  }, [matchingPlanes, showAllConditions])

  const { visibleItems: visibleConditionRows, topSpacer: condTopSpacer, bottomSpacer: condBottomSpacer, containerRef: condContainerRef } = useVirtualRows(visibleConditions)

  const loaded = searchLoaded && enrichmentLoaded

  if (!loaded) {
    return (
      <PageSection>
        <Spinner aria-label={t('Loading mesh details')} />
      </PageSection>
    )
  }

  if (searchError) {
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

  if (matchingPlanes.length === 0) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Mesh not found')}</Title>
          <EmptyStateBody>
            {t('Discovered mesh "{{meshID}}" was not found.', { meshID })}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  const networkDisplay = networks.length === 0
    ? '-'
    : networks.length <= 2
      ? networks.join(', ')
      : (
          <Tooltip content={networks.join(', ')}>
            <span>{t('Multiple networks')}</span>
          </Tooltip>
        )

  return (
    <>
      <PageSection>
        <Breadcrumb>
          <BreadcrumbItem>
            <Link to="/fleet-mesh/meshes">{t('Meshes')}</Link>
          </BreadcrumbItem>
          <BreadcrumbItem>{t('Discovered')}</BreadcrumbItem>
          <BreadcrumbItem isActive>{meshID}</BreadcrumbItem>
        </Breadcrumb>
        <Flex alignItems={{ default: 'alignItemsCenter' }} style={{ marginTop: '1rem' }}>
          <FlexItem>
            <Title headingLevel="h1">{meshID}</Title>
          </FlexItem>
          <FlexItem>
            <MeshStatus conditions={worstConditions} conditionType="Ready" />
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

          {hasConflict && (
            <GridItem span={12}>
              <Alert
                variant="warning"
                isInline
                title={t('Mesh ID Conflict')}
              >
                {t('This mesh ID is also used by a managed mesh. This is a misconfiguration — each mesh ID should belong to exactly one mesh.')}
              </Alert>
            </GridItem>
          )}

          <GridItem span={5}>
            <Card isCompact>
              <CardBody>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Mesh ID')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{meshID}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Networks')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{networkDisplay}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Clusters')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{uniqueClusterNames.length}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Created')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {created ? <Timestamp timestamp={created} /> : '-'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={12}>
            <Card isCompact>
              <CardTitle><strong>{t('Clusters ({{count}})', { count: uniqueClusterNames.length })}</strong></CardTitle>
              <CardBody>
                <Flex style={{ marginBottom: '1rem' }}>
                  <FlexItem>
                    <ToggleGroup>
                      <ToggleGroupItem
                        text={t('All ({{count}})', { count: uniqueClusterNames.length })}
                        isSelected={clusterFilter === 'all'}
                        onChange={() => setClusterFilter('all')}
                      />
                      <ToggleGroupItem
                        text={t('Available ({{count}})', { count: clusterCounts.available })}
                        isSelected={clusterFilter === 'available'}
                        onChange={() => setClusterFilter('available')}
                      />
                      <ToggleGroupItem
                        text={t('Unavailable ({{count}})', { count: clusterCounts.unavailable })}
                        isSelected={clusterFilter === 'unavailable'}
                        onChange={() => setClusterFilter('unavailable')}
                      />
                      <ToggleGroupItem
                        text={t('Unreachable ({{count}})', { count: clusterCounts.unreachable })}
                        isSelected={clusterFilter === 'unreachable'}
                        onChange={() => setClusterFilter('unreachable')}
                      />
                    </ToggleGroup>
                  </FlexItem>
                  <FlexItem grow={{ default: 'grow' }}>
                    <SearchInput
                      placeholder={t('Filter by cluster name')}
                      value={clusterSearch}
                      onChange={(_event, value) => setClusterSearch(value)}
                      onClear={() => setClusterSearch('')}
                    />
                  </FlexItem>
                </Flex>

                <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid" style={{ tableLayout: 'fixed' }}>
                  <thead className="pf-v6-c-table__thead">
                    <tr className="pf-v6-c-table__tr">
                      <th className="pf-v6-c-table__th" scope="col" style={{ width: CLUSTER_COL_WIDTHS[0] }}>{t('Cluster')}</th>
                      <th className="pf-v6-c-table__th" scope="col" style={{ width: CLUSTER_COL_WIDTHS[1] }}>{t('Cluster Status')}</th>
                    </tr>
                  </thead>
                </table>
                <div ref={clusterContainerRef} style={{ maxHeight: '368px', overflowY: 'auto' }}>
                  {filteredClusters.length === 0 ? (
                    <div style={{ textAlign: 'center', padding: '1rem' }}>
                      {t('No clusters match the current filter.')}
                    </div>
                  ) : (
                    <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid" style={{ tableLayout: 'fixed' }}>
                      <tbody className="pf-v6-c-table__tbody">
                        {clusterTopSpacer > 0 && <tr><td colSpan={2} style={{ height: clusterTopSpacer, padding: 0, border: 'none' }} /></tr>}
                        {visibleClusters.map((clusterName) => {
                          const availability = clusterAvailabilityMap.get(clusterName) ?? 'unreachable'
                          return (
                            <tr className="pf-v6-c-table__tr" key={clusterName}>
                              <td className="pf-v6-c-table__td" style={{ width: CLUSTER_COL_WIDTHS[0] }}>
                                <Link to={`/multicloud/infrastructure/clusters/details/${clusterName}/${clusterName}/overview`}>
                                  {clusterName}
                                </Link>
                              </td>
                              <td className="pf-v6-c-table__td" style={{ width: CLUSTER_COL_WIDTHS[1] }}>
                                <Label color={availabilityColor(availability)} isCompact>{t(availabilityLabelKey(availability))}</Label>
                              </td>
                            </tr>
                          )
                        })}
                        {clusterBottomSpacer > 0 && <tr><td colSpan={2} style={{ height: clusterBottomSpacer, padding: 0, border: 'none' }} /></tr>}
                      </tbody>
                    </table>
                  )}
                </div>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={12}>
            <ControlPlanesCard planes={matchingPlanes} />
          </GridItem>

          {matchingPlanes.some((cp) => cp.status?.conditions?.length) && (
            <GridItem span={12}>
              <Card isCompact>
                <CardTitle>
                  <Flex justifyContent={{ default: 'justifyContentSpaceBetween' }} alignItems={{ default: 'alignItemsCenter' }}>
                    <FlexItem><strong>{t('Conditions')}</strong></FlexItem>
                    <FlexItem>
                      <Button
                        variant="link"
                        onClick={() => setShowAllConditions((v) => !v)}
                      >
                        {showAllConditions ? t('Show issues only') : t('Show all conditions')}
                      </Button>
                    </FlexItem>
                  </Flex>
                </CardTitle>
                <CardBody>
                  {visibleConditions.length === 0 ? (
                    <EmptyState variant="xs">
                      <EmptyStateBody>{t('No issues detected.')}</EmptyStateBody>
                    </EmptyState>
                  ) : (
                    <>
                    <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid" style={{ tableLayout: 'fixed' }}>
                      <thead className="pf-v6-c-table__thead">
                        <tr className="pf-v6-c-table__tr">
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[0] }}>{t('Cluster')}</th>
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[1] }}>{t('Control Plane')}</th>
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[2] }}>{t('Type')}</th>
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[3] }}>{t('Status')}</th>
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[4] }}>{t('Reason')}</th>
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[5] }}>{t('Message')}</th>
                          <th className="pf-v6-c-table__th" scope="col" style={{ width: CONDITION_COL_WIDTHS[6] }}>{t('Last Transition')}</th>
                        </tr>
                      </thead>
                    </table>
                    <div ref={condContainerRef} style={{ maxHeight: '368px', overflowY: 'auto' }}>
                      <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid" style={{ tableLayout: 'fixed' }}>
                        <tbody className="pf-v6-c-table__tbody">
                          {condTopSpacer > 0 && <tr><td colSpan={7} style={{ height: condTopSpacer, padding: 0, border: 'none' }} /></tr>}
                          {visibleConditionRows.map((entry, i) => (
                            <tr className="pf-v6-c-table__tr" key={`${entry.clusterName}-${entry.cpName}-${entry.condition.type}-${i}`}>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[0] }}>{entry.clusterName}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[1] }}>{entry.cpName}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[2] }}>{entry.condition.type}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[3] }}>{statusIcon(entry.condition.status)}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[4] }}>{entry.condition.reason ?? '-'}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[5] }}>{entry.condition.message ?? '-'}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[6] }}>
                                {entry.condition.lastTransitionTime ? <Timestamp timestamp={entry.condition.lastTransitionTime} /> : '-'}
                              </td>
                            </tr>
                          ))}
                          {condBottomSpacer > 0 && <tr><td colSpan={7} style={{ height: condBottomSpacer, padding: 0, border: 'none' }} /></tr>}
                        </tbody>
                      </table>
                    </div>
                    </>
                  )}
                </CardBody>
              </Card>
            </GridItem>
          )}
        </Grid>
      </PageSection>
    </>
  )
}

const DiscoveredMeshDetailPage: FC = () => {
  const { t } = useMeshTranslation()
  const { meshID } = useParams<{ meshID: string }>()

  if (!meshID) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Not Found')}</Title>
          <EmptyStateBody>
            {t('Invalid mesh URL. Expected /fleet-mesh/meshes/discovered/:meshID.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <DiscoveredMeshDetailContent meshID={decodeURIComponent(meshID)} />
}

/** Detail page for a discovered (unmanaged) mesh grouped by meshID. */
export default DiscoveredMeshDetailPage
