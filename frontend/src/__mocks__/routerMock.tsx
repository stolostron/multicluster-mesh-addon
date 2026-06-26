import { rs } from '@rstest/core'
import type { FC, ReactNode } from 'react'

export const Link: FC<{ to: string; children?: ReactNode }> = ({ to, children }) => (
  <a href={to}>{children}</a>
)

export const useParams = rs.fn(() => ({}))
