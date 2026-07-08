import { useCallback, useMemo, useState } from 'react'
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
  Spinner,
  Title,
  Tooltip,
} from '@patternfly/react-core'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../hooks/useEnrichedControlPlanes'
import { useManagedClusterMap } from '../hooks/useManagedClusterMap'
import type { EnrichedControlPlane } from '../types/istio'
import type { K8sCondition } from '../types/common'
import type { ClusterAvailability } from '../types/managedCluster'
import { getClusterAvailability, availabilityColor, availabilityLabelKey } from '../types/managedCluster'
import { oldestTimestamp } from '../utils/oldestTimestamp'
import { worstConditions } from '../utils/worstConditions'
import { clusterDetailLink } from '../utils/linkUtils'
import { ControlPlanesCard } from './ControlPlanesCard'
import { MeshStatus, statusIcon } from './MeshStatus'
import { VirtualFilterTable } from './VirtualFilterTable'
import type { CategoryLabel, VirtualFilterColumn } from './VirtualFilterTable'
import { useVirtualRows } from '../hooks/useVirtualRows'
import { useMeshTranslation } from '../utils/i18nUtils'

const CONDITION_COL_WIDTHS = ['12%', '12%', '14%', '10%', '12%', '25%', '15%']

const discoveredClusterRowKey = (name: string) => name
const discoveredClusterSearchMatch = (name: string, query: string) =>
  name.toLowerCase().includes(query.toLowerCase())

const DISCOVERED_CLUSTER_CATEGORIES: CategoryLabel[] = [
  { key: 'all', label: 'All ({{count}})' },
  { key: 'available', label: 'Available ({{count}})' },
  { key: 'unavailable', label: 'Unavailable ({{count}})' },
  { key: 'unreachable', label: 'Unreachable ({{count}})' },
]

function uniqueNetworks(planes: EnrichedControlPlane[]): string[] {
  const networks = new Set<string>()
  for (const cp of planes) {
    if (cp.network) networks.add(cp.network)
  }
  return [...networks].sort()
}

const DiscoveredMeshDetailContent: FC<{ meshID: string }> = ({ meshID }) => {
  const { t } = useMeshTranslation()
  const [showAllConditions, setShowAllConditions] = useState(false)
  const [mcms] = useMultiClusterMeshes()
  const [managedClusterMap] = useManagedClusterMap()
  const { results: searchResults, loaded: searchLoaded, error: searchError } = useDiscoveredControlPlanes()
  const [enrichedPlanes, , enrichmentLoaded, enrichmentError] = useEnrichedControlPlanes(searchResults, mcms ?? [])

  const matchingPlanes = useMemo(
    () => enrichedPlanes.filter((cp) => !cp.managedBy && cp.meshID === meshID),
    [enrichedPlanes, meshID],
  )

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

  const discoveredClusterCategorize = useCallback(
    (name: string) => clusterAvailabilityMap.get(name) ?? 'unreachable',
    [clusterAvailabilityMap],
  )

  const clusterColumns = useMemo<VirtualFilterColumn<string>[]>(() => [
    {
      key: 'cluster',
      label: 'Cluster',
      render: (clusterName) => (
        <Link to={clusterDetailLink(clusterName)}>{clusterName}</Link>
      ),
      width: '60%',
    },
    {
      key: 'clusterStatus',
      label: 'Cluster Status',
      render: (clusterName) => {
        const availability = clusterAvailabilityMap.get(clusterName) ?? 'unreachable'
        return <Label color={availabilityColor(availability)} isCompact>{t(availabilityLabelKey(availability))}</Label>
      },
      width: '40%',
    },
  ], [clusterAvailabilityMap, t])

  const worstConds = useMemo(() => worstConditions(matchingPlanes).conditions, [matchingPlanes])
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
            <MeshStatus conditions={worstConds} conditionType="Ready" />
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
                <VirtualFilterTable
                  categorize={discoveredClusterCategorize}
                  categoryLabels={DISCOVERED_CLUSTER_CATEGORIES}
                  columns={clusterColumns}
                  emptyMessage="No clusters match the current filter."
                  items={uniqueClusterNames}
                  rowKey={discoveredClusterRowKey}
                  searchMatch={discoveredClusterSearchMatch}
                  searchPlaceholder="Filter by cluster name"
                />
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
                        <colgroup>
                          {CONDITION_COL_WIDTHS.map((w, i) => <col key={i} style={{ width: w }} />)}
                        </colgroup>
                        <tbody className="pf-v6-c-table__tbody">
                          {condTopSpacer > 0 && <tr><td colSpan={7} style={{ height: condTopSpacer, padding: 0, border: 'none' }} /></tr>}
                          {visibleConditionRows.map((entry, i) => (
                            <tr className="pf-v6-c-table__tr" key={`${entry.clusterName}-${entry.cpName}-${entry.condition.type}-${i}`}>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[0] }}>{entry.clusterName}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[1] }}>{entry.cpName}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[2] }}>{entry.condition.type}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[3] }}>{statusIcon(entry.condition.status)}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[4] }}>{entry.condition.reason ?? '-'}</td>
                              <td className="pf-v6-c-table__td" style={{ width: CONDITION_COL_WIDTHS[5], overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.condition.message ?? '-'}</td>
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
