import { useMemo, useState } from 'react'
import type { FC } from 'react'
import { Link } from 'react-router-dom-v5-compat'
import {
  Card,
  CardBody,
  CardTitle,
  Flex,
  FlexItem,
  Label,
  SearchInput,
  ToggleGroup,
  ToggleGroupItem,
} from '@patternfly/react-core'
import type { EnrichedControlPlane, CpStatusCategory } from '../types/istio'
import { categorizeCp } from '../types/istio'
import { MeshStatus } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

const ControlPlanesCard: FC<{ planes: EnrichedControlPlane[] }> = ({ planes }) => {
  const { t } = useMeshTranslation()
  const [filter, setFilter] = useState<CpStatusCategory>('all')
  const [search, setSearch] = useState('')

  const categoryMap = useMemo(() => {
    const map = new Map<string, CpStatusCategory>()
    planes.forEach((cp) => map.set(`${cp.clusterName}/${cp.metadata.name}`, categorizeCp(cp)))
    return map
  }, [planes])

  const counts = useMemo(() => {
    const result = { ready: 0, notReady: 0, unknown: 0 }
    categoryMap.forEach((cat) => { if (cat !== 'all') result[cat]++ })
    return result
  }, [categoryMap])

  const filtered = useMemo(() => {
    return planes.filter((cp) => {
      if (filter !== 'all' && categoryMap.get(`${cp.clusterName}/${cp.metadata.name}`) !== filter) return false
      if (search) {
        const q = search.toLowerCase()
        if (!cp.clusterName.toLowerCase().includes(q) && !cp.metadata.name.toLowerCase().includes(q)) return false
      }
      return true
    })
  }, [planes, categoryMap, filter, search])

  if (planes.length === 0) return null

  return (
    <Card isCompact>
      <CardTitle><strong>{t('Control Planes ({{count}})', { count: planes.length })}</strong></CardTitle>
      <CardBody>
        <Flex style={{ marginBottom: '1rem' }}>
          <FlexItem>
            <ToggleGroup>
              <ToggleGroupItem
                text={t('All ({{count}})', { count: planes.length })}
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
                <th className="pf-v6-c-table__th" scope="col">{t('Name')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Namespace')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Version')}</th>
                <th className="pf-v6-c-table__th" scope="col">{t('Status')}</th>
              </tr>
            </thead>
            <tbody className="pf-v6-c-table__tbody">
              {filtered.length === 0 ? (
                <tr className="pf-v6-c-table__tr">
                  <td className="pf-v6-c-table__td" colSpan={5} style={{ textAlign: 'center' }}>
                    {t('No control planes match the current filter.')}
                  </td>
                </tr>
              ) : (
                filtered.map((cp) => (
                  <tr className="pf-v6-c-table__tr" key={`${cp.clusterName}/${cp.metadata.name}`}>
                    <td className="pf-v6-c-table__td">
                      <Link to={`/multicloud/infrastructure/clusters/details/${cp.clusterName}/${cp.clusterName}/overview`}>
                        {cp.clusterName}
                      </Link>
                    </td>
                    <td className="pf-v6-c-table__td">
                      <Link to={`/fleet-mesh/control-planes/${encodeURIComponent(cp.clusterName)}/${encodeURIComponent(cp.metadata.name)}`}>
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
                ))
              )}
            </tbody>
          </table>
        </div>
      </CardBody>
    </Card>
  )
}

export { ControlPlanesCard }
