import type { FC } from 'react'
import { Timestamp } from '@openshift-console/dynamic-plugin-sdk'
import type { K8sCondition } from '../types/common'
import { statusIcon } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

interface ConditionsTableProps {
  conditions: K8sCondition[]
}

export const ConditionsTable: FC<ConditionsTableProps> = ({ conditions }) => {
  const { t } = useMeshTranslation()
  return (
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
  )
}
