import { useEffect, useMemo, useState } from 'react'
import type { FC } from 'react'
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
import { MeshStatus, statusIcon } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

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
            <Link to="/fleet-mesh/control-planes">{t('Control Planes')}</Link>
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
          <FlexItem>
            {matchedMCM
              ? <Label color="blue">{t('Managed')}</Label>
              : meshID
                ? <Label color="purple">{t('Discovered')}</Label>
                : null}
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <Grid hasGutter>
          <GridItem span={5}>
            <Card isCompact>
              <CardBody>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Mesh ID')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {meshID
                        ? (matchedMCM
                            ? meshID
                            : <Link to={`/fleet-mesh/meshes/discovered/${encodeURIComponent(meshID)}`}>{meshID}</Link>)
                        : '-'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Network')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{network ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Cluster')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Link to={`/multicloud/infrastructure/clusters/details/${cluster}/${cluster}/overview`}>
                        {cluster}
                      </Link>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Control Plane Namespace')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{spec.namespace}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Version')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{spec.version ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Created')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={istio.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  {multiClusterName && (
                    <DescriptionListGroup>
                      <DescriptionListTerm><strong>{t('Cluster Name (Istio)')}</strong></DescriptionListTerm>
                      <DescriptionListDescription>{multiClusterName}</DescriptionListDescription>
                    </DescriptionListGroup>
                  )}
                  {matchedMCM && (
                    <DescriptionListGroup>
                      <DescriptionListTerm><strong>{t('Managed Mesh')}</strong></DescriptionListTerm>
                      <DescriptionListDescription>
                        <Link to={`/fleet-mesh/meshes/${matchedMCM.namespace}/${matchedMCM.name}`}>
                          {matchedMCM.name}
                        </Link>
                      </DescriptionListDescription>
                    </DescriptionListGroup>
                  )}
                </DescriptionList>
              </CardBody>
            </Card>
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
            {t('Invalid URL. Expected /fleet-mesh/control-planes/:cluster/:name.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <ControlPlaneDetailContent cluster={cluster} name={name} />
}

/** Detail page for a single Istio control plane, reached via /fleet-mesh/control-planes/:cluster/:name. */
export default ControlPlaneDetailPage
