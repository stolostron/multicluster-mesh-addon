import { useEffect, useMemo, useState } from 'react'
import type { FC, ReactNode } from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import {
  useK8sWatchResource,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Breadcrumb,
  BreadcrumbItem,
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
} from '@patternfly/react-core'
import type { MultiClusterMesh, K8sCondition, ClusterMeshStatus } from '../types/multiClusterMesh'
import { multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'
import { MeshStatus } from './MeshStatus'
import { TrustStatusCard } from './TrustStatusCard'
import { useMeshTranslation } from '../utils/i18nUtils'

function conditionMessage(condition: K8sCondition): string {
  if (condition.message) return condition.message
  if (condition.reason) return condition.reason
  return condition.status
}

function statusIcon(status: string): ReactNode {
  const color = status === 'True' ? 'green' : status === 'Unknown' ? 'grey' : 'red'
  return <Label color={color}>{status}</Label>
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
export const ClusterStatusSection: FC<{ clusterStatuses: ClusterMeshStatus[]; meshConditions?: K8sCondition[] }> = ({
  clusterStatuses,
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
      <Card>
        <CardTitle>{t('Cluster Status (0)')}</CardTitle>
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
    <Card>
      <CardTitle>{t('Cluster Status ({{count}})', { count: clusterStatuses.length })}</CardTitle>
      <CardBody>
        <Flex style={{ marginBottom: '1rem' }} spaceItems={{ default: 'spaceItemsMd' }}>
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
                    <th className="pf-v6-c-table__th" scope="col">{t('Operator Status')}</th>
                    <th className="pf-v6-c-table__th" scope="col">{t('Message')}</th>
                  </tr>
                </thead>
                <tbody className="pf-v6-c-table__tbody">
                  {filtered.length === 0 ? (
                    <tr className="pf-v6-c-table__tr">
                      <td className="pf-v6-c-table__td" colSpan={3} style={{ textAlign: 'center' }}>
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
                            <MeshStatus conditions={cs.conditions} conditionType="OperatorInstalled" />
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
            <Link to="/service-mesh">{t('Fleet Meshes')}</Link>
          </BreadcrumbItem>
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
          <GridItem span={6}>
            <Card isCompact>
              <CardTitle>{t('Overview')}</CardTitle>
              <CardBody>
                <DescriptionList isHorizontal isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Cluster Set')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      <Link to={`/multicloud/infrastructure/clusters/sets/details/${spec.clusterSet}/overview`}>
                        {spec.clusterSet}
                      </Link>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Control Plane Namespace')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.controlPlane?.namespace || 'istio-system'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('cert-manager Issuer')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      {issuerName
                        ? `${issuerName} (${issuerRef?.kind || 'Issuer'})`
                        : t('Not configured')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Created')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={mesh.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={6}>
            <Card isCompact>
              <CardTitle>{t('OSSM Operator')}</CardTitle>
              <CardBody>
                <DescriptionList isHorizontal isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Namespace')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.namespace || t('(platform default)')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Channel')}</DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.channel || 'stable'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Source')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.source || t('(platform default)')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Install Plan Approval')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.installPlanApproval || 'Automatic'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={12}>
            <TrustStatusCard
              clusterStatuses={clusterStatuses}
              issuerName={issuerName ?? ''}
              meshName={mesh.metadata?.name ?? ''}
              meshNamespace={ns}
            />
          </GridItem>

          <GridItem span={12}>
            <ClusterStatusSection clusterStatuses={clusterStatuses} meshConditions={conditions} />
          </GridItem>

          {conditions.length > 0 && (
            <GridItem span={12}>
              <Card>
                <CardTitle>{t('Conditions')}</CardTitle>
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
            {t('Invalid mesh URL. Expected /service-mesh/:namespace/:name.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <MeshDetailContent ns={ns} name={name} />
}

/** Detail page for a single MultiClusterMesh, reached via /service-mesh/:ns/:name. */
export default MeshDetailPage
