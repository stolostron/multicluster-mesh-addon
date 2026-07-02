import { rs } from '@rstest/core'
import type { FC, ReactNode } from 'react'

export const Link: FC<{ to: string; state?: unknown; children?: ReactNode }> = ({ to, children }) => (
  <a href={to}>{children}</a>
)

export const useParams = rs.fn(() => ({}))
