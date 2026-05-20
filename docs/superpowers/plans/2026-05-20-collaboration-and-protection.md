# Collaboration & Branch Protection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship lightweight collaborators, GitLab-style branch protection, CODEOWNERS support, a Quickstart relocation, and GCS soft-delete — per the design in `docs/superpowers/specs/2026-05-20-collaboration-and-protection-design.md`.

**Architecture:** Five workstreams. (A) Quickstart relocates to the Code tab empty state. (B) Collaborators add a flat `collaborators[]` array on the repo doc + a new auth helper. (C) Branch protection lives in a `branchProtection` subcollection and enforces at the receive-pack handler and the PR merge handler. (D) CODEOWNERS parser + PR auto-request + merge gate + file-browser display. (E) `enable-soft-delete.sh` plus doc updates — no GCS object versioning.

**Tech Stack:** Go 1.x (backend), chi router, Firestore, GCS, React/Vite (frontend), `git-http-backend` CGI for Git Smart HTTP.

**Workstream dependency order:**

```
A (Quickstart)         ── independent, can ship anytime
E (GCS hardening)      ── independent, can ship anytime
B (Collaborators)      ── must ship before C, D-merge-gate
C (Branch protection)  ── must ship before D's merge-gate integration
D (CODEOWNERS)         ── parser + PR-open after B; merge gate after C
```

When dispatched to parallel agents, A/B/E can run in parallel; C starts when B's auth helper exists; D starts when B's auth helper exists, lands merge-gate after C.

---

## File Structure

### New files (Go backend)
- `internal/auth/repo_access.go` — `HasRepoAccess`, `CanPush`, `CanRead` helpers (Workstream B)
- `internal/auth/repo_access_test.go` — table-driven tests
- `internal/db/collaborators.go` — Firestore CRUD for collaborators (B)
- `internal/db/collaborators_test.go`
- `internal/db/branch_protection.go` — Firestore CRUD for rules (C)
- `internal/db/branch_protection_test.go`
- `internal/db/reviews.go` — PR review CRUD (D)
- `internal/db/reviews_test.go`
- `internal/git/protection.go` — `EnforcePush`, pattern matching, `RefUpdate` (C)
- `internal/git/protection_test.go`
- `internal/git/codeowners.go` — parser + matcher (D)
- `internal/git/codeowners_test.go`
- `internal/api/collaborators.go` — REST handlers (B)
- `internal/api/branch_protection.go` — REST handlers (C)
- `internal/api/codeowners.go` — file-browser endpoint (D)
- `internal/api/reviews.go` — review POST/GET (D)
- `scripts/enable-soft-delete.sh` — GCS soft-delete enablement (E)

### Modified files (Go)
- `internal/db/db.go` — extend `RepositoryMetadata` with `Collaborators []Collaborator`
- `internal/db/pull_requests.go` — add `RequestedReviewers []string` field
- `internal/api/git.go` — swap owner-only checks for `auth.CanPush` / `auth.CanRead`; add push-time protection enforcement (160, 168, 411, 421, 527, 599)
- `internal/api/pull_requests.go` — auto-resolve reviewers on PR open; merge-path enforcement
- `internal/api/api.go` — wire up new routes
- `PROJECT.md` — interface contract additions; GCS decision
- `CLAUDE.md` — reference GCS soft-delete rationale

### New/modified files (frontend)
- `frontend/src/pages/Repository.jsx` — remove Quickstart from Settings (A), add to Code empty state (A), add Collaborators card (B), add Branch Protection card (C), CODEOWNERS handles in file rows (D)
- `frontend/src/pages/PullRequest.jsx` or PR detail subcomponent — Reviewers section + Approve/Request-changes (D)
- (Create if absent) `frontend/src/components/BranchProtectionModal.jsx`
- (Create if absent) `frontend/src/components/CollaboratorsCard.jsx`

---

## Workstream A — Quickstart relocation

### Task A1: Move Quickstart from Settings to Code-tab empty state

**Files:**
- Modify: `frontend/src/pages/Repository.jsx:622-668` (remove); add to Code-tab render path

- [ ] **Step 1: Locate the Code-tab render path**

Open `frontend/src/pages/Repository.jsx`. Find the `{activeTab === 'code' && …}` block. Identify the empty-state branch (where `commits.length === 0` or equivalent).

- [ ] **Step 2: Extract the Quickstart block into a reusable component within the file**

Above the `Repository` component definition, define:

```jsx
function QuickstartCard({ cloneUrl, username }) {
  return (
    <div className="glass-card">
      <h3 style={{ fontSize: '1.25rem', marginBottom: '1rem', color: '#38bdf8' }}>Repository Command Quickstart</h3>
      <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1rem' }}>
        Configure your local command-line client to push and pull from this repository:
      </p>
      <pre style={{
        background: 'rgba(0,0,0,0.4)',
        border: '1px solid var(--border-color)',
        padding: '1.25rem',
        borderRadius: '6px',
        fontFamily: 'var(--font-mono)',
        fontSize: '0.85rem',
        color: '#e2e8f0',
        lineHeight: '1.6',
        whiteSpace: 'pre'
      }}>
{`# 1. Initialize a new git directory locally
git init
git checkout -b main

# 2. Add files and commit
git add .
git commit -m "initial commit"

# 3. Link remote repository
git remote add origin ${cloneUrl}

# 4. Push to Cloud Run (use your Username and PAT when prompted)
git push -u origin main`}
      </pre>
      <div style={{
        marginTop: '1rem',
        padding: '0.75rem 1rem',
        background: 'rgba(245, 158, 11, 0.08)',
        border: '1px solid rgba(245, 158, 11, 0.2)',
        borderRadius: '6px',
        color: '#f59e0b',
        fontSize: '0.85rem'
      }}>
        <strong>Note:</strong> When git asks you for credentials on push, use your username (<strong>{username}</strong>) and your generated <strong>Personal Access Token (PAT)</strong> as the password. Standard Firebase account passwords will not work on the command line.
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Remove the Quickstart block from the Settings tab**

Delete lines covering the `{/* Repository Setup Quickstart */}` block (currently `frontend/src/pages/Repository.jsx:625-668`). The block to delete starts with the comment and ends with the closing `</div>` of the outer glass-card.

- [ ] **Step 4: Render `<QuickstartCard>` on the Code tab when the repo is empty and the viewer is the owner**

Inside the Code tab block, where the empty state currently renders, add:

```jsx
{isOwner && commits.length === 0 && (
  <QuickstartCard cloneUrl={cloneUrl} username={user.username} />
)}
```

Place it adjacent to the existing empty-state messaging.

- [ ] **Step 5: Verify in browser**

Run:
```bash
cd frontend && npm run dev
```
Open a freshly created empty repo as owner — Quickstart should appear on Code tab. Push a commit — Quickstart should disappear. Open Settings — Quickstart should not appear anywhere.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/Repository.jsx
git commit -m "feat(ui): move Quickstart to Code tab empty state"
```

---

## Workstream B — Lightweight collaborators

### Task B1: Add `Collaborator` type and extend `RepositoryMetadata`

**Files:**
- Modify: `internal/db/db.go:126-155`

- [ ] **Step 1: Add the `Collaborator` struct and field**

In `internal/db/db.go`, just above the existing `RepositoryMetadata` definition, add:

```go
// Collaborator represents a non-owner user with access to a repository.
type Collaborator struct {
    UID       string    `json:"uid" firestore:"uid"`
    Username  string    `json:"username" firestore:"username"`
    AddedAt   time.Time `json:"addedAt" firestore:"addedAt"`
    AddedBy   string    `json:"addedBy" firestore:"addedBy"`
}
```

Then extend `RepositoryMetadata`:

```go
type RepositoryMetadata struct {
    OwnerUID      string         `json:"ownerUid" firestore:"ownerUid"`
    Owner         string         `json:"owner" firestore:"owner"`
    // ... existing fields ...
    Collaborators []Collaborator `json:"collaborators" firestore:"collaborators"`
}
```

Update the manual `m["..."]` decoder helper to deserialize `collaborators` if you maintain one; otherwise rely on `DataTo` which handles it automatically.

- [ ] **Step 2: Verify build**

```bash
go build ./...
```
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/db/db.go
git commit -m "feat(db): add Collaborator type and field on RepositoryMetadata"
```

### Task B2: Write failing tests for collaborators CRUD

**Files:**
- Create: `internal/db/collaborators_test.go`

- [ ] **Step 1: Write the test file**

```go
package db

import (
    "context"
    "testing"
    "time"

    "cloud.google.com/go/firestore"
)

func TestAddCollaborator(t *testing.T) {
    ctx := context.Background()
    client := newTestFirestore(t, ctx)
    defer client.Close()

    // Seed a repo
    if err := CreateRepositoryMetadata(ctx, client, "owner-uid", "owner", "repo1", "", "private"); err != nil {
        t.Fatalf("seed: %v", err)
    }
    // Seed a user to add
    _, err := client.Collection("users").Doc("alice-uid").Set(ctx, map[string]interface{}{"username": "alice"})
    if err != nil { t.Fatalf("seed user: %v", err) }

    err = AddCollaborator(ctx, client, "owner", "repo1", "alice-uid", "alice", "owner-uid")
    if err != nil { t.Fatalf("AddCollaborator: %v", err) }

    collab, err := ListCollaborators(ctx, client, "owner", "repo1")
    if err != nil { t.Fatalf("ListCollaborators: %v", err) }
    if len(collab) != 1 || collab[0].Username != "alice" {
        t.Fatalf("unexpected: %+v", collab)
    }
}

func TestAddCollaboratorIdempotent(t *testing.T) {
    ctx := context.Background()
    client := newTestFirestore(t, ctx)
    defer client.Close()
    _ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r", "", "private")

    _ = AddCollaborator(ctx, client, "owner", "r", "u1", "alice", "o")
    err := AddCollaborator(ctx, client, "owner", "r", "u1", "alice", "o")
    if err != nil { t.Fatalf("second add: %v", err) }

    list, _ := ListCollaborators(ctx, client, "owner", "r")
    if len(list) != 1 { t.Fatalf("expected 1, got %d", len(list)) }
}

func TestRemoveCollaborator(t *testing.T) {
    ctx := context.Background()
    client := newTestFirestore(t, ctx)
    defer client.Close()
    _ = CreateRepositoryMetadata(ctx, client, "o", "owner", "r", "", "private")
    _ = AddCollaborator(ctx, client, "owner", "r", "u1", "alice", "o")

    err := RemoveCollaborator(ctx, client, "owner", "r", "alice")
    if err != nil { t.Fatalf("Remove: %v", err) }
    list, _ := ListCollaborators(ctx, client, "owner", "r")
    if len(list) != 0 { t.Fatalf("expected 0, got %d", len(list)) }
}

// newTestFirestore opens a client to the local emulator (FIRESTORE_EMULATOR_HOST).
// If the emulator isn't running, the test is skipped.
func newTestFirestore(t *testing.T, ctx context.Context) *firestore.Client {
    t.Helper()
    if v := getenvOr("FIRESTORE_EMULATOR_HOST", ""); v == "" {
        t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping emulator-backed test")
    }
    client, err := firestore.NewClient(ctx, "test-project")
    if err != nil { t.Fatalf("firestore client: %v", err) }
    // Clear collections between tests
    _ = clearCollection(ctx, client, "repositories")
    _ = clearCollection(ctx, client, "users")
    _ = time.Sleep
    return client
}
```

(If `newTestFirestore`, `clearCollection`, `getenvOr` helpers don't yet exist in the package, add them in a `testutil_test.go` file in the same package. Helpers should be minimal — wrap emulator client setup and collection clearing.)

- [ ] **Step 2: Run tests and verify they fail to compile**

```bash
go test ./internal/db -run TestAddCollaborator
```
Expected: compile error (`AddCollaborator`, `ListCollaborators`, `RemoveCollaborator` undefined).

### Task B3: Implement collaborator CRUD

**Files:**
- Create: `internal/db/collaborators.go`

- [ ] **Step 1: Implement the functions**

```go
package db

import (
    "context"
    "fmt"
    "strings"
    "time"

    "cloud.google.com/go/firestore"
)

// AddCollaborator appends a collaborator to the repo's collaborators array.
// No-op if a collaborator with the same UID already exists.
func AddCollaborator(ctx context.Context, client *firestore.Client, owner, repo, uid, username, addedBy string) error {
    if client == nil { return fmt.Errorf("firestore client is nil") }
    repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
    repoRef := client.Collection("repositories").Doc(repoId)

    return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
        doc, err := tx.Get(repoRef)
        if err != nil { return err }
        var data map[string]interface{}
        if err := doc.DataTo(&data); err != nil { return err }

        existing, _ := data["collaborators"].([]interface{})
        for _, c := range existing {
            cm, ok := c.(map[string]interface{})
            if !ok { continue }
            if cm["uid"] == uid { return nil }
        }
        entry := map[string]interface{}{
            "uid": uid, "username": username,
            "addedAt": time.Now(), "addedBy": addedBy,
        }
        return tx.Update(repoRef, []firestore.Update{
            {Path: "collaborators", Value: append(existing, entry)},
        })
    })
}

// ListCollaborators returns the collaborators on a repo.
func ListCollaborators(ctx context.Context, client *firestore.Client, owner, repo string) ([]Collaborator, error) {
    if client == nil { return nil, fmt.Errorf("firestore client is nil") }
    repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
    doc, err := client.Collection("repositories").Doc(repoId).Get(ctx)
    if err != nil { return nil, err }
    var meta RepositoryMetadata
    if err := doc.DataTo(&meta); err != nil { return nil, err }
    if meta.Collaborators == nil { return []Collaborator{}, nil }
    return meta.Collaborators, nil
}

// RemoveCollaborator removes the collaborator with the given username. No-op if not present.
func RemoveCollaborator(ctx context.Context, client *firestore.Client, owner, repo, username string) error {
    if client == nil { return fmt.Errorf("firestore client is nil") }
    repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
    repoRef := client.Collection("repositories").Doc(repoId)
    return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
        doc, err := tx.Get(repoRef)
        if err != nil { return err }
        var meta RepositoryMetadata
        if err := doc.DataTo(&meta); err != nil { return err }
        out := make([]Collaborator, 0, len(meta.Collaborators))
        for _, c := range meta.Collaborators {
            if c.Username != username { out = append(out, c) }
        }
        return tx.Update(repoRef, []firestore.Update{{Path: "collaborators", Value: out}})
    })
}
```

- [ ] **Step 2: Run tests**

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8084   # start emulator first if not running
go test ./internal/db -run TestAddCollaborator -run TestRemoveCollaborator -v
```
Expected: PASS (or SKIP if emulator unavailable — note in PR description).

- [ ] **Step 3: Commit**

```bash
git add internal/db/collaborators.go internal/db/collaborators_test.go
git commit -m "feat(db): add collaborator CRUD"
```

### Task B4: Write failing tests for repo-access helper

**Files:**
- Create: `internal/auth/repo_access_test.go`

- [ ] **Step 1: Write the test**

```go
package auth

import (
    "testing"
    "gitbucket/internal/db"   // adjust import path to match module
)

func TestCanPushOwner(t *testing.T) {
    meta := &db.RepositoryMetadata{OwnerUID: "u1"}
    if !CanPush(meta, "u1") { t.Fatal("owner should be able to push") }
}
func TestCanPushCollaborator(t *testing.T) {
    meta := &db.RepositoryMetadata{
        OwnerUID: "u1",
        Collaborators: []db.Collaborator{{UID: "u2", Username: "alice"}},
    }
    if !CanPush(meta, "u2") { t.Fatal("collaborator should be able to push") }
    if CanPush(meta, "u3") { t.Fatal("stranger should not be able to push") }
}
func TestCanReadPublic(t *testing.T) {
    meta := &db.RepositoryMetadata{Visibility: "public"}
    if !CanRead(meta, "") { t.Fatal("public should be readable by anyone") }
}
func TestCanReadPrivateRequiresAccess(t *testing.T) {
    meta := &db.RepositoryMetadata{Visibility: "private", OwnerUID: "u1"}
    if CanRead(meta, "u2") { t.Fatal("stranger cannot read private") }
    if !CanRead(meta, "u1") { t.Fatal("owner can read private") }
}
```

(Verify the actual module path with `head -1 go.mod` and substitute.)

- [ ] **Step 2: Run tests and verify they fail**

```bash
go test ./internal/auth -run TestCanPush
```
Expected: compile error.

### Task B5: Implement repo-access helper

**Files:**
- Create: `internal/auth/repo_access.go`

- [ ] **Step 1: Implement**

```go
package auth

import "gitbucket/internal/db"   // adjust to actual module path

// RepoAccess captures whether a user is the owner or a collaborator.
type RepoAccess struct {
    IsOwner        bool
    IsCollaborator bool
}

// HasRepoAccess returns the access tuple for the given uid against the repo metadata.
func HasRepoAccess(meta *db.RepositoryMetadata, uid string) RepoAccess {
    if meta == nil || uid == "" { return RepoAccess{} }
    if meta.OwnerUID == uid { return RepoAccess{IsOwner: true} }
    for _, c := range meta.Collaborators {
        if c.UID == uid { return RepoAccess{IsCollaborator: true} }
    }
    return RepoAccess{}
}

// CanPush is true when the user is the owner or a collaborator. Branch protection layers narrow this further.
func CanPush(meta *db.RepositoryMetadata, uid string) bool {
    ra := HasRepoAccess(meta, uid)
    return ra.IsOwner || ra.IsCollaborator
}

// CanRead is true when the repo is public, or the user has any access.
func CanRead(meta *db.RepositoryMetadata, uid string) bool {
    if meta == nil { return false }
    if meta.Visibility == "public" { return true }
    return CanPush(meta, uid)
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/auth -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/repo_access.go internal/auth/repo_access_test.go
git commit -m "feat(auth): add HasRepoAccess/CanPush/CanRead helpers"
```

### Task B6: Replace owner-only checks in `internal/api/git.go`

**Files:**
- Modify: `internal/api/git.go` at lines 160, 168, 411, 421, 527, 599

- [ ] **Step 1: Update each call site**

Each site currently checks `meta.OwnerUID != uid` (or equivalent) and returns `Unauthorized. ...requires ownership.`. Replace with the appropriate helper:
- Lines 160 (push), 411 (LFS push), 527 (LFS upload): use `!auth.CanPush(&meta, uid)`.
- Lines 168, 421, 599 (private read): use `!auth.CanRead(&meta, uid)`.

Example diff at line 160:

```go
// BEFORE
if meta.OwnerUID != uid {
    http.Error(w, "Unauthorized. Pushing requires ownership.", http.StatusUnauthorized)
    return
}

// AFTER
if !auth.CanPush(&meta, uid) {
    http.Error(w, "Unauthorized: push access required.", http.StatusUnauthorized)
    return
}
```

Add the `auth` import if not already present:
```go
"<module>/internal/auth"
```

- [ ] **Step 2: Build**

```bash
go build ./...
```
Expected: success.

- [ ] **Step 3: Run all tests**

```bash
go test ./internal/...
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/api/git.go
git commit -m "feat(api): honor collaborators in git push/LFS/private-read auth"
```

### Task B7: Add collaborator REST API

**Files:**
- Create: `internal/api/collaborators.go`
- Modify: `internal/api/api.go` (route wiring)

- [ ] **Step 1: Implement handlers**

```go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "<module>/internal/auth"
    "<module>/internal/db"
)

type collaboratorAddReq struct{ Username string `json:"username"` }

// GET /api/repos/{owner}/{repo}/collaborators
func (s *Server) ListCollaboratorsHandler(w http.ResponseWriter, r *http.Request) {
    owner := chi.URLParam(r, "owner")
    repo := chi.URLParam(r, "repo")
    meta, err := db.GetRepositoryMetadata(r.Context(), s.FirestoreClient, owner, repo)
    if err != nil { http.Error(w, err.Error(), http.StatusNotFound); return }

    uid, _ := auth.UIDFromContext(r.Context())
    if !auth.CanRead(meta, uid) { http.Error(w, "forbidden", http.StatusForbidden); return }

    list, err := db.ListCollaborators(r.Context(), s.FirestoreClient, owner, repo)
    if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
    _ = json.NewEncoder(w).Encode(list)
}

// POST /api/repos/{owner}/{repo}/collaborators  (owner only)
func (s *Server) AddCollaboratorHandler(w http.ResponseWriter, r *http.Request) {
    owner := chi.URLParam(r, "owner")
    repo := chi.URLParam(r, "repo")
    uid, _ := auth.UIDFromContext(r.Context())
    meta, err := db.GetRepositoryMetadata(r.Context(), s.FirestoreClient, owner, repo)
    if err != nil { http.Error(w, err.Error(), http.StatusNotFound); return }
    if meta.OwnerUID != uid { http.Error(w, "forbidden", http.StatusForbidden); return }

    var req collaboratorAddReq
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w, "bad request", http.StatusBadRequest); return }

    targetUID, err := db.GetUIDByUsername(r.Context(), s.FirestoreClient, req.Username)
    if err != nil { http.Error(w, "user not found", http.StatusNotFound); return }

    if err := db.AddCollaborator(r.Context(), s.FirestoreClient, owner, repo, targetUID, req.Username, uid); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError); return
    }
    w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/repos/{owner}/{repo}/collaborators/{username}  (owner only)
func (s *Server) RemoveCollaboratorHandler(w http.ResponseWriter, r *http.Request) {
    owner := chi.URLParam(r, "owner")
    repo := chi.URLParam(r, "repo")
    username := chi.URLParam(r, "username")
    uid, _ := auth.UIDFromContext(r.Context())
    meta, err := db.GetRepositoryMetadata(r.Context(), s.FirestoreClient, owner, repo)
    if err != nil { http.Error(w, err.Error(), http.StatusNotFound); return }
    if meta.OwnerUID != uid { http.Error(w, "forbidden", http.StatusForbidden); return }

    if err := db.RemoveCollaborator(r.Context(), s.FirestoreClient, owner, repo, username); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError); return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

If `GetUIDByUsername` doesn't exist in `internal/db`, add it (small helper that queries `users` collection by `username` field, returns doc ID).

- [ ] **Step 2: Wire routes**

In `internal/api/api.go`, inside the chi router setup for `/api/repos/{owner}/{repo}/...`:

```go
r.Get("/collaborators", s.ListCollaboratorsHandler)               // OptionalWebAuth group
// inside RequireWebAuth group:
r.Post("/collaborators", s.AddCollaboratorHandler)
r.Delete("/collaborators/{username}", s.RemoveCollaboratorHandler)
```

Match the existing chi route style.

- [ ] **Step 3: Manual smoke test**

```bash
go run main.go &
# Use curl with a dev-mode token:
curl -H "Authorization: Bearer mock_owner-uid" -H "Content-Type: application/json" \
     -d '{"username":"alice"}' \
     http://localhost:8080/api/repos/owner/repo/collaborators
curl -H "Authorization: Bearer mock_owner-uid" http://localhost:8080/api/repos/owner/repo/collaborators
```

Expected: `204` on POST; JSON array with `alice` on GET.

- [ ] **Step 4: Commit**

```bash
git add internal/api/collaborators.go internal/api/api.go internal/db/
git commit -m "feat(api): collaborator add/list/remove endpoints"
```

### Task B8: Frontend — Collaborators card on Settings

**Files:**
- Modify: `frontend/src/pages/Repository.jsx`
- Create (optional refactor): `frontend/src/components/CollaboratorsCard.jsx`

- [ ] **Step 1: Add a Collaborators card above "Repository Settings"**

Inside the `{activeTab === 'settings' && isOwner && (...)}` block, before the "Repository Settings" card, insert a new card. Code:

```jsx
<div className="glass-card">
  <h3 style={{ fontSize: '1.25rem', marginBottom: '1rem', color: '#38bdf8' }}>Collaborators</h3>
  <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1rem' }}>
    Users with push and read access to this repository.
  </p>
  <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
    <input
      className="text-input"
      placeholder="username"
      value={newCollabUsername}
      onChange={(e) => setNewCollabUsername(e.target.value)}
      style={{ flex: 1 }}
    />
    <button className="btn" onClick={addCollaborator} disabled={!newCollabUsername.trim()}>Add</button>
  </div>
  {collaboratorError && <div style={{ color: '#ef4444', marginBottom: '0.5rem' }}>{collaboratorError}</div>}
  <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
    {collaborators.map(c => (
      <li key={c.uid} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.5rem 0', borderBottom: '1px solid var(--border-color)' }}>
        <span>{c.username}</span>
        <button className="btn-ghost" onClick={() => removeCollaborator(c.username)}>Remove</button>
      </li>
    ))}
    {collaborators.length === 0 && <li style={{ color: '#64748b' }}>No collaborators yet.</li>}
  </ul>
</div>
```

Add the state and handlers at the top of the component:

```jsx
const [collaborators, setCollaborators] = useState([]);
const [newCollabUsername, setNewCollabUsername] = useState('');
const [collaboratorError, setCollaboratorError] = useState('');

useEffect(() => {
  if (activeTab !== 'settings' || !isOwner) return;
  fetch(`/api/repos/${owner}/${repo}/collaborators`, { headers: authHeaders() })
    .then(r => r.json()).then(setCollaborators).catch(() => {});
}, [owner, repo, activeTab, isOwner]);

const addCollaborator = async () => {
  setCollaboratorError('');
  const res = await fetch(`/api/repos/${owner}/${repo}/collaborators`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: newCollabUsername.trim() }),
  });
  if (!res.ok) { setCollaboratorError((await res.text()) || 'Failed to add'); return; }
  setNewCollabUsername('');
  const list = await fetch(`/api/repos/${owner}/${repo}/collaborators`, { headers: authHeaders() }).then(r => r.json());
  setCollaborators(list);
};

const removeCollaborator = async (username) => {
  await fetch(`/api/repos/${owner}/${repo}/collaborators/${username}`, { method: 'DELETE', headers: authHeaders() });
  setCollaborators(collaborators.filter(c => c.username !== username));
};
```

(Replace `authHeaders()` with the project's existing helper — search the file for an existing usage and reuse it.)

- [ ] **Step 2: Verify in browser**

```bash
cd frontend && npm run dev
```
Add and remove a collaborator; refresh and confirm persistence.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Repository.jsx
git commit -m "feat(ui): collaborators card on Settings"
```

---

## Workstream C — Branch protection

### Task C1: Define `Rule` type and protection package skeleton

**Files:**
- Create: `internal/git/protection.go`

- [ ] **Step 1: Define types**

```go
package git

import (
    "path/filepath"
    "sort"
    "strings"
)

type Rule struct {
    ID                       string   `json:"id" firestore:"id"`
    Pattern                  string   `json:"pattern" firestore:"pattern"`
    PushAllowlist            []string `json:"pushAllowlist" firestore:"pushAllowlist"`
    MergeAllowlist           []string `json:"mergeAllowlist" firestore:"mergeAllowlist"`
    RequirePullRequest       bool     `json:"requirePullRequest" firestore:"requirePullRequest"`
    RequireApprovals         int      `json:"requireApprovals" firestore:"requireApprovals"`
    RequireCodeownerApproval bool     `json:"requireCodeownerApproval" firestore:"requireCodeownerApproval"`
    BlockForcePush           bool     `json:"blockForcePush" firestore:"blockForcePush"`
    BlockDeletion            bool     `json:"blockDeletion" firestore:"blockDeletion"`
}

type RefUpdate struct {
    RefName  string // e.g. "refs/heads/main"
    OldSha   string
    NewSha   string
    IsForce  bool
    IsDelete bool
}

type EnforceResult struct {
    Rejected []RefUpdate
    Reasons  map[string]string // ref → human reason
}

// MatchRule returns the most-specific rule matching the branch name, or nil.
// Branch is the short name (e.g. "main"), not the full ref.
func MatchRule(rules []Rule, branch string) *Rule {
    var matched []Rule
    for _, r := range rules {
        ok, _ := filepath.Match(r.Pattern, branch)
        if ok { matched = append(matched, r) }
    }
    if len(matched) == 0 { return nil }
    sort.SliceStable(matched, func(i, j int) bool {
        ai, aj := wildcardCount(matched[i].Pattern), wildcardCount(matched[j].Pattern)
        if ai != aj { return ai < aj }
        if len(matched[i].Pattern) != len(matched[j].Pattern) { return len(matched[i].Pattern) > len(matched[j].Pattern) }
        return matched[i].Pattern < matched[j].Pattern
    })
    return &matched[0]
}

func wildcardCount(s string) int { return strings.Count(s, "*") + strings.Count(s, "?") + strings.Count(s, "[") }
```

- [ ] **Step 2: Build**

```bash
go build ./internal/git
```
Expected: success.

### Task C2: Write failing tests for `EnforcePush`

**Files:**
- Create: `internal/git/protection_test.go`

- [ ] **Step 1: Write tests**

```go
package git

import "testing"

func TestEnforcePushBlockForcePush(t *testing.T) {
    rules := []Rule{{Pattern: "main", BlockForcePush: true}}
    updates := []RefUpdate{{RefName: "refs/heads/main", OldSha: "abc", NewSha: "def", IsForce: true}}
    res := EnforcePush(rules, updates, "u1")
    if len(res.Rejected) != 1 { t.Fatalf("expected 1 reject, got %d", len(res.Rejected)) }
}

func TestEnforcePushBlockDeletion(t *testing.T) {
    rules := []Rule{{Pattern: "main", BlockDeletion: true}}
    updates := []RefUpdate{{RefName: "refs/heads/main", OldSha: "abc", NewSha: "0000000000000000000000000000000000000000", IsDelete: true}}
    res := EnforcePush(rules, updates, "u1")
    if len(res.Rejected) != 1 { t.Fatal("expected 1 reject") }
}

func TestEnforcePushAllowlist(t *testing.T) {
    rules := []Rule{{Pattern: "main", PushAllowlist: []string{"u-good"}}}
    res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-good")
    if len(res.Rejected) != 0 { t.Fatal("u-good should be allowed") }
    res = EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-bad")
    if len(res.Rejected) != 1 { t.Fatal("u-bad should be rejected") }
}

func TestEnforcePushEmptyAllowlistMeansNoDirectPush(t *testing.T) {
    rules := []Rule{{Pattern: "main", PushAllowlist: []string{}}}
    res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/main", OldSha: "a", NewSha: "b"}}, "u-anybody")
    if len(res.Rejected) != 1 { t.Fatal("empty allowlist must reject all") }
}

func TestEnforcePushNonMatchingRefPasses(t *testing.T) {
    rules := []Rule{{Pattern: "main", BlockForcePush: true}}
    res := EnforcePush(rules, []RefUpdate{{RefName: "refs/heads/feature/x", OldSha: "a", NewSha: "b", IsForce: true}}, "u")
    if len(res.Rejected) != 0 { t.Fatal("feature branch should not match main rule") }
}

func TestMatchRuleMostSpecific(t *testing.T) {
    rules := []Rule{{Pattern: "*"}, {Pattern: "release/*"}, {Pattern: "release/v1"}}
    m := MatchRule(rules, "release/v1")
    if m == nil || m.Pattern != "release/v1" { t.Fatalf("got %+v", m) }
}
```

- [ ] **Step 2: Run and verify fail**

```bash
go test ./internal/git -run TestEnforcePush
```
Expected: `EnforcePush` undefined.

### Task C3: Implement `EnforcePush`

**Files:**
- Modify: `internal/git/protection.go`

- [ ] **Step 1: Add implementation**

```go
// EnforcePush evaluates each ref update against the rule set and returns the rejections.
// "branch" is derived from RefName by stripping refs/heads/. Refs outside refs/heads/ are not gated by branch rules in v1.
func EnforcePush(rules []Rule, updates []RefUpdate, pusherUid string) EnforceResult {
    out := EnforceResult{Reasons: map[string]string{}}
    for _, u := range updates {
        branch := strings.TrimPrefix(u.RefName, "refs/heads/")
        if branch == u.RefName {
            // not a branch (e.g. tag) — skip in v1
            continue
        }
        rule := MatchRule(rules, branch)
        if rule == nil { continue }

        if u.IsDelete && rule.BlockDeletion {
            out.Rejected = append(out.Rejected, u)
            out.Reasons[u.RefName] = "branch deletion is blocked by protection rule"
            continue
        }
        if u.IsForce && rule.BlockForcePush {
            out.Rejected = append(out.Rejected, u)
            out.Reasons[u.RefName] = "force-push is blocked by protection rule"
            continue
        }
        if !inAllowlist(rule.PushAllowlist, pusherUid) {
            out.Rejected = append(out.Rejected, u)
            out.Reasons[u.RefName] = "direct push to this branch is not allowed"
            continue
        }
    }
    return out
}

func inAllowlist(list []string, uid string) bool {
    // empty list = nobody
    for _, x := range list { if x == uid { return true } }
    return false
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/git -v -run "TestEnforcePush|TestMatchRule"
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/git/protection.go internal/git/protection_test.go
git commit -m "feat(git): EnforcePush + branch rule pattern matching"
```

### Task C4: Firestore CRUD for rules

**Files:**
- Create: `internal/db/branch_protection.go`
- Create: `internal/db/branch_protection_test.go`

- [ ] **Step 1: Write tests first** (mirror the pattern from Task B2 — list/upsert/delete)

Tests should cover: `CreateRule`, `ListRules`, `GetRule`, `UpdateRule`, `DeleteRule`. Use the emulator-skip pattern from `newTestFirestore`.

- [ ] **Step 2: Implement**

Rules live at `repositories/{repoId}/branchProtection/{ruleId}`. `ruleId` is a deterministic hash of `pattern`:

```go
package db

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "strings"

    "cloud.google.com/go/firestore"
    git "<module>/internal/git"
)

func ruleID(pattern string) string {
    h := sha256.Sum256([]byte(pattern))
    return hex.EncodeToString(h[:8])
}

func rulesCol(client *firestore.Client, owner, repo string) *firestore.CollectionRef {
    repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
    return client.Collection("repositories").Doc(repoId).Collection("branchProtection")
}

func CreateRule(ctx context.Context, client *firestore.Client, owner, repo string, rule git.Rule) (git.Rule, error) {
    rule.ID = ruleID(rule.Pattern)
    _, err := rulesCol(client, owner, repo).Doc(rule.ID).Set(ctx, rule)
    return rule, err
}

func ListRules(ctx context.Context, client *firestore.Client, owner, repo string) ([]git.Rule, error) {
    iter := rulesCol(client, owner, repo).Documents(ctx)
    var out []git.Rule
    for {
        doc, err := iter.Next()
        if err != nil { break }
        var r git.Rule
        if err := doc.DataTo(&r); err == nil { out = append(out, r) }
    }
    return out, nil
}

func GetRule(ctx context.Context, client *firestore.Client, owner, repo, id string) (*git.Rule, error) {
    doc, err := rulesCol(client, owner, repo).Doc(id).Get(ctx)
    if err != nil { return nil, err }
    var r git.Rule
    if err := doc.DataTo(&r); err != nil { return nil, err }
    return &r, nil
}

func UpdateRule(ctx context.Context, client *firestore.Client, owner, repo string, rule git.Rule) error {
    rule.ID = ruleID(rule.Pattern)
    _, err := rulesCol(client, owner, repo).Doc(rule.ID).Set(ctx, rule)
    return err
}

func DeleteRule(ctx context.Context, client *firestore.Client, owner, repo, id string) error {
    _, err := rulesCol(client, owner, repo).Doc(id).Delete(ctx)
    return err
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/db -v -run TestRule
```
Expected: PASS (or SKIP if emulator off).

- [ ] **Step 4: Commit**

```bash
git add internal/db/branch_protection.go internal/db/branch_protection_test.go
git commit -m "feat(db): branch protection rule CRUD"
```

### Task C5: REST API for branch protection

**Files:**
- Create: `internal/api/branch_protection.go`
- Modify: `internal/api/api.go`

- [ ] **Step 1: Implement handlers**

Follow the pattern from `internal/api/collaborators.go`:
- `GET /api/repos/{o}/{r}/branch-protection` → read-access; returns `[]git.Rule`.
- `POST /api/repos/{o}/{r}/branch-protection` → owner-only; body is `git.Rule`. Validate `pattern` non-empty, ≤200 chars, `filepath.Match(pattern, "test")` does not error.
- `PUT /api/repos/{o}/{r}/branch-protection/{ruleId}` → owner-only; full replace.
- `DELETE /api/repos/{o}/{r}/branch-protection/{ruleId}` → owner-only.

Pattern validation helper:
```go
func validatePattern(p string) error {
    if p == "" || len(p) > 200 { return fmt.Errorf("invalid pattern length") }
    if _, err := filepath.Match(p, "test"); err != nil { return fmt.Errorf("invalid pattern: %w", err) }
    return nil
}
```

- [ ] **Step 2: Wire routes in `internal/api/api.go`** following the existing route style.

- [ ] **Step 3: Manual smoke test**

```bash
curl -H "Authorization: Bearer mock_owner-uid" -H "Content-Type: application/json" \
     -d '{"pattern":"main","blockForcePush":true,"blockDeletion":true,"requirePullRequest":true,"requireApprovals":1}' \
     http://localhost:8080/api/repos/owner/repo/branch-protection
curl -H "Authorization: Bearer mock_owner-uid" http://localhost:8080/api/repos/owner/repo/branch-protection
```

- [ ] **Step 4: Commit**

```bash
git add internal/api/branch_protection.go internal/api/api.go
git commit -m "feat(api): branch protection CRUD endpoints"
```

### Task C6: Integrate push-time enforcement into `internal/api/git.go`

**Files:**
- Modify: `internal/api/git.go` (receive-pack handler)

- [ ] **Step 1: Locate the receive-pack code path**

In `internal/api/git.go`, find where `git-http-backend` is invoked for `git-receive-pack`. The current flow: parse pack → run CGI → sync new objects to GCS → sync new refs to Firestore.

- [ ] **Step 2: Compute proposed ref updates from the post-CGI repo state**

Just before syncing refs back, diff the current refs in the materialized repo vs the previous refs (loaded from Firestore at the start of the handler). Produce `[]RefUpdate` with `IsForce` (new not descendant of old, via `git merge-base --is-ancestor`) and `IsDelete` (new SHA = `0000…`).

Add helper in `internal/git/`:

```go
// IsAncestor returns true if old is an ancestor of new in repoPath.
func IsAncestor(repoPath, oldSha, newSha string) bool {
    if oldSha == "" || oldSha == "0000000000000000000000000000000000000000" { return true }
    cmd := exec.Command("git", "-C", repoPath, "merge-base", "--is-ancestor", oldSha, newSha)
    return cmd.Run() == nil
}
```

- [ ] **Step 3: Apply `EnforcePush` and reject when needed**

```go
rules, _ := db.ListRules(ctx, fs, owner, repo)
res := git.EnforcePush(rules, updates, uid)
if len(res.Rejected) > 0 {
    // Reset the materialized repo to its pre-push state (drop new refs / objects)
    // Easiest: re-materialize from GCS+Firestore in next request; for this request, surface error.
    msg := strings.Builder{}
    for ref, why := range res.Reasons { fmt.Fprintf(&msg, "%s: %s\n", ref, why) }
    http.Error(w, "push rejected by branch protection:\n"+msg.String(), http.StatusForbidden)
    return
}
```

**Important:** because the CGI already accepted the pack, "reject" must prevent GCS/Firestore sync. Do not call the sync-back code path on rejection. The materialized working directory on disk is ephemeral — next request re-pulls from GCS.

- [ ] **Step 4: Add Go test for the integration**

Add to `internal/api/git_test.go` (create if missing): test that pushing to a branch with `BlockForcePush=true` is rejected. Use httptest + a real git repo on disk. (If the existing test harness for git.go is heavy, gate this behind `-tags integration` and document.)

- [ ] **Step 5: Run tests**

```bash
go test ./internal/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/git.go internal/git/
git commit -m "feat(git): enforce branch protection at receive-pack"
```

### Task C7: Frontend — Branch Protection card

**Files:**
- Modify: `frontend/src/pages/Repository.jsx`
- Optionally create: `frontend/src/components/BranchProtectionModal.jsx`

- [ ] **Step 1: List view in Settings**

Add a "Branch protection" card under Collaborators. Fetch `/api/repos/{o}/{r}/branch-protection`, render a table with columns: Pattern, Push allowlist, Merge allowlist, Toggles (badges for each enabled flag), Actions (Edit, Delete).

- [ ] **Step 2: Add/Edit modal**

Modal with fields:
- Pattern (text)
- Push allowlist (multiselect from collaborators + owner)
- Merge allowlist (multiselect from collaborators + owner)
- Require PR (checkbox)
- Required approvals (number, ≥0)
- Require codeowner approval (checkbox)
- Block force push (checkbox)
- Block deletion (checkbox)

Default: owner pre-selected in both allowlists when creating new rule.

- [ ] **Step 3: Wire submit handlers**

POST for create, PUT for edit, DELETE for remove. After each, refetch list.

- [ ] **Step 4: Verify in browser end-to-end**

Create a rule with BlockForcePush. From CLI: `git push --force` — confirm rejection with the protection message.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/Repository.jsx frontend/src/components/BranchProtectionModal.jsx
git commit -m "feat(ui): branch protection settings card"
```

---

## Workstream D — CODEOWNERS

### Task D1: Write failing tests for parser

**Files:**
- Create: `internal/git/codeowners_test.go`

- [ ] **Step 1: Write tests**

```go
package git

import (
    "strings"
    "testing"
)

func TestCodeOwnersBasic(t *testing.T) {
    co, err := ParseCodeOwners(strings.NewReader(`
# Comment
*           @alice
*.go        @bob
/docs/      @carol
`))
    if err != nil { t.Fatalf("parse: %v", err) }

    tests := []struct{ path string; want []string }{
        {"README.md", []string{"@alice"}},
        {"main.go", []string{"@bob"}},
        {"docs/intro.md", []string{"@carol"}},
        {"src/foo.go", []string{"@bob"}}, // last matching wins
    }
    for _, tt := range tests {
        got := co.Match(tt.path)
        if !equalSlice(got, tt.want) { t.Errorf("Match(%q)=%v want %v", tt.path, got, tt.want) }
    }
}

func TestCodeOwnersLastMatchWins(t *testing.T) {
    co, _ := ParseCodeOwners(strings.NewReader(`
*       @everyone
*.go    @gopher
`))
    got := co.Match("main.go")
    if len(got) != 1 || got[0] != "@gopher" { t.Fatalf("got %v", got) }
}

func TestCodeOwnersDoubleStar(t *testing.T) {
    co, _ := ParseCodeOwners(strings.NewReader(`/internal/** @backend`))
    got := co.Match("internal/auth/repo_access.go")
    if len(got) != 1 || got[0] != "@backend" { t.Fatalf("got %v", got) }
}

func equalSlice(a, b []string) bool {
    if len(a) != len(b) { return false }
    for i := range a { if a[i] != b[i] { return false } }
    return true
}
```

- [ ] **Step 2: Run and verify fail**

```bash
go test ./internal/git -run TestCodeOwners
```
Expected: `ParseCodeOwners` undefined.

### Task D2: Implement parser

**Files:**
- Create: `internal/git/codeowners.go`

- [ ] **Step 1: Implementation**

```go
package git

import (
    "bufio"
    "io"
    "path"
    "strings"
)

type CodeOwnersRule struct {
    Pattern string
    Owners  []string
    LineNo  int
}

type CodeOwners struct { Rules []CodeOwnersRule }

func ParseCodeOwners(r io.Reader) (*CodeOwners, error) {
    co := &CodeOwners{}
    s := bufio.NewScanner(r)
    line := 0
    for s.Scan() {
        line++
        raw := strings.TrimSpace(s.Text())
        if i := strings.Index(raw, "#"); i >= 0 { raw = strings.TrimSpace(raw[:i]) }
        if raw == "" { continue }
        fields := strings.Fields(raw)
        if len(fields) < 2 { continue }
        co.Rules = append(co.Rules, CodeOwnersRule{Pattern: fields[0], Owners: fields[1:], LineNo: line})
    }
    return co, s.Err()
}

// Match returns the owners for the path. Last-matching-rule wins; if no rule matches, returns nil.
func (c *CodeOwners) Match(p string) []string {
    p = strings.TrimPrefix(path.Clean(p), "/")
    var winners []string
    for _, r := range c.Rules {
        if codeownerMatch(r.Pattern, p) { winners = r.Owners }
    }
    return winners
}

// codeownerMatch supports a simplified subset of gitignore/CODEOWNERS syntax:
//   - leading "/" anchors to root
//   - trailing "/" matches directory only
//   - "**" matches across path separators
//   - "*", "?" behave as in filepath.Match within a segment
func codeownerMatch(pattern, p string) bool {
    pat := pattern
    anchored := strings.HasPrefix(pat, "/")
    pat = strings.TrimPrefix(pat, "/")
    dirOnly := strings.HasSuffix(pat, "/")
    pat = strings.TrimSuffix(pat, "/")

    // If the pattern has no "/" and is not anchored, it matches a file/dir anywhere — apply to basename.
    if !anchored && !strings.Contains(pat, "/") {
        base := path.Base(p)
        m, _ := path.Match(pat, base)
        if m { return true }
    }

    // Convert "**" to a marker, then do segment-by-segment match.
    return doubleStarMatch(pat, p, dirOnly)
}

func doubleStarMatch(pat, p string, dirOnly bool) bool {
    // Greedy "**" handling: split on "**" and match left-anchored.
    parts := strings.Split(pat, "**")
    cursor := 0
    for i, part := range parts {
        part = strings.Trim(part, "/")
        if part == "" { continue }
        if i == 0 {
            if !startsWithMatch(p[cursor:], part) { return false }
            cursor += matchLen(p[cursor:], part)
        } else {
            idx := findMatch(p[cursor:], part)
            if idx < 0 { return false }
            cursor += idx + matchLen(p[cursor+idx:], part)
        }
    }
    _ = dirOnly
    return true
}

func startsWithMatch(s, pat string) bool { return strings.HasPrefix(s, pat) || globPrefix(s, pat) }
func matchLen(s, pat string) int        { if strings.HasPrefix(s, pat) { return len(pat) }; return len(pat) }
func findMatch(s, pat string) int        { return strings.Index(s, pat) }
func globPrefix(s, pat string) bool {
    m, _ := path.Match(pat, s)
    return m
}
```

(If this simplified matcher fails edge cases the team uses, swap it for the `doublestar` package — `github.com/bmatcuk/doublestar/v4` — and re-run tests. Add as a dep only if necessary.)

- [ ] **Step 2: Run tests**

```bash
go test ./internal/git -v -run TestCodeOwners
```
Expected: PASS. If anything fails, switch to `doublestar`:

```bash
go get github.com/bmatcuk/doublestar/v4
```
and replace `codeownerMatch` body with `doublestar.Match(pattern, p)`.

- [ ] **Step 3: Commit**

```bash
git add internal/git/codeowners.go internal/git/codeowners_test.go
git commit -m "feat(git): CODEOWNERS parser and matcher"
```

### Task D3: Resolve CODEOWNERS file from materialized repo

**Files:**
- Modify: `internal/git/codeowners.go`

- [ ] **Step 1: Add a loader**

```go
import "os"

// LoadCodeOwners looks for CODEOWNERS at root, .gitbucket/, docs/ — first found wins.
// Returns empty (non-nil) CodeOwners if no file exists.
func LoadCodeOwners(repoPath string) (*CodeOwners, error) {
    for _, rel := range []string{"CODEOWNERS", ".gitbucket/CODEOWNERS", "docs/CODEOWNERS"} {
        f, err := os.Open(repoPath + "/" + rel)
        if err == nil {
            defer f.Close()
            return ParseCodeOwners(f)
        }
    }
    return &CodeOwners{}, nil
}
```

- [ ] **Step 2: Add a test using a temp dir**

```go
func TestLoadCodeOwnersResolutionOrder(t *testing.T) {
    dir := t.TempDir()
    os.MkdirAll(dir+"/docs", 0o755)
    os.WriteFile(dir+"/docs/CODEOWNERS", []byte("* @docs"), 0o644)
    co, _ := LoadCodeOwners(dir)
    if len(co.Match("README.md")) != 1 || co.Match("README.md")[0] != "@docs" {
        t.Fatal("expected docs/CODEOWNERS to be used")
    }
    os.WriteFile(dir+"/CODEOWNERS", []byte("* @root"), 0o644)
    co, _ = LoadCodeOwners(dir)
    if co.Match("README.md")[0] != "@root" { t.Fatal("root must win over docs") }
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/git -run TestLoadCodeOwners -v
git add internal/git/codeowners.go internal/git/codeowners_test.go
git commit -m "feat(git): CODEOWNERS file resolution"
```

### Task D4: Reviews subcollection + types

**Files:**
- Modify: `internal/db/pull_requests.go` (add `RequestedReviewers []string` to `PullRequest`)
- Create: `internal/db/reviews.go`
- Create: `internal/db/reviews_test.go`

- [ ] **Step 1: Extend PullRequest**

In `internal/db/pull_requests.go`, add the field:

```go
type PullRequest struct {
    // ... existing fields ...
    RequestedReviewers []string `json:"requestedReviewers" firestore:"requestedReviewers"`
}
```

- [ ] **Step 2: Define Review type and CRUD**

```go
package db

import (
    "context"
    "fmt"
    "strconv"
    "strings"
    "time"

    "cloud.google.com/go/firestore"
)

type Review struct {
    UID         string    `json:"uid" firestore:"uid"`
    Username    string    `json:"username" firestore:"username"`
    State       string    `json:"state" firestore:"state"`     // approved | changes_requested | commented
    Body        string    `json:"body" firestore:"body"`
    SubmittedAt time.Time `json:"submittedAt" firestore:"submittedAt"`
}

func reviewsCol(client *firestore.Client, owner, repo string, n int) *firestore.CollectionRef {
    repoId := fmt.Sprintf("%s_%s", strings.ToLower(owner), strings.ToLower(repo))
    return client.Collection("repositories").Doc(repoId).Collection("pulls").Doc(strconv.Itoa(n)).Collection("reviews")
}

func UpsertReview(ctx context.Context, client *firestore.Client, owner, repo string, n int, r Review) error {
    r.SubmittedAt = time.Now()
    _, err := reviewsCol(client, owner, repo, n).Doc(r.UID).Set(ctx, r)
    return err
}

func ListReviews(ctx context.Context, client *firestore.Client, owner, repo string, n int) ([]Review, error) {
    iter := reviewsCol(client, owner, repo, n).Documents(ctx)
    var out []Review
    for {
        doc, err := iter.Next()
        if err != nil { break }
        var rv Review
        if err := doc.DataTo(&rv); err == nil { out = append(out, rv) }
    }
    return out, nil
}
```

- [ ] **Step 3: Test, commit**

```bash
go test ./internal/db -run TestReview -v
git add internal/db/pull_requests.go internal/db/reviews.go internal/db/reviews_test.go
git commit -m "feat(db): PR reviews subcollection"
```

### Task D5: Compute requestedReviewers on PR open

**Files:**
- Modify: `internal/api/pull_requests.go` (`CreatePullRequest` handler)

- [ ] **Step 1: After PR doc creation, compute diff and resolve owners**

Inside the create-PR handler, after `db.CreatePullRequest` returns successfully:

```go
// Materialize repo (or re-use existing materialized path)
repoPath := s.GitService.MaterializedPath(owner, repo)   // or whatever helper exists

cmd := exec.CommandContext(r.Context(), "git", "-C", repoPath,
    "diff", "--name-only", pr.TargetBranch+"..."+pr.SourceBranch)
out, err := cmd.Output()
if err != nil {
    // non-fatal: log and continue with empty reviewers
    log.Printf("diff for codeowners failed: %v", err)
}
files := strings.Split(strings.TrimSpace(string(out)), "\n")

co, _ := git.LoadCodeOwners(repoPath)
owners := map[string]struct{}{}
for _, f := range files {
    if f == "" { continue }
    for _, o := range co.Match(f) {
        u := strings.TrimPrefix(o, "@")
        if u != pr.AuthorUsername { owners[u] = struct{}{} }
    }
}
reviewers := make([]string, 0, len(owners))
for u := range owners { reviewers = append(reviewers, u) }
sort.Strings(reviewers)

if err := db.UpdatePullRequestReviewers(r.Context(), s.FirestoreClient, owner, repo, pr.Number, reviewers); err != nil {
    log.Printf("set reviewers: %v", err)
}
pr.RequestedReviewers = reviewers
```

(Add `UpdatePullRequestReviewers` helper in `internal/db/pull_requests.go` — single-field update on the PR doc.)

- [ ] **Step 2: Test**

Create a PR via API in dev mode against a repo that has a `CODEOWNERS` file. Verify the PR doc's `requestedReviewers` is populated.

- [ ] **Step 3: Commit**

```bash
git add internal/api/pull_requests.go internal/db/pull_requests.go
git commit -m "feat(pr): auto-resolve codeowners on PR open"
```

### Task D6: Review POST/GET API

**Files:**
- Create: `internal/api/reviews.go`
- Modify: `internal/api/api.go`

- [ ] **Step 1: Handlers**

```go
type reviewReq struct{ State, Body string }

// POST /api/repos/{o}/{r}/pulls/{n}/reviews  — RequireWebAuth + read access; reject if user is PR author
func (s *Server) SubmitReviewHandler(w http.ResponseWriter, r *http.Request) {
    // parse owner/repo/n from URL
    // load PR; reject if pr.AuthorUID == uid
    // validate state ∈ {approved, changes_requested, commented}
    // upsert via db.UpsertReview
    // 204
}

// GET /api/repos/{o}/{r}/pulls/{n}/reviews
func (s *Server) ListReviewsHandler(w http.ResponseWriter, r *http.Request) {
    // load PR; check CanRead
    // db.ListReviews → JSON
}
```

- [ ] **Step 2: Wire routes, commit**

```bash
git add internal/api/reviews.go internal/api/api.go
git commit -m "feat(api): PR review submit/list endpoints"
```

### Task D7: Merge-path enforcement (BP + codeowner gate)

**Files:**
- Modify: `internal/api/pull_requests.go` (merge handler)

- [ ] **Step 1: Pre-merge checks**

In the merge handler, before performing the merge ref update:

```go
rules, _ := db.ListRules(ctx, s.FirestoreClient, owner, repo)
rule := git.MatchRule(rules, pr.TargetBranch)
if rule != nil {
    if !contains(rule.MergeAllowlist, uid) {
        http.Error(w, "merge not allowed for this user", http.StatusForbidden); return
    }
    if rule.RequireApprovals > 0 || rule.RequireCodeownerApproval {
        reviews, _ := db.ListReviews(ctx, s.FirestoreClient, owner, repo, pr.Number)
        approvals := 0
        approvalUsernames := map[string]bool{}
        for _, rv := range reviews {
            if rv.State == "approved" { approvals++; approvalUsernames[rv.Username] = true }
        }
        if approvals < rule.RequireApprovals {
            http.Error(w, fmt.Sprintf("merge requires %d approvals (have %d)", rule.RequireApprovals, approvals), http.StatusForbidden); return
        }
        if rule.RequireCodeownerApproval {
            ok := false
            for _, ru := range pr.RequestedReviewers {
                if approvalUsernames[ru] { ok = true; break }
            }
            if !ok { http.Error(w, "merge requires approval from a codeowner", http.StatusForbidden); return }
        }
    }
}
```

- [ ] **Step 2: Test**

Existing test file: `internal/api/pull_requests_test.go`. Add cases: merge blocked by missing approval, merge blocked by missing codeowner approval, merge succeeds when both satisfied.

- [ ] **Step 3: Commit**

```bash
git add internal/api/pull_requests.go internal/api/pull_requests_test.go
git commit -m "feat(pr): enforce branch protection + codeowner approval on merge"
```

### Task D8: CODEOWNERS endpoint + file-browser UI

**Files:**
- Create: `internal/api/codeowners.go`
- Modify: `internal/api/api.go`
- Modify: `frontend/src/pages/Repository.jsx` (file browser rows)

- [ ] **Step 1: Endpoint**

```go
// GET /api/repos/{o}/{r}/codeowners?path={dir}&ref={sha}
// Returns: { "entries": { "filename": ["@alice"], ... } }
func (s *Server) CodeOwnersHandler(w http.ResponseWriter, r *http.Request) {
    // load meta + check CanRead
    // materialize repo at ref
    // list entries in dir (reuse the same listing helper as the Code tab)
    // co := git.LoadCodeOwners(repoPath)
    // for each entry: co.Match(joinedPath) → handles
    // JSON
}
```

- [ ] **Step 2: UI**

In the Code tab file listing, fetch CODEOWNERS map alongside the listing. Render handles next to each filename in muted text.

- [ ] **Step 3: Verify, commit**

```bash
git add internal/api/codeowners.go internal/api/api.go frontend/src/pages/Repository.jsx
git commit -m "feat: show CODEOWNERS handles in file browser"
```

### Task D9: PR detail page Reviewers section

**Files:**
- Modify: `frontend/src/pages/Repository.jsx` (PR detail) or dedicated PR component

- [ ] **Step 1: Render requested reviewers + their review state**

Fetch `/api/repos/{o}/{r}/pulls/{n}/reviews` on PR detail load. Join with `pr.requestedReviewers` to render:

```
Reviewers
  @alice  ✓ approved
  @bob    – pending
```

- [ ] **Step 2: Approve / Request changes / Comment buttons**

For the signed-in user when they are not the PR author. POST to the reviews endpoint with the chosen state. Refresh on success.

- [ ] **Step 3: Show merge block reasons**

If the merge endpoint returns 403, surface the response text under the Merge button.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/Repository.jsx
git commit -m "feat(ui): PR reviewers section + approval buttons"
```

---

## Workstream E — GCS hardening

### Task E1: Soft-delete script

**Files:**
- Create: `scripts/enable-soft-delete.sh`

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
set -euo pipefail

: "${GCS_BUCKET:?GCS_BUCKET must be set, e.g. git-bucket-repositories-79382}"

echo "Enabling 7-day soft-delete on gs://${GCS_BUCKET} ..."
gcloud storage buckets update "gs://${GCS_BUCKET}" --soft-delete-duration=7d

echo "Verifying ..."
gcloud storage buckets describe "gs://${GCS_BUCKET}" --format="value(softDeletePolicy.retentionDurationSeconds)"
```

```bash
chmod +x scripts/enable-soft-delete.sh
```

- [ ] **Step 2: Documentation**

Add to `PROJECT.md` under a new "Operations" or "Storage" section:

```markdown
### GCS object versioning

**Decision: not enabled.** Git objects (loose, pack, LFS blobs) are content-addressed
by SHA — they are immutable. Versioning protects against overwrite/delete of mutable
objects and provides no recovery benefit on content-addressed storage. The only
mutable state that benefits from recovery — refs and metadata — lives in Firestore.

### GCS soft-delete

The repositories bucket has soft-delete enabled with 7-day retention to guard against
accidental bulk-deletion or GC bugs. Enable on a new environment with
`scripts/enable-soft-delete.sh` (requires `gcloud` auth and `GCS_BUCKET` env).
```

- [ ] **Step 3: Reference from CLAUDE.md**

Append to the "Conventions worth knowing" section in `CLAUDE.md`:

```markdown
- GCS object versioning is intentionally disabled — Git objects are content-addressed. See `PROJECT.md` §GCS. Soft-delete (7d) is the recovery mechanism.
```

- [ ] **Step 4: Commit**

```bash
git add scripts/enable-soft-delete.sh PROJECT.md CLAUDE.md
git commit -m "docs: GCS versioning decision + soft-delete enablement script"
```

---

## Cross-cutting: interface contract updates

### Task X1: Update PROJECT.md §Interface Contracts

**Files:**
- Modify: `PROJECT.md`

- [ ] **Step 1: Add response shapes for all new endpoints**

For each:
- `GET/POST/DELETE /api/repos/{o}/{r}/collaborators[/{username}]`
- `GET/POST/PUT/DELETE /api/repos/{o}/{r}/branch-protection[/{ruleId}]`
- `GET /api/repos/{o}/{r}/codeowners?path=...`
- `POST/GET /api/repos/{o}/{r}/pulls/{n}/reviews`

Document the request body, response shape, status codes, and auth requirement.

- [ ] **Step 2: Add the PR doc field**

Note that `PullRequest` responses now include `requestedReviewers: string[]`.

- [ ] **Step 3: Commit**

```bash
git add PROJECT.md
git commit -m "docs: REST contracts for collaborators, branch protection, codeowners, reviews"
```

---

## E2E (optional but recommended)

### Task Z1: Extend `tests/test_e2e.go`

**Files:**
- Modify: `tests/test_e2e.go`

- [ ] **Step 1: Add scenarios**

1. Force-push to protected branch → expect non-zero exit, error message visible.
2. Direct push to protected branch by non-allowlisted user → rejected.
3. Direct push by allowlisted collaborator → succeeds.
4. PR merge blocked by missing approval → 403.
5. PR merge blocked by missing codeowner approval → 403.
6. PR merge succeeds after codeowner approves → 200.

Reuse existing E2E utilities for repo creation, PAT generation, push helpers.

- [ ] **Step 2: Run E2E**

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8084
go run tests/test_e2e.go -url http://localhost:8080 -emulator=true -project git-bucket-79382
```

- [ ] **Step 3: Commit**

```bash
git add tests/test_e2e.go
git commit -m "test(e2e): branch protection + codeowner merge gating scenarios"
```

---

## Notes for the team lead

- **Parallelism:** Workstream A and E are fully independent and can land at any time. B should land before C and D's merge integration. C and D parser work can proceed in parallel once B's auth helper is in.
- **Merge order:** A or E first (trivial), then B, then C and D parser/PR-open in parallel, then D merge integration, then E2E.
- **Frontend changes**: B, C, D each touch `frontend/src/pages/Repository.jsx`. To avoid merge conflicts, coordinate so one frontend agent owns this file or workstreams rebase sequentially.
- **CODEOWNERS matcher edge cases**: if the simplified matcher in D2 fails any real test case, fall back to `github.com/bmatcuk/doublestar/v4` — instructions inline.
- **Receive-pack rejection rollback** (Task C6): the materialized working tree is ephemeral; rejection just skips the sync-back to GCS/Firestore. Confirm no partial state is left behind by checking that a rejected push followed by a successful push from another client produces correct final state.
