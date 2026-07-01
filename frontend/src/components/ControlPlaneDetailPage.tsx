import { useEffect, useMemo, useState } from 'react'
import type { FC, ReactNode } from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import {
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
  Spinner,
  Title,
} from '@patternfly/react-core'
import type { Istio } from '../types/istio'
import { istioModel } from '../types/istio'
import type { K8sCondition } from '../types/common'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { buildMcmIndex, lookupMcm } from '../utils/correlateMCM'
import { MeshStatus } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

function statusIcon(status: string): ReactNode {
  const color = status === 'True' ? 'green' : status === 'Unknown' ? 'grey' : 'red'
  return <Label color={color}>{status}</Label>
}

const ControlPlaneDetailContent: FC<{ cluster: string; name: string }> = ({ cluster, name }) => {
  const { t } = useMeshTranslation()
  const [istio, setIstio] = useState<Istio | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [mcms] = useMultiClusterMeshes()
  const mcmIndex = useMemo(() => buildMcmIndex(mcms ?? []), [mcms])

  useEffect(() => {
    let cancelled = false
    setLoaded(false)
    setError(null)
    setIstio(null)
    fleetK8sGet<Istio>({ model: istioModel, name, cluster })
      .then((r) => { if (!cancelled) { setIstio(r); setLoaded(true) } })
      .catch((e) => { if (!cancelled) { console.error('Failed to load control plane:', e); setError(e); setLoaded(true) } })
    return () => { cancelled = true }
  }, [cluster, name])

  if (!loaded) {
    return (
      <PageSection>
        <Spinner aria-label={t('Loading control plane')} />
      </PageSection>
    )
  }

  if (error) {
    const err = error as Record<string, any>
    const is404 = err?.response?.status === 404 || err?.statusCode === 404 || err?.code === 404
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">
            {is404 ? t('Control plane not found') : t('Error loading control plane')}
          </Title>
          <EmptyStateBody>
            {is404
              ? t('Istio "{{name}}" was not found on cluster "{{cluster}}".', { name, cluster })
              : t('An unexpected error occurred. Check the browser console for details.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  if (!istio) return null
  const spec = istio.spec
  const conditions = istio.status?.conditions ?? []
  const meshID = spec.values?.global?.meshID
  const network = spec.values?.global?.network
  const multiClusterName = spec.values?.global?.multiCluster?.clusterName
  const matchedMCM = lookupMcm(mcmIndex, cluster, spec.namespace)

  return (
    <>
      <PageSection>
        <Breadcrumb>
          <BreadcrumbItem>
            <Link to="/mesh-control-planes">{t('Control Planes')}</Link>
          </BreadcrumbItem>
          <BreadcrumbItem isActive>{`${cluster} / ${name}`}</BreadcrumbItem>
        </Breadcrumb>
        <Flex alignItems={{ default: 'alignItemsCenter' }} style={{ marginTop: '1rem' }}>
          <FlexItem>
            <Title headingLevel="h1">{name}</Title>
          </FlexItem>
          <FlexItem>
            {conditions.length > 0 ? (
              <MeshStatus conditions={conditions} conditionType="Ready" />
            ) : (
              <Label color="grey">{t('Unknown')}</Label>
            )}
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
                    <DescriptionListTerm>{t('Cluster')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      <Link to={`/multicloud/infrastructure/clusters/details/${cluster}/${cluster}/overview`}>
                        {cluster}
                      </Link>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Control Plane Namespace')}</DescriptionListTerm>
                    <DescriptionListDescription>{spec.namespace}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Version')}</DescriptionListTerm>
                    <DescriptionListDescription>{spec.version ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Mesh ID')}</DescriptionListTerm>
                    <DescriptionListDescription>{meshID ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Network')}</DescriptionListTerm>
                    <DescriptionListDescription>{network ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  {multiClusterName && (
                    <DescriptionListGroup>
                      <DescriptionListTerm>{t('Cluster Name (Istio)')}</DescriptionListTerm>
                      <DescriptionListDescription>{multiClusterName}</DescriptionListDescription>
                    </DescriptionListGroup>
                  )}
                  <DescriptionListGroup>
                    <DescriptionListTerm>{t('Created')}</DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={istio.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          {matchedMCM && (
            <GridItem span={6}>
              <Card isCompact>
                <CardTitle>{t('Managed By')}</CardTitle>
                <CardBody>
                  <DescriptionList isHorizontal isCompact>
                    <DescriptionListGroup>
                      <DescriptionListTerm>{t('Mesh')}</DescriptionListTerm>
                      <DescriptionListDescription>
                        <Link to={`/service-mesh/${matchedMCM.namespace}/${matchedMCM.name}`}>
                          {matchedMCM.name}
                        </Link>
                      </DescriptionListDescription>
                    </DescriptionListGroup>
                  </DescriptionList>
                </CardBody>
              </Card>
            </GridItem>
          )}

          {!matchedMCM && meshID && (
            <GridItem span={6}>
              <Card isCompact>
                <CardTitle>{t('Discovered Mesh')}</CardTitle>
                <CardBody>
                  <DescriptionList isHorizontal isCompact>
                    <DescriptionListGroup>
                      <DescriptionListTerm>{t('Mesh ID')}</DescriptionListTerm>
                      <DescriptionListDescription>
                        <Link to={`/fleet-mesh-discovered/${encodeURIComponent(meshID)}`}>
                          {meshID}
                        </Link>
                      </DescriptionListDescription>
                    </DescriptionListGroup>
                  </DescriptionList>
                </CardBody>
              </Card>
            </GridItem>
          )}

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
                      {conditions.map((c: K8sCondition, i: number) => (
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

const ControlPlaneDetailPage: FC = () => {
  const { t } = useMeshTranslation()
  const { cluster, name } = useParams<{ cluster: string; name: string }>()

  if (!cluster || !name) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Not Found')}</Title>
          <EmptyStateBody>
            {t('Invalid control plane URL. Expected /mesh-control-planes/:cluster/:name.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <ControlPlaneDetailContent cluster={cluster} name={name} />
}

/** Detail page for a single Istio control plane, reached via /mesh-control-planes/:cluster/:name. */
export default ControlPlaneDetailPage
