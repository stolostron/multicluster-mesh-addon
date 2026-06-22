import { render, screen } from '@testing-library/react'
import { MeshStatus, getStatusRank } from '../MeshStatus'
import type { K8sCondition } from '../../types/multiClusterMesh'

// i18n is mocked in setupTests.ts: t(key) returns the key (pass-through)

const ready: K8sCondition = { type: 'Ready', status: 'True' }
const readyFalse: K8sCondition = { type: 'Ready', status: 'False' }
const readyUnknown: K8sCondition = { type: 'Ready', status: 'Unknown' }
const operatorInstalled: K8sCondition = { type: 'OperatorInstalled', status: 'True' }
const operatorFailed: K8sCondition = { type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError' }

describe('MeshStatus', () => {
  describe('with no conditions', () => {
    it('shows Unknown', () => {
      render(<MeshStatus />)
      expect(screen.getByText('Unknown')).toBeInTheDocument()
    })

    it('shows Unknown when conditions is empty', () => {
      render(<MeshStatus conditions={[]} />)
      expect(screen.getByText('Unknown')).toBeInTheDocument()
    })
  })

  describe('Ready condition', () => {
    it('shows Ready when Ready=True', () => {
      render(<MeshStatus conditions={[ready]} />)
      expect(screen.getByText('Ready')).toBeInTheDocument()
    })

    it('shows Not Ready when Ready=False with no reason', () => {
      render(<MeshStatus conditions={[readyFalse]} />)
      expect(screen.getByText('Not Ready')).toBeInTheDocument()
    })

    it('shows Unknown for Ready=Unknown', () => {
      render(<MeshStatus conditions={[readyUnknown]} />)
      expect(screen.getByText('Unknown')).toBeInTheDocument()
    })
  })

  describe('friendly reason codes', () => {
    const cases: [string, string][] = [
      ['OperatorConfigConflict', 'Operator Config Conflict'],
      ['NamespaceConflict', 'Namespace Conflict'],
      ['ClustersNotReady', 'Clusters Not Ready'],
      ['ManifestWorkCreated', 'Installing'],
      ['MissingProductClaim', 'Missing Product Claim'],
      ['ReconcileError', 'Reconcile Error'],
    ]

    test.each(cases)('maps reason %s to "%s"', (reason, label) => {
      const condition: K8sCondition = { type: 'Ready', status: 'False', reason }
      render(<MeshStatus conditions={[condition]} />)
      expect(screen.getByText(label)).toBeInTheDocument()
    })
  })

  it('passes unknown reason codes through unchanged', () => {
    const condition: K8sCondition = { type: 'Ready', status: 'False', reason: 'SomeUnknownReason' }
    render(<MeshStatus conditions={[condition]} />)
    expect(screen.getByText('SomeUnknownReason')).toBeInTheDocument()
  })

  describe('custom conditionType', () => {
    it('shows OperatorInstalled when OperatorInstalled=True', () => {
      render(<MeshStatus conditions={[operatorInstalled]} conditionType="OperatorInstalled" />)
      expect(screen.getByText('OperatorInstalled')).toBeInTheDocument()
    })

    it('shows friendly reason for OperatorInstalled=False', () => {
      render(<MeshStatus conditions={[operatorFailed]} conditionType="OperatorInstalled" />)
      expect(screen.getByText('Reconcile Error')).toBeInTheDocument()
    })
  })

  it('shows Healthy when all non-target conditions are True', () => {
    const conditions: K8sCondition[] = [
      { type: 'SomeOther', status: 'True' },
    ]
    render(<MeshStatus conditions={conditions} conditionType="Ready" />)
    expect(screen.getByText('Healthy')).toBeInTheDocument()
  })

  it('falls back to degraded condition reason when target type is absent', () => {
    const conditions: K8sCondition[] = [
      { type: 'Degraded', status: 'False', reason: 'NetworkIssue' },
    ]
    render(<MeshStatus conditions={conditions} conditionType="Ready" />)
    expect(screen.getByText('NetworkIssue')).toBeInTheDocument()
  })
})

describe('getStatusRank', () => {
  it('ranks green (True) as 0', () => {
    expect(getStatusRank([ready])).toBe(0)
  })

  it('ranks grey (no conditions) as 1', () => {
    expect(getStatusRank(undefined)).toBe(1)
    expect(getStatusRank([])).toBe(1)
  })

  it('ranks orange (degraded non-target) as 2', () => {
    const conditions: K8sCondition[] = [{ type: 'Other', status: 'False', reason: 'X' }]
    expect(getStatusRank(conditions, 'Ready')).toBe(2)
  })

  it('ranks red (False) as 3', () => {
    expect(getStatusRank([readyFalse])).toBe(3)
  })

  it('uses provided conditionType for ranking', () => {
    expect(getStatusRank([operatorInstalled], 'OperatorInstalled')).toBe(0)
    expect(getStatusRank([operatorFailed], 'OperatorInstalled')).toBe(3)
  })
})
