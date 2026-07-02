import { rs } from '@rstest/core'
import type { FC, ReactNode } from 'react'

export const Link: FC<{ to: string; state?: unknown; children?: ReactNode }> = ({ to, children }) => (
  <a href={to}>{children}</a>
)

export const useLocation = rs.fn(() => ({ pathname: '/', state: null, search: '', hash: '', key: 'default' }))

export const useParams = rs.fn(() => ({}))
