# Google Sign-In Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "Continue with Google" to the GitBucket login page so users can authenticate via Firebase's Google provider, with a username-pick step for first-time Google users.

**Architecture:** Frontend-only change. `authService.js` gains a `loginWithGoogle()` method that calls `signInWithPopup` with `GoogleAuthProvider`; the existing `onAuthStateChanged` handler already fetches `/api/user/me` and surfaces `username: null` when the backend returns 404 for a brand-new user. `Login.jsx` adds the Google button above the email form and a new in-page "Choose your username" step that renders when `authService.currentUser` exists with a null username.

**Tech Stack:** React 19, Firebase JS SDK v12 (`firebase/auth`), Vite. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-21-google-sign-in-design.md`

---

## File Structure

**Modify:**
- `frontend/src/authService.js` — add `loginWithGoogle()`. Imports `GoogleAuthProvider` and `signInWithPopup` from `firebase/auth`.
- `frontend/src/pages/Login.jsx` — add Google button, an "or" divider, and a third UI state ("pick a username") that activates when an authenticated user has no username yet.

**No other files change.** No backend changes. No new tests files — the existing repo has no frontend test runner configured (verify with `cat frontend/package.json` — no `test` script, no Jest/Vitest dep), so we test manually per the spec's "Testing" section.

---

### Task 1: Add `loginWithGoogle()` to authService

**Files:**
- Modify: `frontend/src/authService.js` (imports at top of file; new method at the end of the class, after `getConfig()`)

- [ ] **Step 1: Update the firebase/auth import**

Open `frontend/src/authService.js`. The current import block (lines 2-8) is:

```js
import {
  getAuth,
  signInWithEmailAndPassword as fbSignIn,
  createUserWithEmailAndPassword as fbSignUp,
  signOut as fbSignOut,
  onAuthStateChanged as fbOnAuthStateChanged
} from 'firebase/auth';
```

Replace it with:

```js
import {
  getAuth,
  signInWithEmailAndPassword as fbSignIn,
  createUserWithEmailAndPassword as fbSignUp,
  signOut as fbSignOut,
  onAuthStateChanged as fbOnAuthStateChanged,
  GoogleAuthProvider,
  signInWithPopup
} from 'firebase/auth';
```

- [ ] **Step 2: Add `loginWithGoogle()` method**

Find the `getConfig()` method (around line 232):

```js
  getConfig() {
    return this.config;
  }
}
```

Insert the new method *before* `getConfig()` so the file ends with:

```js
  async loginWithGoogle() {
    await this.init();

    if (this.devMode) {
      throw new Error('Google sign-in is not available in dev mode.');
    }

    const provider = new GoogleAuthProvider();
    const cred = await signInWithPopup(this.auth, provider);
    // onAuthStateChanged will fire, fetch /api/user/me, and set this.currentUser.
    // If the user is brand new, /api/user/me returns 404 and currentUser.username will be null.
    return cred.user;
  }

  getConfig() {
    return this.config;
  }
}
```

- [ ] **Step 3: Manual verification — build succeeds**

Run from the repo root:

```bash
cd frontend && npm run build
```

Expected: build completes with no errors. (Bundle size will increase slightly — Google provider code is tree-shaken in from `firebase/auth`.)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/authService.js
git commit -m "feat(auth): add loginWithGoogle to authService"
```

---

### Task 2: Add the "Continue with Google" button to Login.jsx

This task adds the button and the "or" divider but does NOT yet handle the post-sign-in username step (that's Task 3). At the end of this task, a returning Google user (one who already has a username in Firestore) can sign in successfully; a first-time Google user will be signed into Firebase but stuck because the login page won't know what to do with `username: null`. Task 3 closes that gap.

**Files:**
- Modify: `frontend/src/pages/Login.jsx`

- [ ] **Step 1: Update the lucide-react import to include the Google icon stand-in**

`lucide-react` does not ship a Google logo (trademark). We'll use an inline SVG for the Google "G". The existing import on line 3 is:

```js
import { Database, Lock, Mail, User } from 'lucide-react';
```

Leave it as-is for now — we'll add the SVG inline in the JSX in Step 3.

- [ ] **Step 2: Add a `handleGoogleSignIn` handler inside the `Login` component**

Locate the `handleSubmit` function in `Login.jsx` (line 13). Immediately *after* the closing `};` of `handleSubmit` (around line 38), insert:

```jsx
  const handleGoogleSignIn = async () => {
    setError('');
    setLoading(true);
    try {
      await authService.loginWithGoogle();
      // Navigation is handled in Task 3 once we know whether the user needs to pick a username.
      // For now (returning users only), if currentUser has a username, navigate.
      const user = authService.currentUser;
      if (user && user.username) {
        if (currentNavigation && currentNavigation.page !== 'dashboard') {
          onNavigate(currentNavigation.page, currentNavigation.params, true);
        } else {
          onNavigate('dashboard');
        }
      }
    } catch (err) {
      console.error(err);
      if (err.code === 'auth/popup-closed-by-user' || err.code === 'auth/cancelled-popup-request') {
        setError('Sign-in cancelled.');
      } else {
        setError(err.message || 'Google sign-in failed.');
      }
    } finally {
      setLoading(false);
    }
  };

  const devMode = authService.getConfig()?.devMode === true;
```

Note: `authService.currentUser` is set synchronously inside `signInWithPopup` for returning users only *after* `onAuthStateChanged` fires and the `/api/user/me` fetch resolves. In practice this is fast enough that by the time `signInWithPopup` resolves, the listener has run. But this is a race we'll fix properly in Task 3 by listening to `onAuthStateChanged` from the component itself.

- [ ] **Step 3: Add the Google button and divider above the form**

Find the line `<form onSubmit={handleSubmit}>` (around line 89). Immediately *before* that line, insert the following JSX block:

```jsx
        {!devMode && (
          <>
            <button
              type="button"
              onClick={handleGoogleSignIn}
              disabled={loading}
              style={{
                width: '100%',
                padding: '0.75rem',
                background: '#ffffff',
                color: '#1f2937',
                border: '1px solid rgba(255, 255, 255, 0.1)',
                borderRadius: '8px',
                fontWeight: 600,
                fontSize: '0.95rem',
                cursor: loading ? 'not-allowed' : 'pointer',
                opacity: loading ? 0.6 : 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: '0.6rem',
                marginBottom: '1.25rem'
              }}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true">
                <path fill="#4285F4" d="M17.64 9.2c0-.637-.057-1.251-.164-1.84H9v3.481h4.844a4.14 4.14 0 0 1-1.796 2.716v2.259h2.908c1.702-1.567 2.684-3.875 2.684-6.615z"/>
                <path fill="#34A853" d="M9 18c2.43 0 4.467-.806 5.956-2.184l-2.908-2.259c-.806.54-1.837.86-3.048.86-2.344 0-4.328-1.584-5.036-3.711H.957v2.332A8.997 8.997 0 0 0 9 18z"/>
                <path fill="#FBBC05" d="M3.964 10.706A5.41 5.41 0 0 1 3.682 9c0-.593.102-1.17.282-1.706V4.962H.957A8.997 8.997 0 0 0 0 9c0 1.452.348 2.827.957 4.038l3.007-2.332z"/>
                <path fill="#EA4335" d="M9 3.58c1.321 0 2.508.454 3.44 1.345l2.582-2.58C13.463.891 11.426 0 9 0A8.997 8.997 0 0 0 .957 4.962L3.964 7.294C4.672 5.167 6.656 3.58 9 3.58z"/>
              </svg>
              Continue with Google
            </button>

            <div style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.75rem',
              marginBottom: '1.25rem',
              color: '#64748b',
              fontSize: '0.8rem'
            }}>
              <div style={{ flex: 1, height: '1px', background: 'rgba(255, 255, 255, 0.08)' }} />
              <span>or</span>
              <div style={{ flex: 1, height: '1px', background: 'rgba(255, 255, 255, 0.08)' }} />
            </div>
          </>
        )}

```

- [ ] **Step 4: Manual verification — build succeeds**

```bash
cd frontend && npm run build
```

Expected: build completes with no errors.

- [ ] **Step 5: Manual verification — button renders**

Start the frontend dev server *and* the Go backend (the dev server proxies `/api/*` to the backend). From the repo root:

```bash
# Terminal 1 — backend (uses .env)
go run main.go

# Terminal 2 — frontend
cd frontend && npm run dev
```

Open the Vite URL (default `http://localhost:5173`) in a browser. Confirm:
- The "Continue with Google" button renders above the email field on the login page.
- A horizontal "or" divider sits between the button and the email form.
- In `DEV_MODE=true` (the committed `.env`), the button **does not render**. To temporarily verify a real Google flow you'd need to set `DEV_MODE=false` and have valid Firebase config; this is left to the user.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/Login.jsx
git commit -m "feat(auth): add Continue with Google button to login page"
```

---

### Task 3: Add the post-sign-in "Choose your username" step

This task makes `Login.jsx` react to `authService.onAuthStateChanged`. When a signed-in user has no username (the first-time Google case), the page swaps the email/password form for a username-pick form. Submitting calls `POST /api/user/username`, then navigates.

**Files:**
- Modify: `frontend/src/pages/Login.jsx`

- [ ] **Step 1: Add `useEffect` and the post-sign-in state**

The current `import` line at the top of `Login.jsx` is:

```jsx
import { useState } from 'react';
```

Replace with:

```jsx
import { useState, useEffect } from 'react';
```

Find the `useState` block at the top of the `Login` component (lines 6-11):

```jsx
  const [isSignUp, setIsSignUp] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [username, setUsername] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
```

Add two new state variables immediately after, so the block becomes:

```jsx
  const [isSignUp, setIsSignUp] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [username, setUsername] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [pendingUser, setPendingUser] = useState(null); // {uid, email} when signed in but no username
  const [pickingUsername, setPickingUsername] = useState(false);
```

- [ ] **Step 2: Subscribe to auth state changes**

Immediately after the `useState` block from Step 1, add:

```jsx
  useEffect(() => {
    const unsubscribe = authService.onAuthStateChanged((user) => {
      if (user && !user.username) {
        setPendingUser({ uid: user.uid, email: user.email });
        setPickingUsername(true);
      } else {
        setPendingUser(null);
        setPickingUsername(false);
      }
    });
    return unsubscribe;
  }, []);
```

- [ ] **Step 3: Simplify `handleGoogleSignIn` — drop the in-line navigation**

In Task 2 Step 2 we navigated manually after `loginWithGoogle()`. The `useEffect` from Step 2 above is now the single source of truth for "signed in, has username → navigate". Replace the `handleGoogleSignIn` function (the one added in Task 2 Step 2) with:

```jsx
  const handleGoogleSignIn = async () => {
    setError('');
    setLoading(true);
    try {
      await authService.loginWithGoogle();
      // The useEffect above handles both branches:
      //  - has username → falls through to the navigation effect added in Step 4
      //  - no username → renders the username-pick UI
    } catch (err) {
      console.error(err);
      if (err.code === 'auth/popup-closed-by-user' || err.code === 'auth/cancelled-popup-request') {
        setError('Sign-in cancelled.');
      } else {
        setError(err.message || 'Google sign-in failed.');
      }
    } finally {
      setLoading(false);
    }
  };

  const devMode = authService.getConfig()?.devMode === true;
```

- [ ] **Step 4: Add the "navigate when user has a username" effect**

Immediately after the `useEffect` added in Step 2, add a second effect:

```jsx
  useEffect(() => {
    const unsubscribe = authService.onAuthStateChanged((user) => {
      if (user && user.username) {
        if (currentNavigation && currentNavigation.page !== 'dashboard') {
          onNavigate(currentNavigation.page, currentNavigation.params, true);
        } else {
          onNavigate('dashboard');
        }
      }
    });
    return unsubscribe;
  }, [onNavigate, currentNavigation]);
```

This effect intentionally duplicates the listener — the two effects could be merged into one, but keeping them separate makes the responsibilities legible and matches how the two states (`pendingUser` vs "ready to navigate") are mutually exclusive.

- [ ] **Step 5: Add the username-submission handler**

Immediately after `handleGoogleSignIn`, add:

```jsx
  const handlePickUsername = async (e) => {
    e.preventDefault();
    setError('');
    if (!username || !/^[a-zA-Z0-9-]{3,20}$/.test(username)) {
      setError('Username must be alphanumeric (with hyphens), 3-20 characters long.');
      return;
    }
    setLoading(true);
    try {
      const token = await authService.getToken();
      const res = await fetch('/api/user/username', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ username: username.toLowerCase() })
      });
      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || 'Failed to register username.');
      }
      // Force authService to refresh currentUser so its username field is populated,
      // which triggers the navigation effect in Step 4.
      authService.currentUser = {
        ...authService.currentUser,
        username: username.toLowerCase()
      };
      authService._notifyListeners();
    } catch (err) {
      console.error(err);
      setError(err.message || 'Failed to register username.');
    } finally {
      setLoading(false);
    }
  };

  const handleCancelPickUsername = async () => {
    setError('');
    await authService.logout();
    setUsername('');
    // useEffect from Step 2 will clear pendingUser/pickingUsername.
  };
```

- [ ] **Step 6: Render the username-pick UI conditionally**

Find the heading block (lines 49-72 in the original file — the `<div style={{ textAlign: 'center', marginBottom: '2rem' }}>` containing the icon, `<h1>`, and `<p>`).

Replace the `<h1>` and `<p>` contents to react to `pickingUsername`. The block becomes:

```jsx
        <div style={{ textAlign: 'center', marginBottom: '2rem' }}>
          <div style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: '60px',
            height: '60px',
            borderRadius: '16px',
            background: 'linear-gradient(135deg, rgba(56, 189, 248, 0.15) 0%, rgba(99, 102, 241, 0.15) 100%)',
            border: '1px solid rgba(56, 189, 248, 0.3)',
            color: '#38bdf8',
            marginBottom: '1rem'
          }}>
            <Database size={32} />
          </div>
          <h1 style={{ fontSize: '2rem', marginBottom: '0.5rem', fontWeight: 800 }}>
            {pickingUsername
              ? 'Choose a username'
              : isSignUp ? 'Create Account' : 'Welcome to GitBucket'}
          </h1>
          <p style={{ color: '#94a3b8', fontSize: '0.95rem' }}>
            {pickingUsername
              ? `Signed in as ${pendingUser?.email ?? ''}. Pick a username to finish setup.`
              : isSignUp
                ? 'Sign up to host repositories on GCS'
                : 'Sign in to access your git repositories'}
          </p>
        </div>
```

- [ ] **Step 7: Wrap the existing form + Google button in a conditional**

Locate the `{error && (...)}` block (lines 74-87) — leave it where it is; we want errors to render in either mode.

Then locate the Google button + divider block (added in Task 2 Step 3) and the `<form onSubmit={handleSubmit}>` block (around line 89). These two together should only render when `!pickingUsername`. Wrap them:

```jsx
        {!pickingUsername && (
          <>
            {!devMode && (
              <>
                {/* the Google button block from Task 2 Step 3 */}
                {/* the "or" divider from Task 2 Step 3 */}
              </>
            )}

            <form onSubmit={handleSubmit}>
              {/* existing form contents unchanged */}
            </form>

            <div style={{
              textAlign: 'center',
              marginTop: '1.5rem',
              fontSize: '0.9rem',
              color: '#94a3b8'
            }}>
              {/* existing "Already have an account? / Don't have an account?" toggle, unchanged */}
            </div>
          </>
        )}
```

Concretely: take everything from the start of `{!devMode && (` (Google button block) through the end of the bottom "Sign In / Sign Up" toggle `</div>`, and wrap it all in a single `{!pickingUsername && (<>...</>)}`.

- [ ] **Step 8: Add the username-pick form alongside**

Immediately after the `{!pickingUsername && (...)}` block from Step 7 (so it's a sibling, not nested), add:

```jsx
        {pickingUsername && (
          <form onSubmit={handlePickUsername}>
            <div className="form-group">
              <label className="form-label">Username</label>
              <div style={{ position: 'relative' }}>
                <User size={18} style={{
                  position: 'absolute',
                  left: '1rem',
                  top: '50%',
                  transform: 'translateY(-50%)',
                  color: '#64748b'
                }} />
                <input
                  type="text"
                  className="text-input"
                  style={{ paddingLeft: '2.75rem' }}
                  placeholder="e.g. trey-caliva"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  disabled={loading}
                  autoFocus
                  required
                />
              </div>
              <span style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.25rem', display: 'block' }}>
                Lowercase letters, numbers, and hyphens only (3-20 chars).
              </span>
            </div>

            <button
              type="submit"
              className="btn btn-primary"
              style={{ width: '100%', padding: '0.85rem' }}
              disabled={loading}
            >
              {loading ? (
                <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
              ) : (
                'Continue'
              )}
            </button>

            <div style={{
              textAlign: 'center',
              marginTop: '1.5rem',
              fontSize: '0.9rem',
              color: '#94a3b8'
            }}>
              <button
                type="button"
                className="btn-link"
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#38bdf8',
                  fontWeight: 600,
                  cursor: 'pointer',
                  padding: 0
                }}
                onClick={handleCancelPickUsername}
                disabled={loading}
              >
                Cancel
              </button>
            </div>
          </form>
        )}
```

- [ ] **Step 9: Manual verification — build succeeds**

```bash
cd frontend && npm run build
```

Expected: build completes with no errors.

- [ ] **Step 10: Manual verification — first-time Google sign-in flow**

Requires `DEV_MODE=false` and the Firebase project's Google provider enabled (already done per user). With the backend and Vite dev server running:

1. Open the login page in an incognito window (so no cached Google account).
2. Click **Continue with Google**, pick a Google account that has *never* signed in to this GitBucket instance.
3. Confirm:
   - The popup closes after consent.
   - The login page swaps to the "Choose a username" view, showing the Google email under the heading.
   - The email/password form and the Google button are hidden.
4. Type a valid username and submit. Confirm:
   - The dashboard loads.
   - Firestore (via console or emulator UI) shows a new document at `users/{firebase-uid}` with `username` and `email` fields.

- [ ] **Step 11: Manual verification — returning Google user**

5. Sign out, then click **Continue with Google** again with the same Google account.
6. Confirm: no username modal, dashboard loads directly.

- [ ] **Step 12: Manual verification — username collision**

7. Sign out. Sign in with a *different* Google account.
8. On the username-pick screen, enter the username you registered in Step 10.
9. Confirm: a server error renders inline ("username already taken" or similar — exact text comes from `RegisterUsername` in `internal/api/api.go`).
10. Enter a different valid username. Confirm: dashboard loads.

- [ ] **Step 13: Manual verification — popup cancellation**

11. Sign out. Click **Continue with Google**, then close the popup before picking an account.
12. Confirm: "Sign-in cancelled." appears in the error area; the login page remains usable.

- [ ] **Step 14: Manual verification — dev mode unaffected**

13. With `DEV_MODE=true` (committed `.env`), restart the backend, reload the page.
14. Confirm: the Google button is NOT rendered. The existing mock email/password flow still works end-to-end.

- [ ] **Step 15: Commit**

```bash
git add frontend/src/pages/Login.jsx
git commit -m "feat(auth): add username-pick step for first-time Google users"
```

---

## Self-Review Notes

- **Spec coverage:** Goal ✓ (Task 1+2+3), first-time flow ✓ (Task 3 Step 10), returning user ✓ (Step 11), popup-closed edge case ✓ (Step 13), backend-failure-during-username ✓ (Step 12), dev mode hidden ✓ (Step 14), no backend changes ✓.
- **Non-goals respected:** no MFA, no account linking, no other providers, no dev-mode Google mock.
- **Type consistency:** `authService.loginWithGoogle()` defined in Task 1 matches the call site in Task 2 Step 2 / Task 3 Step 3. `authService.currentUser` shape (`{uid, email, username}`) matches the existing code in `authService.js:51-55`. `onAuthStateChanged` callback signature matches the existing implementation.
- **No placeholders.** All code is concrete; manual-verification steps name exact UI states to check.
