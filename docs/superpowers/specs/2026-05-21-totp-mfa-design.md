# TOTP Multi-Factor Authentication — Design

**Status:** Draft (pending user review)
**Date:** 2026-05-21
**Predecessor:** `2026-05-21-google-sign-in-design.md` (Google sign-in shipped first)

## Goal

Let users enroll a Time-based One-Time Password (TOTP) second factor (Google Authenticator, 1Password, Authy, etc.) on their GitBucket account, and challenge them for that code when they sign in with email and password. The Firebase project (`git-bucket-79382`) is already on Identity Platform (`"subtype": "IDENTITY_PLATFORM"`) with `mfa.state: "DISABLED"` — part of this work flips that state on.

## Non-Goals

- **SMS second factor.** Explicitly rejected during brainstorming in favor of TOTP. No phone numbers collected, no reCAPTCHA, no SMS region policy.
- **Recovery codes.** Punted to v2. A user who loses their TOTP device cannot self-recover and will require admin intervention (see "Operational limitation" below).
- **Federated provider MFA.** Firebase does not challenge users who sign in via federated providers (Google) with the second factor at sign-in — by design. We do not work around this. A user who has both Google sign-in and TOTP enrolled can sign in via Google without entering a TOTP code; this is a documented Firebase behavior, not a bug to fix here.
- **Step-up auth.** No "re-prompt for MFA before sensitive operations." MFA is enforced at the sign-in boundary only.
- **Backend enforcement.** No backend code changes for sign-in enforcement. Firebase will not issue an ID token until MFA is satisfied, so the existing token-verification middleware (`internal/auth/auth.go`) inherits MFA protection for free.
- **Multiple TOTP factors per user.** A user enrolls one TOTP factor. To rotate the device, they must unenroll and re-enroll. Firebase technically supports multiple factors of the same type, but we surface a single-factor UI for simplicity.

## Background

- The project is on Identity Platform (confirmed via `identitytoolkit.googleapis.com/admin/v2/projects/git-bucket-79382/config`).
- `mfa: { state: "DISABLED" }` today. This spec includes a one-time config update to enable MFA with TOTP as the only provider.
- The frontend uses Firebase JS SDK v12 — TOTP support is available via `TotpMultiFactorGenerator`, `multiFactor()`, and `getMultiFactorResolver()`.
- No new npm dependencies are required: Firebase's `generateSecret` returns a `qrCodeUrl` (`otpauth://`) which we can render to QR via a tiny well-known package (~20KB). We'll evaluate `qrcode` (npm) during implementation.
- The Go backend's Firebase Admin SDK can also unenroll factors from a user (used for admin recovery) but we do not build an admin endpoint in this spec.

## User flows

### Enrollment (a signed-in user adds TOTP)

1. User navigates to **Security** in the account menu and clicks **Set up authenticator**.
2. The page calls `multiFactor(user).getSession()` to obtain an MFA session.
3. The page calls `TotpMultiFactorGenerator.generateSecret(session)`. The returned `TotpSecret` exposes:
   - `secretKey` — base32 string to display as a fallback for typing into an authenticator app.
   - `generateQrCodeUrl(accountName, issuer)` — `otpauth://` URL suitable for QR encoding. We pass the user's email as `accountName` and `"GitBucket"` as `issuer`.
4. The page renders the QR code and the secret string. Below it, a 6-digit code input.
5. User scans the QR (or types the secret), enters the 6-digit code from their authenticator app, clicks **Verify and enroll**.
6. The page calls `TotpMultiFactorGenerator.assertionForEnrollment(totpSecret, verificationCode)` → `multiFactor(user).enroll(assertion, "Authenticator app")`.
7. On success the page shows "Two-factor authentication is on" with an **Unenroll** button.

**Re-auth requirement.** Firebase requires recent authentication to enroll a factor. If `enroll()` throws `auth/requires-recent-login`, the page logs the user out with a friendly message: "Please sign in again before enrolling two-factor authentication." (We do not implement an in-place re-auth modal in v1 — sign-out + sign-back-in is acceptable, since enrollment is a one-time setup action.)

### Sign-in challenge (an enrolled user signs in with email/password)

1. User submits the email/password form (`handleSubmit` in `Login.jsx`).
2. `signInWithEmailAndPassword` throws an error with `code === 'auth/multi-factor-auth-required'`.
3. The page calls `getMultiFactorResolver(auth, error)`. The resolver exposes `hints` (one entry — the enrolled TOTP factor) and `session`.
4. The page swaps to a **Verify your authenticator** UI with a 6-digit code input.
5. User enters the code. The page calls `TotpMultiFactorGenerator.assertionForSignIn(resolver.hints[0].uid, code)` → `resolver.resolveSignIn(assertion)`.
6. On success, `resolveSignIn` returns a `UserCredential` and Firebase's `onAuthStateChanged` fires normally. The existing navigation effect (added in the Google sign-in spec) handles routing.
7. On failure (wrong code, expired code), surface a retryable inline error. No lockout — Firebase enforces its own throttling.

### Unenrollment (a signed-in user removes TOTP)

1. User clicks **Unenroll** on the Security page.
2. A confirmation modal explains: "Removing two-factor authentication makes your account less secure. You'll only need your password to sign in."
3. On confirm, the page calls `multiFactor(user).unenroll(factorInfo)` where `factorInfo` is the entry from `multiFactor(user).enrolledFactors`.
4. UI returns to the "Set up authenticator" state.

Same `auth/requires-recent-login` handling as enrollment.

### Federated (Google) user enrolls TOTP — what happens?

Firebase allows it. The TOTP secret is added to the user's Firebase profile and `multiFactor(user).enrolledFactors` contains the entry. **However, the factor is never challenged at sign-in for Google flows**, because Firebase trusts the federated IdP. We disclose this on the Security page with a notice: *"Two-factor authentication only protects email/password sign-in. If you also use Google to sign in, that path is not affected."* This is honest and prevents the user from believing they have stronger protection than they do.

### Edge case: enrollment race

If the user enrolls TOTP in one browser tab and then attempts to sign in via email/password in another tab using a stale token, Firebase challenges them at the next sign-in. No special handling needed.

## Architecture

### Frontend changes

#### `frontend/src/pages/Security.jsx` (new)

A new account-settings page rendering one of three states based on `multiFactor(user).enrolledFactors`:

- **Empty state** — "Set up authenticator" button. Shows the federated-provider disclosure paragraph.
- **Enrollment in progress** — QR code (rendered via `qrcode` npm package, drawn into a `<canvas>`), the base32 secret in a copyable text field, and a 6-digit code input.
- **Enrolled state** — Factor name + enrolled date, "Unenroll" button.

The page reads `multiFactor(auth.currentUser)` directly from the Firebase SDK; no backend endpoint is needed for the enrolled-factors list.

#### `frontend/src/pages/Login.jsx` (modified)

Extend `handleSubmit`'s catch block to handle `auth/multi-factor-auth-required`:

```js
} catch (err) {
  if (err.code === 'auth/multi-factor-auth-required') {
    const resolver = getMultiFactorResolver(auth, err);
    setMfaResolver(resolver);
    setMfaChallenge(true);
    // ...does NOT setError; keeps loading false to allow code entry
    return;
  }
  // existing error handling
}
```

Add a fourth UI state to `Login.jsx` (in addition to sign-in form, sign-up form, and the Google username-pick form): **MFA challenge**. Renders when `mfaChallenge === true`:

- 6-digit code input.
- "Verify" submit button calling `TotpMultiFactorGenerator.assertionForSignIn(resolver.hints[0].uid, code)` + `resolver.resolveSignIn(assertion)`.
- "Cancel" link clears the MFA state and returns to the sign-in form. (Firebase has already authenticated the password at this point but has not minted a token, so cancelling is safe — no session exists.)

The MFA challenge UI is mutually exclusive with the sign-up, Google username-pick, and email/password forms (so the same `&&`-wrapping pattern from the Google sign-in spec extends naturally).

#### `frontend/src/authService.js` (modified)

Export `auth` (the Firebase Auth instance) and `getMultiFactorResolver` so `Login.jsx` and `Security.jsx` can build resolvers and read `multiFactor(currentUser)`. Today `authService` keeps `this.auth` private to the class; we'll add a small `getAuth()` accessor (or simply make `auth` a class field readable from outside) rather than building a wrapper around every MFA API call. The wrapper-per-call alternative was considered and rejected as over-abstraction.

No changes to `loginWithGoogle()` (Google flow bypasses MFA).
No changes to `getToken()` (Firebase only mints tokens after MFA is satisfied).

#### Navigation

Add a **Security** nav entry alongside the existing **Access Tokens** entry. Routed at `/security`. Standard pattern in `App.jsx`.

### Backend changes

**None for sign-in enforcement.** Firebase does not mint an ID token until MFA is satisfied, so the Go middleware in `internal/auth/auth.go` is automatically MFA-protected.

**Optional follow-up (not in this spec):** an admin endpoint to clear a user's MFA enrollment via the Firebase Admin SDK, used when a user loses their device. Deferred until the operational need actually arises — until then, recovery is manual (a developer with project access runs a one-off script).

### Identity Platform configuration

A one-time admin API call enables MFA with TOTP as the only provider. This is part of the implementation work but is a config change, not application code:

```
PATCH https://identitytoolkit.googleapis.com/admin/v2/projects/git-bucket-79382/config?updateMask=mfa
{
  "mfa": {
    "state": "ENABLED",
    "providerConfigs": [
      { "state": "ENABLED", "totpProviderConfig": { "adjacentIntervals": 5 } }
    ]
  }
}
```

`adjacentIntervals: 5` allows codes from the 5 prior + 5 next 30-second windows (Google's default), accommodating modest device clock drift.

This call must be made with credentials that have the Identity Platform Admin role on the project. We do this as a separate ops step before the user-facing UI ships; otherwise enrollment calls would fail with `auth/operation-not-allowed`.

### Dev mode (`DEV_MODE=true`)

The Security page is hidden. The MFA sign-in challenge code path in `Login.jsx` is unreachable because mock auth never throws `auth/multi-factor-auth-required`. Same approach as Google sign-in — MFA is a real-Firebase concern.

## Data flow (enrollment, happy path)

```
[Security page] --click "Set up"--> multiFactor(user).getSession()
                                       |
                                       v
                              TotpMultiFactorGenerator.generateSecret(session)
                                       |
                                       v
                              {secretKey, qrCodeUrl}
                                       |
                                       v
                              Render QR (qrcode lib) + secret + code input
                                       |
                                       v
            user enters code --> TotpMultiFactorGenerator.assertionForEnrollment(secret, code)
                                       |
                                       v
                              multiFactor(user).enroll(assertion, "Authenticator app")
                                       |
                                       v
                              UI swaps to "enrolled" state
```

## Data flow (sign-in challenge)

```
[Login page] --submit email/password--> signInWithEmailAndPassword()
                                          |
                                          v
                                 throws {code: 'auth/multi-factor-auth-required'}
                                          |
                                          v
                                 getMultiFactorResolver(auth, err) --> resolver
                                          |
                                          v
                                 UI swaps to MFA challenge
                                          |
                user enters 6-digit code  v
                                 TotpMultiFactorGenerator.assertionForSignIn(hints[0].uid, code)
                                          |
                                          v
                                 resolver.resolveSignIn(assertion) --> UserCredential
                                          |
                                          v
                                 onAuthStateChanged fires --> existing nav effect routes to dashboard
```

## Testing

All manual; the existing E2E suite does not exercise Firebase flows.

**Enrollment:**
- [ ] Signed-in user navigates to Security → sees the empty state and federated-provider disclosure.
- [ ] Clicks **Set up** → QR + secret appear.
- [ ] Scans with Google Authenticator (or types secret into 1Password) → enters code → "Two-factor authentication is on" appears.
- [ ] Refresh page → still shows enrolled state.
- [ ] Recently-authenticated requirement: sign in, wait >5 min, attempt enroll → see "Please sign in again" message and get logged out.

**Sign-in challenge:**
- [ ] Sign out, sign in with the email/password of the enrolled user → MFA challenge UI appears.
- [ ] Enter a valid code → land on dashboard.
- [ ] Enter a wrong code → inline error, retry succeeds.
- [ ] Enter an expired code (wait >90s) → inline error, retry with fresh code succeeds.
- [ ] Cancel from challenge → returns to sign-in form; no session exists.

**Federated bypass:**
- [ ] User with TOTP enrolled signs in with Google → goes straight to dashboard, no challenge. This is expected; the disclosure on the Security page sets the right expectation.

**Unenrollment:**
- [ ] Click Unenroll → confirmation → factor removed → Security page returns to empty state.
- [ ] Sign in with email/password → no MFA challenge (factor is gone).

**Dev mode:**
- [ ] `DEV_MODE=true`: Security nav entry is hidden; mock email/password flow unchanged.

## Operational limitation: TOTP device loss

Without recovery codes (deferred per the brainstorming decision), a user who loses their authenticator device cannot sign in via email/password and cannot self-recover. Resolution:

1. User contacts an admin (out-of-band — email).
2. Admin verifies identity and runs a small Firebase Admin SDK script:
   ```js
   await admin.auth().updateUser(uid, { multiFactor: { enrolledFactors: [] } });
   ```
3. User signs in (no MFA challenge), navigates to Security, re-enrolls.

This is acceptable for an internal-tools deployment but would need to be productized (recovery codes, admin UI, audit log) before broader use. We will revisit when an actual device-loss incident occurs.

## Risks

- **Federated bypass surprise.** A user might think enrolling TOTP protects all sign-in paths. The disclosure on the Security page mitigates this; if it becomes a real point of confusion, we can add an explicit setting "Require MFA for email/password only" (which is the only enforceable policy Firebase offers without blocking Google sign-in entirely).
- **Clock drift.** The default `adjacentIntervals: 5` is generous (±2.5 minutes). If users still hit code-rejection, the next step is to instruct them to resync their device clock; Firebase has no per-user override.
- **QR code library footprint.** `qrcode` npm is ~20KB minified. Acceptable. Alternative: render QR via the Google Charts API (no dep) — rejected for privacy (the secret would leak through Google's URL).
- **Identity Platform billing.** GCIP pricing kicks in above 50 free MAU. Confirm with finance owner before shipping if MAU is approaching that threshold. Not a code concern.

## Open questions

None known. The brainstorming session resolved scope, provider choice, and recovery-code deferral. The user has confirmed all three.
