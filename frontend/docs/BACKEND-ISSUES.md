# Backend Issues — Frontend Impact Analysis

<!-- ======================================================================
HOW TO REGENERATE THIS FILE

This file tracks open issues on the backend controller repository that may
affect the frontend console plugin. It should be regenerated periodically
(e.g. when new backend issues are filed, or before a frontend sprint planning
session) to keep the frontend team aware of backend changes that could break,
degrade, or require updates to the UI.

To regenerate, give an AI agent the following prompt:

---BEGIN PROMPT---
You are analyzing the frontend console plugin in the `frontend/` directory of
the stolostron/multicluster-mesh-addon repository. Your job is to determine
how open backend issues affect the frontend.

1. Read ALL source files under `frontend/src/` — every component, hook, type
   definition, and utility. Pay special attention to:
   - TypeScript types in `src/types/` (the CRD shape the frontend expects)
   - Data fetching hooks in `src/hooks/` (what resources are watched and how)
   - Status display logic in `MeshStatus.tsx` (condition reason mapping)
   - `TrustStatusCard.tsx` (how trust status is derived from Certificates and
     ManifestWorks, since the backend may not report trust status per-cluster)
   - `MeshDetailPage.tsx` (per-cluster status categorization, what conditions
     are checked, how the overview card displays spec fields)
   - `OverviewPage.tsx` (health counts, recent issues collection)
   - `ServiceMeshPage.tsx` (list columns, what fields are displayed)
   - `ControlPlanesPage.tsx` and `ControlPlaneDetailPage.tsx` (enrichment,
     MCM correlation)
   - `utils/correlateMCM.ts` (how control planes are matched to meshes)

2. Also read `frontend/docs/ROADMAP.md` and `frontend/docs/PERFORMANCE.md`
   for context on known limitations, workarounds, and planned features.

3. Fetch ALL open issues from the backend repository:
   https://github.com/stolostron/multicluster-mesh-addon/issues?q=is%3Aissue+state%3Aopen
   Read the full body of every issue.

4. For EACH open issue, analyze deeply:
   a. Does the frontend have a workaround for this issue that will need
      updating when the issue is fixed? (e.g., the TrustStatusCard watches
      Certificates and ManifestWorks directly because the backend doesn't
      report per-cluster trust status)
   b. Does this issue cause the frontend to display misleading information?
      (e.g., showing "Pending" when the real state is "Failed")
   c. Will fixing this issue require frontend TypeScript type changes?
   d. Will fixing this issue add new condition types, reasons, or status
      fields that the frontend should display?
   e. Does this issue affect planned frontend features (see ROADMAP.md)?
   f. Is the frontend actually broken due to this issue without us realizing?

5. Classify each issue's frontend impact as:
   - HIGH: Frontend has a workaround that must be updated, or the UI is
     actively misleading users
   - MEDIUM: Frontend will need changes when fixed, or displays subtly
     incorrect information
   - LOW: Minor impact, frontend handles it gracefully or needs trivial updates
   - NONE: No frontend impact (test infra, CI, controller internals, docs)

6. Write the report in the exact format of the existing file at
   `frontend/docs/BACKEND-ISSUES.md`, preserving the structure:
   - Summary table (sorted by severity descending)
   - Detailed analysis per issue

   For each issue with HIGH, MEDIUM, or LOW impact, use these five
   sub-sections (as bold labels, not markdown headers):

   - **Impact:** Severity rating and one-line summary.
   - **Backend issue:** Brief description of what the backend issue is.
   - **Frontend today:** How the frontend currently handles this area —
     what code is involved, any workarounds in place. Include code
     snippets where relevant.
   - **Frontend risk:** Is the frontend broken, misleading, or showing
     incorrect data because of this issue? Answer explicitly
     (yes / no / partially) with explanation.
   - **When backend is fixed:** What happens to the frontend when the
     backend resolves this issue — does anything need changing, and if
     so, what specifically?

   For NONE-impact issues, write a single paragraph (no sub-sections).

7. For issues that are new, add them. For issues that were already
   analyzed, update the analysis if the frontend code has changed since
   the last analysis. For issues that were in the previous version of
   this file but are now closed: check whether the frontend code has
   already been updated to address the fixes identified in the analysis.
   If all frontend work for that closed issue is done (or the closed
   issue had no frontend impact), remove the closed issue from this file.
   If the issue is closed but the frontend has NOT yet been updated,
   keep the issue in the file — a closed backend issue often means the
   frontend NOW needs to implement the changes identified in the analysis.

Keep the regeneration prompt (this block) and the metadata section intact.
Update the "Last generated" date.
---END PROMPT---

====================================================================== -->

**Purpose:** Track backend controller issues that affect the frontend UI — workarounds that will need updating, misleading displays, missing status fields, and type changes. This prevents the frontend from going stale as the backend evolves.

**Source:** [stolostron/multicluster-mesh-addon open issues](https://github.com/stolostron/multicluster-mesh-addon/issues?q=is%3Aissue+state%3Aopen)

**Last generated:** 2026-06-24

---

## Summary

| Issue | Title | Frontend Impact | Severity |
|-------|-------|-----------------|----------|
| [#118](#118--report-trust-establishment-status-per-cluster) | Report trust establishment status per cluster | **Workaround exists; will need update** | HIGH |
| [#98](#98--handle-manifestwork-cache-lag-in-determinestatus) | Handle ManifestWork cache lag in determineStatus | **Users see transient false-alarm errors** | HIGH |
| [#112](#112--clarify-cert-manager-requirement-and-discuss-byo-ca-support) | Clarify cert-manager requirement / BYO CA | **TrustStatusCard tightly coupled to cert-manager** | MEDIUM |
| [#101](#101--decide-mutability-of-specsecuritytrustcertmanagerissurerref) | Decide mutability of issuerRef | **Silent inconsistency between overview and trust card** | MEDIUM |
| [#72](#72--ensure-unique-manifestwork-naming-to-prevent-multi-tenant-collisions) | Unique ManifestWork naming for multi-tenant | **TrustStatusCard shows "Pending" instead of "Failed"** | MEDIUM |
| [#124](#124--clean-up-olm-csv-when-deleting-operator-manifestwork-on-ocp) | Clean up OLM CSV on OCP | Minor — error displays correctly | LOW |
| [#90](#90--detect-pre-existing-operator-subscription-before-creating-manifestwork) | Detect pre-existing operator Subscription | Minor — future "adopted" vs "installed" distinction | LOW |
| [#66](#66--make-istio-ca-certificate-duration-and-renewal-time-configurable) | Configurable cert duration/renewal | TypeScript types need updating when fields added | LOW |
| [#109](#109--workapplier-dual-cache-race-causes-missed-mw-drift-detection) | WorkApplier dual-cache race | Minimal — frontend reads API, not controller cache | LOW |
| [#153](#153--document-trust-root-isolation-in-multi-tenant-deployments) | Document trust root isolation | None — documentation only | NONE |
| [#120](#120--update-existing-managedserviceaccount-resources-when-the-user-changes-the-tokenvalidity-on-the-mesh) | Update ManagedServiceAccount tokenValidity | None — feature not yet exposed in UI | NONE |
| [#133](#133--implement-e2e-tests-for-managedserviceaccount-testing-and-endpoint-discovery-checking) | e2e tests for ManagedServiceAccount | None — backend test infra | NONE |
| [#169](#169--improve-logging-in-integration-tests) | Improve logging in integration tests | None — backend test infra | NONE |
| [#79](#79--implement-cache-level-filtering-for-managed-resources-to-reduce-memory-footprint) | Cache-level filtering for managed resources | None — controller internals | NONE |
| [#78](#78--fix-sonar-prow-job) | Fix sonar prow job | None — CI infra | NONE |
| [#69](#69--investigate-and-implement-server-side-apply) | Investigate server side apply | None — controller internals | NONE |

---

## Detailed Analysis

### #118 — Report trust establishment status per cluster

**Impact:** HIGH — Workaround exists; will need update when fixed.

**Backend issue:** The controller currently doesn't report a `TrustEstablished` condition in `status.clusterStatus[].conditions[]`. Only `OperatorInstalled` is reported per-cluster. The issue proposes adding `TrustEstablished` and making the mesh `Ready` condition require both.

**Frontend today:** The `TrustStatusCard` component (`TrustStatusCard.tsx`) works around this by building its own per-cluster trust status. It directly watches two resource types from the hub API:

1. **cert-manager Certificates** — watched with label selector `mesh.open-cluster-management.io/mesh-name: <meshName>`, keyed by `mesh.open-cluster-management.io/cluster-name` label.
2. **ManifestWorks** (cacerts) — watched with label selectors `mesh-name` + `mesh-namespace`, keyed by `cluster-name` label.

The card derives trust categories (ready / pending / failed) from the cert `Ready` condition and the ManifestWork `Applied`/`Available` conditions:

```typescript
// TrustStatusCard.tsx — categorizeTrust()
function categorizeTrust(cert: Certificate | undefined, mw: ManifestWork | undefined): TrustCategory {
  if (!cert) return 'pending'
  const ready = findCondition(cert.status?.conditions, 'Ready')
  if (!ready || ready.status !== 'True') return 'failed'
  if (!mw) return 'pending'
  const applied = findCondition(mw.status?.conditions, 'Applied')
  const available = findCondition(mw.status?.conditions, 'Available')
  if (applied?.status === 'True' && available?.status === 'True') return 'ready'
  if (applied?.status === 'True') return 'pending'
  return 'failed'
}
```

Separately, the `ClusterStatusSection` in `MeshDetailPage.tsx` categorizes clusters using only the `OperatorInstalled` condition:

```typescript
// MeshDetailPage.tsx — only checks OperatorInstalled
function categorizeCluster(cs: ClusterMeshStatus): ClusterStatusCategory {
  const op = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
  if (!op) return 'unknown'
  if (op.status === 'True') return 'ready'
  // ...
}
```

**Frontend risk:** Partially. The TrustStatusCard workaround is functional today but derives status independently from the backend. When `TrustEstablished` is added, `categorizeCluster` will still only check `OperatorInstalled`, so a cluster could show green (operator installed) while the mesh itself shows red (trust not established) — a confusing disconnect.

**When backend is fixed:**

1. Update `categorizeCluster()` in `MeshDetailPage.tsx` to consider all per-cluster conditions, not just `OperatorInstalled`. Update the cluster status table header "Operator Status" accordingly.
2. Refactor `TrustStatusCard` to use the new per-cluster `TrustEstablished` condition as the primary status source. Keep the direct cert/MW watches only for showing cert expiry/renewal timestamps, which the condition alone won't carry.
3. Overview page's `countByStatus` counts mesh-level conditions, which would correctly reflect the new `Ready` semantics. No change needed there.

---

### #98 — Handle ManifestWork cache lag in determineStatus

**Impact:** HIGH — Users see transient false-alarm errors.

**Backend issue:** When the controller creates a ManifestWork, it immediately tries to read it from the informer cache for `determineStatus`. The cache hasn't synced yet, producing a `ReconcileError` condition that resolves on the next reconcile (seconds later).

**Frontend today:** The `MeshStatus` component (`MeshStatus.tsx`) maps `ReconcileError` to a friendly label and displays it as a red status:

```typescript
const friendlyReasons: Record<string, string> = {
  // ...
  ReconcileError: 'Reconcile Error',
}
```

When this bug fires, the mesh briefly shows a red "Reconcile Error" on the list page, detail page conditions table, and overview health counts — then flips to green "Ready" seconds later. The overview's "Recent Issues" panel also surfaces the `ReconcileError` during the transient window (it collects non-True conditions sorted by `lastTransitionTime`).

**Frontend risk:** No — the frontend correctly displays whatever the backend reports. But the user experience is degraded: every mesh creation or reconciliation triggers a brief red error flash that could alarm users. There is no frontend workaround to suppress this.

**When backend is fixed:** Users will see fewer transient errors. No frontend code changes required — the improvement is automatic.

---

### #112 — Clarify cert-manager requirement and discuss BYO CA support

**Impact:** MEDIUM — TrustStatusCard tightly coupled to cert-manager.

**Backend issue:** Two parts: (1) Make `issuerRef.name` required in the CRD (short-term), (2) Potentially support BYO CA without cert-manager (future).

**Frontend today:** The frontend already treats `issuerRef.name` as non-optional in the TypeScript type (`multiClusterMesh.ts`), but multiple components handle the "no issuer" case: `TrustStatusCard` checks `hasIssuer = !!issuerName` and renders a "Trust is not configured" empty state; `MeshDetailPage` shows "Not configured" for the cert-manager Issuer field; `ServiceMeshPage` shows a grey "Not configured" label in the Trust column. If trust becomes mandatory, these code paths become dead code (minor cleanup, no functional breakage).

The `TrustStatusCard` is tightly coupled to cert-manager — it watches `cert-manager.io/v1 Certificate` resources by GVK, derives cert status from the Certificate `Ready` condition, and shows cert expiry (`notAfter`) and renewal (`renewalTime`) timestamps from `CertificateStatus`.

**Frontend risk:** No, for the short-term change (making issuerRef required). Yes, for BYO CA — if BYO CA is implemented without cert-manager, the TrustStatusCard would show "No certificates have been created yet — the controller may still be reconciling" indefinitely, which is wrong.

**When backend is fixed:**

- If issuerRef becomes required: minor cleanup to remove dead "not configured" code paths.
- If BYO CA is implemented:
  1. `TrustStatusCard` needs a second code path for non-cert-manager trust, possibly driven by a new spec field (e.g., `spec.security.trust.type: "certManager" | "byoCA"`).
  2. The "Trust" column on `ServiceMeshPage` currently checks `issuerRef.name` to show "Configured" — for BYO CA, a different check is needed.
  3. `MeshDetailPage` overview section shows "cert-manager Issuer" — would need to show the appropriate trust mechanism.

---

### #101 — Decide mutability of spec.security.trust.certManager.issuerRef

**Impact:** MEDIUM — Silent inconsistency between overview and trust card.

**Backend issue:** If a user changes `issuerRef.name` after mesh creation, the controller doesn't update existing Certificate resources (they still point to the old issuer).

**Frontend today:** The `MeshDetailPage` overview card shows the current spec value:

```typescript
// MeshDetailPage.tsx
{issuerName
  ? `${issuerName} (${issuerRef?.kind || 'Issuer'})`
  : t('Not configured')}
```

Meanwhile, the `TrustStatusCard` shows the actual Certificate state — certificates issued by the *old* issuer, with their existing `Ready` condition and expiry/renewal times. The TrustStatusCard could cross-check the cert's issuer against the mesh spec's issuerRef and flag mismatches, but this requires knowing the Certificate's issuerRef (which isn't currently in the frontend's `Certificate` type — only status fields are typed).

**Frontend risk:** Yes — a user who changes `issuerRef.name` from `"issuer-a"` to `"issuer-b"` would see the new issuer in the mesh overview but old issuer's certs still showing `Ready` (green) in the Trust Status card. The user thinks trust is correctly configured with `issuer-b`, but it's actually still using `issuer-a`'s certificates. This is a silent inconsistency — nothing in the UI signals the mismatch.

**When backend is fixed:**

- **If immutable:** The planned "Edit mesh" feature must disable the field. No current frontend code change needed (read-only view is fine).
- **If the controller updates certs:** The inconsistency resolves naturally. No frontend change needed.

---

### #72 — Ensure unique ManifestWork naming to prevent multi-tenant collisions

**Impact:** MEDIUM — TrustStatusCard shows "Pending" instead of "Failed" for affected meshes.

**Backend issue:** The controller uses a hardcoded ManifestWork name (`multicluster-mesh-cacerts`) for cacerts. When two meshes in different namespaces target the same cluster, the second mesh's MW creation fails due to name collision.

**Frontend today:** The `TrustStatusCard` fetches ManifestWorks using label selectors (not by name):

```typescript
// TrustStatusCard.tsx
selector: {
  matchLabels: {
    [MESH_NAME_LABEL]: meshName,
    [MESH_NAMESPACE_LABEL]: meshNamespace,
  },
},
```

When Mesh B's MW creation fails, Mesh B's TrustStatusCard finds zero matching ManifestWorks. The `categorizeTrust` function returns `'pending'` when `mw` is undefined:

```typescript
if (!mw) return 'pending'
```

**Frontend risk:** Partially — for Mesh B, every cluster shows trust status as grey "Pending," implying the controller is still working. In reality, the MW creation permanently failed. The TrustStatusCard can't distinguish between "MW hasn't been created yet" and "MW creation failed due to naming collision."

**When backend is fixed:** The naming collision goes away, so each mesh gets its own uniquely-named MW and the label selector works correctly. No frontend code change needed, assuming the labels on the new MWs remain consistent.

---

### #124 — Clean up OLM CSV when deleting operator ManifestWork on OCP

**Impact:** LOW — Error displays correctly, "(platform default)" label is appropriately vague.

**Backend issue:** On OCP, deleting a mesh leaves the CSV running. Creating a new mesh for the same cluster causes a Subscription resolution failure.

**Frontend today:** The per-cluster `OperatorInstalled` condition shows the failure reason via `MeshStatus`. The `MeshDetailPage` displays the operator namespace with a vague fallback:

```typescript
{spec.operator?.namespace || t('(platform default)')}
```

The fallback `"(platform default)"` intentionally doesn't name `openshift-operators`, so if the backend changes the default namespace, the frontend text remains correct.

**Frontend risk:** No — the frontend correctly displays whatever condition the backend sets. If the Subscription fails, the cluster shows red with the error reason.

**When backend is fixed:** No frontend changes needed.

---

### #90 — Detect pre-existing operator Subscription before creating ManifestWork

**Impact:** LOW — Future "adopted" vs "installed" distinction may be desirable.

**Backend issue:** The controller unconditionally creates a ManifestWork for the operator, potentially hijacking a pre-existing Subscription. Deleting the mesh would then remove that pre-existing Subscription, breaking whatever depended on it.

**Frontend today:** The frontend only shows `OperatorInstalled` status and doesn't distinguish how the operator got installed. If the controller adopts a pre-existing Subscription, the cluster correctly shows `OperatorInstalled=True`.

**Frontend risk:** No — the current display is accurate. However, if the controller adds a conflict condition (e.g., `OperatorInstalled=False reason=SubscriptionConflict`), the `friendlyReasons` map in `MeshStatus.tsx` doesn't include it, so users would see the raw reason string instead of a user-friendly label.

**When backend is fixed:**

1. Add `SubscriptionConflict` (or whatever reason the backend uses) to the `friendlyReasons` map in `MeshStatus.tsx`.
2. If adoption is implemented, consider visual indicators to distinguish "adopted" operators (shown but not managed) from "installed" operators in `ClusterStatusSection`.

---

### #66 — Make Istio CA certificate duration and renewal time configurable

**Impact:** LOW — TypeScript types and detail page need updating when CRD fields are added.

**Backend issue:** Cert duration and renewal times are currently hardcoded in the controller. The proposal is to make them configurable in the CRD spec.

**Frontend today:** The `TrustStatusCard` already displays cert expiry (`notAfter`) and renewal (`renewalTime`) timestamps from Certificate status. These reflect whatever the controller configured, so the actual values are visible. However, the *configured* duration/renewal from the mesh spec is not shown because the fields don't exist yet.

**Frontend risk:** No — the current display is accurate. The user just can't see the configured values because they aren't in the CRD yet.

**When backend is fixed:**

1. Add new fields to `MultiClusterMeshSpec` TypeScript type (e.g., `spec.security.trust.certManager.duration`, `spec.security.trust.certManager.renewBefore`).
2. Display configured cert duration/renewal in `MeshDetailPage` overview section alongside the actual expiry/renewal shown in TrustStatusCard.

---

### #109 — WorkApplier dual-cache race causes missed MW drift detection

**Impact:** LOW — Frontend reads from API, not controller's cache.

**Backend issue:** Two separate informer caches can race, causing the controller to miss ManifestWork drift (external modifications to the MW spec).

**Frontend today:** The frontend watches ManifestWorks via `useK8sWatchResource`, which reads from the Kubernetes API server — completely independent of the controller's caches. If a MW has drifted, the frontend shows the actual current state (the drifted version), which is accurate.

**Frontend risk:** Partially — if the drift means cacerts were removed from the MW spec, the MW conditions might still show `Applied=True` (stale from before the drift). The `TrustStatusCard` would show "Distributed" (green) even though the certs are actually gone. But this is the same for any observer of MW conditions — the conditions reflect the last-reconciled state.

**When backend is fixed:** No frontend changes needed.

---

### #153 — Document trust root isolation in multi-tenant deployments

**Impact: NONE — Documentation only**

Backend documentation issue about what happens when multiple meshes share a trust root (same Issuer or ClusterIssuer). No API changes, no status changes, no frontend impact.

If the backend ever adds a warning condition for shared trust roots, the frontend would display it through the existing conditions table.

---

### #120 — Update existing ManagedServiceAccount resources when tokenValidity changes

**Impact: NONE — Feature not yet exposed in UI**

The frontend TypeScript type includes `spec.security.discovery.tokenValidity` but no component displays or uses it. The `ROADMAP.md` notes that endpoint discovery status is "Blocked on backend." No frontend impact.

---

### #133 — Implement e2e tests for ManagedServiceAccount testing and endpoint discovery checking

**Impact: NONE — Backend test infrastructure**

---

### #169 — Improve logging in integration tests

**Impact: NONE — Backend test infrastructure**

---

### #79 — Implement cache-level filtering for managed resources to reduce memory footprint

**Impact: NONE — Controller performance internals**

The frontend reads from the Kubernetes API server, not the controller's informer cache. Changes to the controller's cache configuration don't affect the frontend.

---

### #78 — Fix sonar prow job

**Impact: NONE — CI infrastructure**

---

### #69 — Investigate and implement server side apply

**Impact: NONE — Controller implementation pattern**

SSA vs. Get-Mutate-Update is an internal controller concern. The resulting K8s resources are the same from the frontend's perspective.

