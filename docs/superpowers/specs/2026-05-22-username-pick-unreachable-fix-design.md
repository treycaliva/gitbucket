# Fix: username-pick step unreachable after Google sign-in

**Status:** Draft (pending user review)
**Date:** 2026-05-22
**Type:** Bug fix
**Related:** `2026-05-21-google-sign-in-design.md` (the feature this fix completes)

## Bug

When a brand-new user signs in with Google, `Login.jsx`'s post-sign-in "Choose a username" step never renders. The user lands on the Dashboard with a Firebase auth account but no Firestore profile, leaving them in a half-broken state (most backend operations that require ownership lookup will fail until a username exists).

Reproduced in production by the project owner on 2026-05-21.

## Root cause

`frontend/src/App.jsx:224` decides which top-level view to render based on `if (!user)`. After `signInWithPopup` succeeds and `authService` populates `currentUser = {uid, email, username: null}`, the App's auth listener sets `user` to a non-null object. `App.jsx` immediately swaps from `Login` to `Dashboard` — but the username-pick UI lives inside `Login.jsx`, so it never gets a chance to render.

The Google sign-in spec (`2026-05-21-google-sign-in-design.md`) assumed `Login.jsx` would stay mounted long enough for its `useEffect` to detect `user.username === null` and swap to the pick-username state. That assumption was wrong; `App.jsx`'s routing wins first.

## Fix

Change `App.jsx:224` from:

```jsx
if (!user) return <Login onNavigate={navigate} currentNavigation={navigation} />;
```

to:

```jsx
if (!user || !user.username) return <Login onNavigate={navigate} currentNavigation={navigation} />;
```

That's the entire fix. The `Login.jsx` component's existing `useEffect` already sets `pickingUsername = true` when it observes a user with no username, and the existing UI renders correctly from there.

## Why this works

- **Existing Login.jsx logic is correct.** It subscribes to `authService.onAuthStateChanged` and reacts to `user && !user.username` by entering the pick-username state. No changes needed in `Login.jsx`.
- **The Cancel button in the pick-username form already calls `authService.logout()`**, which clears `currentUser` and returns control to App.jsx — same behavior as today, except now App.jsx routes back to `Login` (because `!user` becomes true) instead of staying on a half-signed-in Dashboard.
- **Page reload recovery works for existing broken users.** A user already in the broken state (signed in via Google, no Firestore doc) reloads the app: `authService.onAuthStateChanged` fetches `/api/user/me`, gets 404, sets `username: null`, App.jsx now routes to `Login`, the pick-username form renders, the user picks a name, `POST /api/user/username` creates the Firestore doc.
- **Existing flows are unaffected.** Email/password sign-up sets the username during the signup flow itself, so `user.username` is set immediately and the guard does not trigger. Returning Google users also have their username set immediately when `/api/user/me` returns 200. Only first-time Google users hit the new branch — exactly the intended case.

## Edge cases

- **A user types a deep URL while in the broken state** (e.g., `/some-repo/pulls/1`). `App.jsx` now overrides routing and shows `Login` until they pick a username. After picking, they land on the dashboard, not the deep URL. Acceptable — the existing `currentNavigation` plumbing in `Login.jsx` already navigates to the previously-intended page after sign-in, but that wiring is for sign-in via `handleSubmit`, not the pick-username flow. Folding the deep-link redirect into pick-username is **out of scope** for this fix; if it comes up in practice we'll address it then.
- **MFA challenge mid-sign-in.** The MFA challenge UI lives in `Login.jsx` too. While the resolver is active, `user` is still null (Firebase has not minted an ID token), so the guard does not affect this flow. Confirmed by inspection.

## Non-goals

- No extraction of the username-pick UI into its own top-level component (Option 2 from brainstorming — deferred as YAGNI).
- No new tests (no frontend test runner exists in the repo; manual verification is the established pattern).
- No backend changes.
- No fix for the deep-link redirect edge case above.

## Testing

Manual:
- [ ] In an incognito window, click **Continue with Google** with a Google account that has never signed in to GitBucket. Expect the username-pick form to render (not the Dashboard). Pick a username, confirm landing on the Dashboard and a `users/{uid}` doc in Firestore.
- [ ] Verify the broken-account recovery: project owner (Trey) reloads `gitbucket.dev`, sees the username-pick form, picks a name, lands on the Dashboard.
- [ ] Existing email/password sign-up still works without showing the pick-username step (the form's own username field handles it).
- [ ] Returning Google users go straight to the Dashboard (no pick-username flash).
- [ ] MFA-enrolled email/password sign-in still shows the MFA challenge; the username guard is not in the way.

## Risk

Minimal. One-character logic change. Worst case: a regression hides the Dashboard for an authenticated user, which is immediately observable and one-line reversible.
