import { useMemo } from 'react'
import type { FC } from 'react'
import { Link } from 'react-router-dom-v5-compat'
import { Timestamp } from '@openshift-console/dynamic-plugin-sdk'
import {
  Alert,
  Card,
  CardBody,
  CardTitle,
  EmptyState,
  EmptyStateBody,
  Grid,
  GridItem,
  Label,
  PageSection,
  Spinner,
  Title,
  Tooltip,
} from '@patternfly/react-core'
import { TopologyIcon, ServerIcon } from '@patternfly/react-icons'
import { useFleetMeshItems } from '../hooks/useFleetMeshItems'
import type { MultiClusterMesh, K8sCondition } from '../types/multiClusterMesh'
import type { EnrichedControlPlane } from '../types/istio'
import type { StatusColor } from './MeshStatus'
import { deriveStatus } from './MeshStatus'
import { StatusDonutChart } from './StatusDonutChart'
import type { StatusCounts } from './StatusDonutChart'
import { useMeshTranslation } from '../utils/i18nUtils'

function countByStatus(items: { conditions?: K8sCondition[] }[], conditionType?: string): StatusCounts {
  const counts = { degraded: 0, notReady: 0, ready: 0, unknown: 0 }
  for (const item of items) {
    const { color } = deriveStatus(item.conditions, conditionType)
    if (color === 'green') counts.ready++
    else if (color === 'orange') counts.degraded++
    else if (color === 'grey') counts.unknown++
    else counts.notReady++
  }
  return counts
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
    const meshLink = `/fleet-mesh/meshes/managed/${encodeURIComponent(meshNamespace)}/${encodeURIComponent(meshName)}`

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
        link: `/fleet-mesh/control-planes/${encodeURIComponent(cp.clusterName)}/${encodeURIComponent(cp.metadata.name)}`,
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

const OverviewPage: FC = () => {
  const { t } = useMeshTranslation()
  const {
    items,
    mcms,
    mcmsLoaded,
    mcmsError,
    enrichedPlanes,
    enrichmentLoaded,
    enrichmentError,
    searchLoaded,
    searchError,
    isFleetAvailable,
  } = useFleetMeshItems()

  // Two-phase Meshes: show MCM counts immediately, add discovered when ready
  const meshCount = enrichmentLoaded ? items.length : mcms.length
  const meshStatusCounts = useMemo(
    () => enrichmentLoaded
      ? countByStatus(items)
      : countByStatus(mcms.map((m) => ({ conditions: m.status?.conditions }))),
    [items, mcms, enrichmentLoaded],
  )

  const cpLoaded = searchLoaded
  const cpSectionError = searchError ?? enrichmentError
  const cpCount = enrichedPlanes.length

  const cpStatusCounts = useMemo(
    () => countByStatus(enrichedPlanes.map((cp) => ({ conditions: cp.status?.conditions }))),
    [enrichedPlanes],
  )

  const recentIssues = useMemo(
    () => collectRecentIssues(mcms, enrichedPlanes),
    [mcms, enrichedPlanes],
  )

  return (
    <>
      <PageSection>
        <Title headingLevel="h1">{t('Overview')}</Title>
      </PageSection>

      <PageSection>
        <Grid hasGutter>
          <GridItem span={5}>
            <Grid hasGutter>
              <GridItem span={12}>
                <Card>
                  <CardTitle style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span>
                      <TopologyIcon style={{ marginRight: '0.5rem' }} />
                      {t('Meshes')}
                    </span>
                    <Link to="/fleet-mesh/meshes" style={{ fontSize: 'var(--pf-v6-global--FontSize--sm)' }}>{t('View all')}</Link>
                  </CardTitle>
                  <CardBody style={{ overflow: 'hidden' }}>
                    {!mcmsLoaded ? (
                      <Spinner size="md" aria-label={t('Loading fleet meshes')} />
                    ) : mcmsError ? (
                      <Alert variant="danger" isInline isPlain title={t('Unable to load mesh data')} />
                    ) : meshCount === 0 ? (
                      <EmptyState variant="xs">
                        <EmptyStateBody>{t('No managed or discovered meshes found.')}</EmptyStateBody>
                      </EmptyState>
                    ) : (
                      <>
                        {!!cpSectionError && enrichmentLoaded && (
                          <Alert
                            variant="warning"
                            isInline
                            isPlain
                            title={t('Unable to load control plane data. Some meshes may not be shown.')}
                            style={{ marginBottom: '0.5rem' }}
                          />
                        )}
                        <StatusDonutChart counts={meshStatusCounts} subtitle={t('total')} />
                      </>
                    )}
                  </CardBody>
                </Card>
              </GridItem>

              <GridItem span={12}>
                <Card>
                  <CardTitle style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span>
                      <ServerIcon style={{ marginRight: '0.5rem' }} />
                      {t('Control Planes')}
                    </span>
                    <Link to="/fleet-mesh/control-planes" style={{ fontSize: 'var(--pf-v6-global--FontSize--sm)' }}>{t('View all')}</Link>
                  </CardTitle>
                  <CardBody style={{ overflow: 'hidden' }}>
                    {!cpLoaded ? (
                      <Spinner size="md" aria-label={t('Loading control planes')} />
                    ) : !isFleetAvailable ? (
                      <Label color="grey">{t('This page requires Red Hat Advanced Cluster Management.')}</Label>
                    ) : cpSectionError ? (
                      <Alert variant="danger" isInline isPlain title={t('Unable to load control plane data')} />
                    ) : cpCount === 0 ? (
                      <EmptyState variant="xs">
                        <EmptyStateBody>{t('No control planes discovered across the fleet.')}</EmptyStateBody>
                      </EmptyState>
                    ) : (
                      <StatusDonutChart counts={cpStatusCounts} subtitle={t('total')} />
                    )}
                  </CardBody>
                </Card>
              </GridItem>
            </Grid>
          </GridItem>

          <GridItem span={7}>
            <Card isCompact style={{ height: '100%' }}>
              <CardTitle>{t('Recent Issues')}</CardTitle>
              <CardBody>
                {!mcmsLoaded || !cpLoaded ? (
                  <Spinner size="md" aria-label={t('Loading recent issues')} />
                ) : (mcmsError && cpSectionError) ? (
                  <Alert variant="danger" isInline isPlain title={t('Unable to load fleet data')} />
                ) : (
                  <>
                    {(mcmsError || cpSectionError) && (
                      <Alert
                        variant="warning"
                        isInline
                        isPlain
                        title={mcmsError
                          ? t('Unable to load mesh data. Some issues may not be shown.')
                          : t('Unable to load control plane data. Some issues may not be shown.')}
                        style={{ marginBottom: '1rem' }}
                      />
                    )}
                    {recentIssues.length === 0 && !mcmsError && !cpSectionError ? (
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
                          {recentIssues.map((issue) => (
                            <tr className="pf-v6-c-table__tr" key={`${issue.kind}-${issue.source}-${issue.label}`}>
                              <td className="pf-v6-c-table__td">
                                <Tooltip content={issue.kind === 'mesh' ? t('Mesh') : t('Control Plane')}>
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
