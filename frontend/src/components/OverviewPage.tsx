import { useMemo } from 'react'
import type { FC } from 'react'
import { Link } from 'react-router-dom-v5-compat'
import { Timestamp } from '@openshift-console/dynamic-plugin-sdk'
import {
  Alert,
  Card,
  CardBody,
  CardFooter,
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
import { TopologyIcon, ServerIcon } from '@patternfly/react-icons'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../hooks/useEnrichedControlPlanes'
import type { MultiClusterMesh, K8sCondition } from '../types/multiClusterMesh'
import type { EnrichedControlPlane } from '../types/istio'
import type { StatusColor } from './MeshStatus'
import { deriveStatus } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

interface StatusCounts {
  ready: number
  notReady: number
  unknown: number
}

function countByStatus(items: { conditions?: K8sCondition[] }[], conditionType?: string): StatusCounts {
  const counts: StatusCounts = { ready: 0, notReady: 0, unknown: 0 }
  for (const item of items) {
    const { color } = deriveStatus(item.conditions, conditionType)
    if (color === 'green') counts.ready++
    else if (color === 'grey') counts.unknown++
    else counts.notReady++
  }
  return counts
}

function countUniqueClusters(meshes: MultiClusterMesh[]): number {
  const clusters = new Set<string>()
  for (const mesh of meshes) {
    for (const cs of mesh.status?.clusterStatus ?? []) {
      clusters.add(cs.clusterName)
    }
  }
  return clusters.size
}

type IssueKind = 'mesh' | 'controlPlane'

interface RecentIssue {
  color: StatusColor
  kind: IssueKind
  label: string
  lastTransitionTime?: string
  link: string
  source: string
}

const MAX_ISSUES = 5

function collectRecentIssues(meshes: MultiClusterMesh[], controlPlanes: EnrichedControlPlane[]): RecentIssue[] {
  const issues: RecentIssue[] = []

  for (const mesh of meshes) {
    const meshName = mesh.metadata?.name ?? ''
    const meshNamespace = mesh.metadata?.namespace ?? ''
    const meshLink = `/service-mesh/${encodeURIComponent(meshNamespace)}/${encodeURIComponent(meshName)}`

    for (const c of mesh.status?.conditions ?? []) {
      if (c.status === 'True') continue
      const { label, color } = deriveStatus([c], c.type)
      issues.push({ kind: 'mesh', source: meshName, link: meshLink, label, color, lastTransitionTime: c.lastTransitionTime })
    }

    for (const cs of mesh.status?.clusterStatus ?? []) {
      for (const c of cs.conditions ?? []) {
        if (c.status === 'True') continue
        const { label, color } = deriveStatus([c], c.type)
        issues.push({
          kind: 'mesh',
          source: `${meshName} / ${cs.clusterName}`,
          link: meshLink,
          label,
          color,
          lastTransitionTime: c.lastTransitionTime,
        })
      }
    }
  }

  for (const cp of controlPlanes) {
    for (const c of cp.status?.conditions ?? []) {
      if (c.status === 'True') continue
      const { label, color } = deriveStatus([c], c.type)
      issues.push({
        kind: 'controlPlane',
        source: `${cp.clusterName} / ${cp.metadata.name}`,
        link: `/mesh-control-planes/${encodeURIComponent(cp.clusterName)}/${encodeURIComponent(cp.metadata.name)}`,
        label,
        color,
        lastTransitionTime: c.lastTransitionTime,
      })
    }
  }

  issues.sort((a, b) => {
    if (!a.lastTransitionTime) return 1
    if (!b.lastTransitionTime) return -1
    return b.lastTransitionTime.localeCompare(a.lastTransitionTime)
  })

  return issues.slice(0, MAX_ISSUES)
}

const StatusCountLabels: FC<{ counts: StatusCounts }> = ({ counts }) => {
  const { t } = useMeshTranslation()
  return (
    <Flex spaceItems={{ default: 'spaceItemsMd' }}>
      <FlexItem>
        <Label color="green" isCompact>{t('{{count}} Ready', { count: counts.ready })}</Label>
      </FlexItem>
      <FlexItem>
        <Label color="red" isCompact>{t('{{count}} Not Ready', { count: counts.notReady })}</Label>
      </FlexItem>
      <FlexItem>
        <Label color="grey" isCompact>{t('{{count}} Unknown', { count: counts.unknown })}</Label>
      </FlexItem>
    </Flex>
  )
}

const OverviewPage: FC = () => {
  const { t } = useMeshTranslation()

  const [meshes, meshesLoaded, meshesError] = useMultiClusterMeshes()
  const {
    results: searchResults,
    loaded: cpLoaded,
    error: cpError,
    isFleetAvailable,
  } = useDiscoveredControlPlanes()
  const [enrichedPlanes, , , enrichmentError] = useEnrichedControlPlanes(searchResults, meshes ?? [])

  const meshStatusCounts = useMemo(
    () => countByStatus((meshes ?? []).map((m) => ({ conditions: m.status?.conditions }))),
    [meshes],
  )

  const clusterCount = useMemo(() => countUniqueClusters(meshes ?? []), [meshes])

  const cpStatusCounts = useMemo(
    () => countByStatus(enrichedPlanes.map((cp) => ({ conditions: cp.status?.conditions }))),
    [enrichedPlanes],
  )

  const recentIssues = useMemo(
    () => collectRecentIssues(meshes ?? [], enrichedPlanes),
    [meshes, enrichedPlanes],
  )

  const meshCount = meshes?.length ?? 0
  const cpCount = enrichedPlanes.length
  const cpSectionError = cpError ?? enrichmentError

  return (
    <>
      <PageSection>
        <Title headingLevel="h1">{t('Overview')}</Title>
      </PageSection>

      <PageSection>
        <Grid hasGutter>
          {/* Count cards */}
          <GridItem span={4}>
            <Card isCompact>
              <CardTitle><Tooltip content={t('Fleet Mesh')}><TopologyIcon style={{ marginRight: '0.5rem' }} /></Tooltip>{t('Fleet Meshes')}</CardTitle>
              <CardBody>
                {!meshesLoaded ? (
                  <Spinner size="lg" aria-label={t('Loading fleet meshes count')} />
                ) : meshesError ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load mesh data')} />
                ) : (
                  <Title headingLevel="h2" size="4xl">{meshCount}</Title>
                )}
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={4}>
            <Card isCompact>
              <CardTitle><Tooltip content={t('Control Plane')}><ServerIcon style={{ marginRight: '0.5rem' }} /></Tooltip>{t('Control Planes')}</CardTitle>
              <CardBody>
                {!cpLoaded ? (
                  <Spinner size="lg" aria-label={t('Loading control planes count')} />
                ) : !isFleetAvailable ? (
                  <DescriptionList>
                    <DescriptionListGroup>
                      <DescriptionListTerm>-</DescriptionListTerm>
                      <DescriptionListDescription>
                        <Label color="grey" isCompact>{t('Requires ACM')}</Label>
                      </DescriptionListDescription>
                    </DescriptionListGroup>
                  </DescriptionList>
                ) : cpSectionError ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load control plane data')} />
                ) : (
                  <Title headingLevel="h2" size="4xl">{cpCount}</Title>
                )}
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={4}>
            <Card isCompact>
              <CardTitle>{t('Managed Clusters')}</CardTitle>
              <CardBody>
                {!meshesLoaded ? (
                  <Spinner size="lg" aria-label={t('Loading managed clusters count')} />
                ) : meshesError ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load mesh data')} />
                ) : (
                  <Title headingLevel="h2" size="4xl">{clusterCount}</Title>
                )}
              </CardBody>
            </Card>
          </GridItem>

          {/* Health cards */}
          <GridItem span={6}>
            <Card isCompact>
              <CardTitle><Tooltip content={t('Fleet Mesh')}><TopologyIcon style={{ marginRight: '0.5rem' }} /></Tooltip>{t('Fleet Meshes Health')}</CardTitle>
              <CardBody>
                {!meshesLoaded ? (
                  <Spinner size="md" aria-label={t('Loading fleet meshes health')} />
                ) : meshesError ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load mesh data')} />
                ) : meshCount === 0 ? (
                  <EmptyState variant="xs">
                    <EmptyStateBody>{t('No meshes have been created yet.')}</EmptyStateBody>
                  </EmptyState>
                ) : (
                  <StatusCountLabels counts={meshStatusCounts} />
                )}
              </CardBody>
              <CardFooter>
                <Link to="/service-mesh">{t('View all fleet meshes')}</Link>
              </CardFooter>
            </Card>
          </GridItem>

          <GridItem span={6}>
            <Card isCompact>
              <CardTitle><Tooltip content={t('Control Plane')}><ServerIcon style={{ marginRight: '0.5rem' }} /></Tooltip>{t('Control Planes Health')}</CardTitle>
              <CardBody>
                {!cpLoaded ? (
                  <Spinner size="md" aria-label={t('Loading control planes health')} />
                ) : !isFleetAvailable ? (
                  <Label color="grey">{t('This page requires Red Hat Advanced Cluster Management.')}</Label>
                ) : cpSectionError ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load control plane data')} />
                ) : cpCount === 0 ? (
                  <EmptyState variant="xs">
                    <EmptyStateBody>{t('No control planes discovered across the fleet.')}</EmptyStateBody>
                  </EmptyState>
                ) : (
                  <StatusCountLabels counts={cpStatusCounts} />
                )}
              </CardBody>
              <CardFooter>
                <Link to="/mesh-control-planes">{t('View all control planes')}</Link>
              </CardFooter>
            </Card>
          </GridItem>

          {/* Recent issues */}
          <GridItem span={12}>
            <Card isCompact>
              <CardTitle>{t('Recent Issues')}</CardTitle>
              <CardBody>
                {!meshesLoaded || !cpLoaded ? (
                  <Spinner size="md" aria-label={t('Loading recent issues')} />
                ) : (meshesError && cpSectionError) ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load fleet data')} />
                ) : (
                  <>
                    {(meshesError || cpSectionError) && (
                      <Alert
                        variant="warning"
                        isInline
                        isPlain
                        title={meshesError
                          ? t('Unable to load mesh data. Some issues may not be shown.')
                          : t('Unable to load control plane data. Some issues may not be shown.')}
                        style={{ marginBottom: '1rem' }}
                      />
                    )}
                    {recentIssues.length === 0 && !meshesError && !cpSectionError ? (
                      <EmptyState variant="xs">
                        <EmptyStateBody>{t('No issues detected.')}</EmptyStateBody>
                      </EmptyState>
                    ) : recentIssues.length > 0 ? (
                      <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
                        <thead className="pf-v6-c-table__thead">
                          <tr className="pf-v6-c-table__tr">
                            <th className="pf-v6-c-table__th" scope="col">{t('Source')}</th>
                            <th className="pf-v6-c-table__th" scope="col">{t('Status')}</th>
                            <th className="pf-v6-c-table__th" scope="col">{t('Last Transition')}</th>
                          </tr>
                        </thead>
                        <tbody className="pf-v6-c-table__tbody">
                          {recentIssues.map((issue, i) => (
                            <tr className="pf-v6-c-table__tr" key={`${issue.kind}-${issue.source}-${issue.label}`}>
                              <td className="pf-v6-c-table__td">
                                <Tooltip content={issue.kind === 'mesh' ? t('Fleet Mesh') : t('Control Plane')}>
                                  {issue.kind === 'mesh'
                                    ? <TopologyIcon style={{ marginRight: '0.5rem' }} />
                                    : <ServerIcon style={{ marginRight: '0.5rem' }} />}
                                </Tooltip>
                                <Link to={issue.link}>{issue.source}</Link>
                              </td>
                              <td className="pf-v6-c-table__td">
                                <Label color={issue.color} isCompact>{t(issue.label)}</Label>
                              </td>
                              <td className="pf-v6-c-table__td">
                                {issue.lastTransitionTime ? <Timestamp timestamp={issue.lastTransitionTime} /> : '-'}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    ) : null}
                  </>
                )}
              </CardBody>
            </Card>
          </GridItem>
        </Grid>
      </PageSection>
    </>
  )
}

export default OverviewPage
