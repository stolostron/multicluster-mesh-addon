# Track Backend Issues for Frontend Impact

Analyze open backend controller issues in the
[stolostron/multicluster-mesh-addon](https://github.com/stolostron/multicluster-mesh-addon)
repository and create or update GitHub tracking issues with the `frontend`
label for any that affect the frontend console plugin.

## When to use

Run this skill periodically — when new backend issues are filed, before
sprint planning, or when the user asks to check for backend changes that
might affect the frontend. The skill is idempotent: running it multiple
times will not create duplicate issues.

## Prerequisites

- `gh` CLI authenticated with access to the repo.
- All commands run from the `multicluster-mesh-addon` repository root.
- **Labeling requires `triage` or higher permission.** If you only have
  `pull` access, `gh issue create --label` silently fails to apply
  labels. The skill handles this by including a "Labels" line in the
  issue body as a fallback so a maintainer can apply them later.

## Instructions

### 1. Read the frontend source code

Read ALL source files under `frontend/src/` — every component, hook, type
definition, and utility. Pay special attention to:

- `src/types/` — the CRD shape the frontend expects
- `src/hooks/` — what resources are watched and how
- `MeshStatus.tsx` — condition reason mapping (`friendlyReasons`)
- `TrustStatusCard.tsx` — how trust status is derived from Certificates
  and ManifestWorks (workaround for missing per-cluster trust status)
- `MeshDetailPage.tsx` — per-cluster status categorization, what
  conditions are checked, how the overview card displays spec fields
- `OverviewPage.tsx` — health counts, recent issues collection
- `ServiceMeshPage.tsx` — list columns, what fields are displayed
- `ControlPlanesPage.tsx` and `ControlPlaneDetailPage.tsx` — enrichment,
  MCM correlation
- `utils/correlateMCM.ts` — how control planes are matched to meshes

Also read `frontend/docs/ROADMAP.md` and `frontend/docs/PERFORMANCE.md`
for context on known limitations, workarounds, and planned features.

### 2. Fetch all issues from the repo

Run:

```
gh issue list --state open --limit 200 --json number,title,body,labels
```

Split the results into two sets:

- **Existing frontend tracking issues:** issues that have the `frontend`
  label OR whose title starts with `[frontend]`. Either signal
  identifies a tracking issue (the title prefix is the fallback when
  labels can't be applied due to permissions).
- **Backend issues:** all remaining issues. These are candidates for
  analysis.

Also fetch closed frontend tracking issues — these may need updating if
their corresponding backend issue was recently closed:

```
gh issue list --state closed --label frontend --limit 200 --json number,title,body,labels
```

Apply the same filter: `frontend` label OR title starts with
`[frontend]`.

### 3. Analyze each backend issue for frontend impact

For each backend issue (not identified as a frontend tracking issue in
step 2), determine:

a. Does the frontend have a workaround for this issue that will need
   updating when the issue is fixed?
b. Does this issue cause the frontend to display misleading information?
c. Will fixing this issue require frontend TypeScript type changes?
d. Will fixing this issue add new condition types, reasons, or status
   fields that the frontend should display?
e. Does this issue affect planned frontend features (see ROADMAP.md)?
f. Is the frontend actually broken due to this issue without us
   realizing?

### 4. Classify each issue's frontend impact

- **HIGH:** Frontend has a workaround that must be updated, or the
  frontend code must change when the backend issue is fixed.
- **MEDIUM:** Frontend will need non-trivial changes when fixed (new
  code paths, type updates, new UI elements).
- **LOW:** Frontend needs trivial updates when fixed (e.g., adding a
  reason string to a map, minor display tweak).
- **NONE:** No frontend code changes needed — even if the backend bug
  causes suboptimal UX, the frontend displays whatever the backend
  reports and will automatically benefit from the fix. Also includes
  test infra, CI, controller internals, and documentation issues.
  Skip these — do not create a tracking issue.

The key test: **will the frontend need code changes when this backend
issue is fixed?** If not, classify as NONE regardless of current UX
impact. The purpose of tracking issues is to flag frontend work that
needs doing, not to document backend bugs.

### 5. Check for existing tracking issues (deduplication)

Before creating a frontend tracking issue for backend issue #NNN, check
whether one already exists using the set of `frontend`-labeled issues
(both open and closed). Use a two-pass approach:

1. **Title match:** Does any `frontend`-labeled issue have a title
   containing `#NNN` (e.g., `[frontend] Backend #118: ...`)?
2. **Body match:** If no title match, does any `frontend`-labeled issue
   body contain the pattern `Backend issue: #NNN` or the full URL
   `https://github.com/stolostron/multicluster-mesh-addon/issues/NNN`?

If either check finds a match, that is the existing tracking issue —
update it rather than creating a duplicate. Log which match method was
used.

### 6. Present the plan for review

Before creating or modifying any GitHub issues, present a summary of all
planned actions to the user for approval. The summary should include:

**New issues to create:**

For each, show the title, severity, labels, and the full issue body.

**Existing issues to update:**

For each, show the issue number, what changed, and the updated body.

**Closed backend issues with pending frontend work:**

For each, show the issue number and the comment that would be posted.

Ask the user to confirm before proceeding. Do NOT create, update, or
comment on any GitHub issues until the user explicitly approves.

### 7. Create or update frontend tracking issues

After the user confirms, execute the planned actions.

**For new HIGH/MEDIUM/LOW issues with no existing tracking issue:**

Create a new issue:

```
gh issue create \
  --title "[frontend] Backend #NNN: <short title>" \
  --label "frontend" \
  --label "<other relevant labels>" \
  --body "$(cat <<'EOF'
Backend issue: #NNN
Labels: `frontend`, `<other relevant labels>`

**Impact:** <SEVERITY> — <one-line summary>.

**Backend issue:** <Brief description of what the backend issue is.>

**Frontend today:** <How the frontend currently handles this area —
what code is involved, any workarounds in place. Include code snippets
where relevant.>

**Frontend risk:** <Is the frontend broken, misleading, or showing
incorrect data because of this issue? Answer explicitly
(yes / no / partially) with explanation.>

**When backend is fixed:** <What happens to the frontend when the
backend resolves this — does anything need changing, and if so, what
specifically?>
EOF
)"
```

Additional labels to include when relevant (use the backend issue's own
labels): `trust`, `operator`, `status`, `api`, `controller`.

After creating each issue, apply labels via a Prow `/label` comment.
This works even without `triage` permission because Prow's bot applies
the label using its own credentials:

```
gh issue comment <ISSUE_NUMBER> --body "/label frontend"
```

Add additional labels with separate `/label` commands in the same
comment:

```
gh issue comment <ISSUE_NUMBER> --body "$(cat <<'EOF'
/label frontend
/label trust
/label status
EOF
)"
```

Verify labels were applied:

```
gh issue view <ISSUE_NUMBER> --json labels --jq '.labels[].name'
```

If labels are still missing after the Prow comment, the repo may not
have the Prow `label` plugin enabled. The labels are recorded in the
issue body ("Labels:" line) as a fallback so a maintainer can apply
them manually.

**For existing tracking issues that need updating:**

If the frontend code has changed since the tracking issue was last
written, update the issue body with the current analysis:

```
gh issue edit <ISSUE_NUMBER> --body "<updated body>"
```

**For backend issues that are now closed:**

If a backend issue is closed but its frontend tracking issue is still
open, re-read the relevant frontend code and check whether the frontend
work described in the tracking issue has already been done. Compare the
"When backend is fixed" section of the tracking issue against the
current frontend implementation.

If the frontend has already been updated:

```
gh issue comment <ISSUE_NUMBER> --body "Backend issue #NNN has been closed and the frontend has already been updated to address the changes described here. This issue can be closed."
```

If the frontend has NOT yet been updated:

```
gh issue comment <ISSUE_NUMBER> --body "Backend issue #NNN has been closed. The frontend changes described in this issue can now be implemented."
```

In either case, do NOT auto-close the tracking issue — only a human
closes it after verifying.

### 8. Report a summary

After processing all issues, report:

- How many backend issues were analyzed.
- How many had frontend impact (by severity).
- How many new tracking issues were created (list issue numbers).
- How many existing tracking issues were updated.
- How many backend issues were closed with pending frontend work.
- How many backend issues had no frontend impact (skipped).

## Migration from BACKEND-ISSUES.md

If `frontend/docs/BACKEND-ISSUES.md` exists, the first invocation should
seed GitHub issues from its existing analysis rather than re-analyzing
from scratch:

1. Read BACKEND-ISSUES.md to get the existing analysis for each issue.
2. For each HIGH/MEDIUM/LOW issue in the file, create a GitHub issue
   using the existing analysis content, reformatted into the template
   above.
3. Skip NONE-impact issues.
4. Apply the deduplication check (step 5) before creating each issue.

After the seed run completes, BACKEND-ISSUES.md can be deleted from the
repo. The GitHub issues (filtered by `frontend` label) are the source of
truth.
