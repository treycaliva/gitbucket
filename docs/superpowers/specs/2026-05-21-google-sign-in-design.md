# Google Sign-In — Design

**Status:** Approved (pending written-spec review)
**Date:** 2026-05-21
**Related future work:** `2026-05-21-totp-mfa-design.md` (separate spec — TOTP MFA, to be written next)

## Goal

Let users sign in with their Google account in addition to the existing email/password flow. The Google provider is already enabled in the Firebase console for `git-bucket-79382`.

## Non-Goals

- Multi-factor authentication (separate spec).
- Linking an existing email/password account to a Google identity, or vice versa. (Firebase will treat a Google sign-in whose email matches an existing email/password user as a distinct account unless we add explicit account linking. We are deferring that.)
- Other federated providers (GitHub, GitLab, Microsoft, etc.).
- Dev mode (`DEV_MODE=true`) support. The Google button is hidden in dev mode; mock auth continues to work as today.

## Background

- Frontend already uses `firebase/auth` (v12). `authService.js` initializes the SDK from `/api/config` and exposes `login`, `signup`, `logout`, `getToken`.
- The Go backend's web auth middleware (`internal/auth/auth.go`) verifies any Firebase ID token via the Admin SDK. A token minted from a Google credential is the same shape as one minted from email/password. **No backend changes are required for authentication.**
- The application requires a `username` to address a user in URLs (`/r/{owner}/{repo}`). Username is set via `POST /api/user/username` after the Firebase user exists. The username regex `^[a-zA-Z0-9-]{3,20}$` is enforced server-side in `internal/api/api.go` and mirrored client-side in `Login.jsx`.

## User flows

### First-time Google sign-in (new user)

1. User clicks **Continue with Google** on the login page.
2. `signInWithPopup` opens Google's consent screen. User picks an account.
3. Firebase mints an ID token. `onAuthStateChanged` fires, `authService` calls `GET /api/user/me`.
4. `/api/user/me` returns a profile with `username: null` (or 404 — see "Backend contract" below) because the user has never registered.
5. The login page detects "authenticated but no username" and renders a **Choose your username** step in place of the email form. This step reuses the existing username input and the same client-side regex.
6. User submits a username → `POST /api/user/username` with the Firebase token → on success, navigate to dashboard (or `currentNavigation.page` if set, matching current behavior).
7. If `POST /api/user/username` fails (e.g. username taken), show the server error inline and let the user pick another. The user remains signed in to Firebase — they only need to pick a different username.

### Returning Google user

1. Click **Continue with Google** → popup → token → `onAuthStateChanged`.
2. `/api/user/me` returns the existing username → `authService.currentUser.username` is set → navigate to dashboard. No modal shown.

### Edge case: user closes the popup or denies consent

`signInWithPopup` rejects with `auth/popup-closed-by-user` or similar. Show a non-fatal error in the existing error slot ("Sign-in cancelled.") and let the user try again.

### Edge case: Firebase auth succeeds but `/api/user/me` fails (network)

`onAuthStateChanged` already sets `currentUser` with `username: null` in this case (existing behavior in `authService.js:60-69`). The username-pick step will render. If the user submits a username and `POST /api/user/username` discovers the account *was* already registered (server-side conflict), the server returns an error and the user retries. This is a rare network-flake case; not worth special handling.

## Architecture

### `frontend/src/authService.js`

Add one new method:

```js
async loginWithGoogle() {
  await this.init();
  if (this.devMode) {
    throw new Error('Google sign-in is not available in dev mode.');
  }
  const provider = new GoogleAuthProvider();
  const cred = await signInWithPopup(this.auth, provider);
  // onAuthStateChanged handles profile fetch and listener notification.
  // Return the credential so the caller can read fbUser.uid if needed.
  return cred.user;
}
```

No changes to existing methods. The existing `onAuthStateChanged` handler already fetches `/api/user/me` and sets `username: null` when the profile lookup returns non-OK — that is exactly what we need for first-time Google users.

Import additions: `GoogleAuthProvider`, `signInWithPopup` from `firebase/auth`.

### `frontend/src/pages/Login.jsx`

Add a third UI state to the existing two (sign-in form, sign-up form):

- **State A — choosing method:** existing email/password form, plus a new **Continue with Google** button rendered above the form with a horizontal "or" divider below it. Hidden when `authService.getConfig()?.devMode` is true.
- **State B — picking a username after Google sign-in:** rendered when `authService.currentUser` exists and `currentUser.username` is null. This state shows the username input (reusing the existing styled component) and a single **Continue** button. No email/password fields. A small "Signed in as {email}" line above the input. A "Cancel" link calls `authService.logout()` and returns to State A.

State B is triggered by subscribing to `authService.onAuthStateChanged` in `Login.jsx` and inspecting whether `user && !user.username`.

The existing post-submit navigation logic (`currentNavigation.page` vs `dashboard`) is reused for both the Google flow and the username-pick step.

### Backend contract

`GET /api/user/me` is called with a valid Firebase token but the user has no Firestore profile yet. Two reasonable behaviors:

- Return 200 with `{username: null}` (or an empty object).
- Return 404.

The existing client code in `authService.js:46-69` already handles non-OK responses by setting `username: null`, so either works without changes. **We will not change the backend.** Whichever behavior `/api/user/me` currently exhibits is what the client will see. (If we discover during implementation that the endpoint 500s instead of 404s on missing profiles, that is a separate bug to fix and not in scope here.)

`POST /api/user/username` already accepts a Firebase token and creates the Firestore mapping. No changes.

## Data flow (first-time Google user)

```
[Login page] --click--> authService.loginWithGoogle()
                          |
                          v
                   signInWithPopup(Google)
                          |
                          v
                   Firebase ID token minted
                          |
                          v
                   onAuthStateChanged fires
                          |
                          v
                   GET /api/user/me  -->  {username: null} or 404
                          |
                          v
                   authService.currentUser = {uid, email, username: null}
                          |
                          v
                   Login.jsx observes user && !username --> render State B
                          |
                          v
            user submits username --> POST /api/user/username
                          |
                          v
                   200 OK --> currentUser.username set --> onNavigate(dashboard)
```

## Testing

- Manual: sign in with a Google account that has never used GitBucket; verify the username modal appears, pick a name, confirm the dashboard loads and the new user appears in Firestore at the expected key.
- Manual: sign out, sign in again with the same Google account; verify the username modal is skipped and the dashboard loads directly.
- Manual: trigger a username collision (sign in with Google as user B, attempt to pick a name already owned by user A) and confirm the error renders and the user can retry.
- Manual: close the Google popup mid-flow; confirm a friendly error appears and the page is usable.
- Manual: in `DEV_MODE=true`, verify the Google button is not rendered and the existing mock flow is unaffected.
- No new automated tests. The existing E2E suite does not exercise Firebase popup flows (which require a real browser and Google account) and adding that infrastructure is out of scope.

## Open questions

None. Recovery codes, account linking, and MFA are explicitly deferred to later specs.

## Risks

- **Account divergence.** If `alice@gmail.com` previously signed up via email/password and later clicks "Continue with Google" with the same Google account, Firebase will create a *second* account (different `uid`) unless we add account linking. The username-pick step will surface this as a collision, which is graceful but confusing. Acceptable for v1; account linking can be added later if it comes up in practice.
- **Popup blockers.** Some browsers block `signInWithPopup`. Firebase's `signInWithRedirect` is the fallback but changes the page navigation lifecycle. Acceptable for v1; we can swap to redirect later if popup blocking is a real issue.
