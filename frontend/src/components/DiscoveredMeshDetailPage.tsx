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
  Spinner,
  Title,
  Tooltip,
} from '@patternfly/react-core'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../hooks/useEnrichedControlPlanes'
import type { EnrichedControlPlane } from '../types/istio'
import type { K8sCondition } from '../types/common'
import { MeshStatus, getStatusRank, statusIcon } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

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

const DiscoveredMeshDetailContent: FC<{ meshID: string }> = ({ meshID }) => {
  const { t } = useMeshTranslation()
  const [showAllConditions, setShowAllConditions] = useState(false)
  const [mcms] = useMultiClusterMeshes()
  const { results: searchResults, loaded: searchLoaded, error: searchError } = useDiscoveredControlPlanes()
  const [enrichedPlanes, , enrichmentLoaded, enrichmentError] = useEnrichedControlPlanes(searchResults, mcms ?? [])

  const matchingPlanes = useMemo(
    () => enrichedPlanes.filter((cp) => !cp.managedBy && cp.meshID === meshID),
    [enrichedPlanes, meshID],
  )

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

  const worstConditions = aggregateStatus(matchingPlanes)
  const networks = uniqueNetworks(matchingPlanes)
  const created = oldestTimestamp(matchingPlanes)

  // Check for meshID conflict with managed meshes
  const hasConflict = enrichedPlanes.some((cp) => cp.managedBy && cp.meshID === meshID)

  const allConditions: { clusterName: string; condition: K8sCondition }[] = []
  for (const cp of matchingPlanes) {
    for (const c of cp.status?.conditions ?? []) {
      allConditions.push({ clusterName: cp.clusterName, condition: c })
    }
  }
  const visibleConditions = showAllConditions
    ? allConditions
    : allConditions.filter((entry) => entry.condition.status !== 'True')

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
            <Link to="/service-mesh">{t('Meshes')}</Link>
          </BreadcrumbItem>
          <BreadcrumbItem isActive>{meshID}</BreadcrumbItem>
        </Breadcrumb>
        <Flex alignItems={{ default: 'alignItemsCenter' }} style={{ marginTop: '1rem' }}>
          <FlexItem>
            <Title headingLevel="h1">{meshID}</Title>
          </FlexItem>
          <FlexItem>
            <MeshStatus conditions={worstConditions} conditionType="Ready" />
          </FlexItem>
          <FlexItem>
            <Label color="purple">{t('Discovered')}</Label>
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
              <CardTitle><strong>{t('Overview')}</strong></CardTitle>
              <CardBody>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Mesh ID')}</DescriptionListTerm>
                    <DescriptionListDescription>{meshID}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Networks')}</DescriptionListTerm>
                    <DescriptionListDescription>{networkDisplay}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Clusters')}</DescriptionListTerm>
                    <DescriptionListDescription>{matchingPlanes.length}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Created')}</DescriptionListTerm>
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
              <CardTitle><strong>{t('Control Planes')}</strong></CardTitle>
              <CardBody>
                <div style={{ maxHeight: '400px', overflowY: 'auto' }}>
                  <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
                    <thead className="pf-v6-c-table__thead" style={{ position: 'sticky', top: 0, zIndex: 1 }}>
                      <tr className="pf-v6-c-table__tr">
                        <th className="pf-v6-c-table__th" scope="col">{t('Cluster')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Name')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Namespace')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Version')}</th>
                        <th className="pf-v6-c-table__th" scope="col">{t('Status')}</th>
                      </tr>
                    </thead>
                    <tbody className="pf-v6-c-table__tbody">
                      {matchingPlanes.map((cp) => (
                        <tr className="pf-v6-c-table__tr" key={`${cp.clusterName}/${cp.metadata.name}`}>
                          <td className="pf-v6-c-table__td">
                            <Link to={`/multicloud/infrastructure/clusters/details/${cp.clusterName}/${cp.clusterName}/overview`}>
                              {cp.clusterName}
                            </Link>
                          </td>
                          <td className="pf-v6-c-table__td">
                            <Link to={`/mesh-control-planes/${encodeURIComponent(cp.clusterName)}/${encodeURIComponent(cp.metadata.name)}`}>
                              {cp.metadata.name}
                            </Link>
                          </td>
                          <td className="pf-v6-c-table__td">{cp.controlPlaneNamespace ?? '-'}</td>
                          <td className="pf-v6-c-table__td">{cp.version ?? '-'}</td>
                          <td className="pf-v6-c-table__td">
                            {cp.status?.conditions ? (
                              <MeshStatus conditions={cp.status.conditions} conditionType="Ready" isCompact />
                            ) : (
                              <Label color="grey">{t('Unknown')}</Label>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardBody>
            </Card>
          </GridItem>

          {allConditions.length > 0 && (
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
                    <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
                      <thead className="pf-v6-c-table__thead">
                        <tr className="pf-v6-c-table__tr">
                          <th className="pf-v6-c-table__th" scope="col">{t('Cluster')}</th>
                          <th className="pf-v6-c-table__th" scope="col">{t('Type')}</th>
                          <th className="pf-v6-c-table__th" scope="col">{t('Status')}</th>
                          <th className="pf-v6-c-table__th" scope="col">{t('Reason')}</th>
                          <th className="pf-v6-c-table__th" scope="col">{t('Message')}</th>
                          <th className="pf-v6-c-table__th" scope="col">{t('Last Transition')}</th>
                        </tr>
                      </thead>
                      <tbody className="pf-v6-c-table__tbody">
                        {visibleConditions.map((entry, i) => (
                          <tr className="pf-v6-c-table__tr" key={`${entry.clusterName}-${entry.condition.type}-${i}`}>
                            <td className="pf-v6-c-table__td">{entry.clusterName}</td>
                            <td className="pf-v6-c-table__td">{entry.condition.type}</td>
                            <td className="pf-v6-c-table__td">{statusIcon(entry.condition.status)}</td>
                            <td className="pf-v6-c-table__td">{entry.condition.reason ?? '-'}</td>
                            <td className="pf-v6-c-table__td">{entry.condition.message ?? '-'}</td>
                            <td className="pf-v6-c-table__td">
                              {entry.condition.lastTransitionTime ? <Timestamp timestamp={entry.condition.lastTransitionTime} /> : '-'}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
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
            {t('Invalid mesh URL. Expected /fleet-mesh-discovered/:meshID.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <DiscoveredMeshDetailContent meshID={decodeURIComponent(meshID)} />
}

/** Detail page for a discovered (unmanaged) mesh grouped by meshID. */
export default DiscoveredMeshDetailPage
