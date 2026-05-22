import { useState, useEffect } from 'react';
import { authService } from '../authService';
import { getMultiFactorResolver, TotpMultiFactorGenerator } from 'firebase/auth';
import { Database, Lock, Mail, User } from 'lucide-react';

const USERNAME_REGEX = /^[a-zA-Z0-9-]{3,20}$/;

export default function Login({ onNavigate, currentNavigation }) {
  const [isSignUp, setIsSignUp] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [username, setUsername] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [pendingUser, setPendingUser] = useState(null);
  const [pickingUsername, setPickingUsername] = useState(false);
  const [mfaResolver, setMfaResolver] = useState(null);
  const [mfaCode, setMfaCode] = useState('');

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

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      if (isSignUp) {
        if (!username || !USERNAME_REGEX.test(username)) {
          throw new Error('Username must be alphanumeric (with hyphens), 3-20 characters long.');
        }
        await authService.signup(email, password, username);
      } else {
        await authService.login(email, password);
      }
      // Navigation handled by useEffect subscribed to authService.onAuthStateChanged.
    } catch (err) {
      if (err.code === 'auth/multi-factor-auth-required') {
        try {
          const auth = authService.getAuthInstance();
          const resolver = getMultiFactorResolver(auth, err);
          setMfaResolver(resolver);
        } catch (resolveErr) {
          console.error(resolveErr);
          setError(resolveErr.message || 'Failed to start MFA challenge.');
        }
      } else {
        console.error(err);
        setError(err.message || 'Authentication failed. Please check your credentials.');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleGoogleSignIn = async () => {
    setError('');
    setLoading(true);
    try {
      await authService.loginWithGoogle();
      // useEffects above handle both branches: navigate if username exists, or render username-pick UI.
    } catch (err) {
      if (
        err.code === 'auth/popup-closed-by-user' ||
        err.code === 'auth/cancelled-popup-request' ||
        err.code === 'auth/popup-blocked'
      ) {
        setError('Sign-in was cancelled or blocked. Please allow pop-ups and try again.');
      } else {
        console.error(err);
        setError(err.message || 'Google sign-in failed.');
      }
    } finally {
      setLoading(false);
    }
  };

  const devMode = authService.getConfig()?.devMode === true;
  const mfaChallenging = mfaResolver !== null;

  const handlePickUsername = async (e) => {
    e.preventDefault();
    setError('');
    if (!username || !USERNAME_REGEX.test(username)) {
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
        const body = await res.text().catch(() => '');
        let msg = 'Failed to register username.';
        if (body) {
          try {
            const parsed = JSON.parse(body);
            msg = parsed.error || parsed.message || body;
          } catch {
            msg = body;
          }
        }
        throw new Error(msg);
      }
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
  };

  const handleMfaSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!/^\d{6}$/.test(mfaCode)) {
      setError('Enter the 6-digit code from your authenticator app.');
      return;
    }
    setLoading(true);
    try {
      const hint = mfaResolver.hints[0];
      const assertion = TotpMultiFactorGenerator.assertionForSignIn(hint.uid, mfaCode);
      await mfaResolver.resolveSignIn(assertion);
      // onAuthStateChanged will fire; the existing navigation effect handles routing.
      setMfaResolver(null);
      setMfaCode('');
    } catch (err) {
      console.error(err);
      if (err.code === 'auth/invalid-verification-code') {
        setError('That code did not match. Try again with a fresh code from your app.');
      } else {
        setError(err.message || 'Verification failed.');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleCancelMfa = () => {
    setMfaResolver(null);
    setMfaCode('');
    setError('');
  };

  return (
    <div style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      minHeight: '70vh',
      padding: '1rem'
    }}>
      <div className="glass-card modal-content" style={{ padding: '2.5rem' }}>
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
            {mfaChallenging
              ? 'Verify your identity'
              : pickingUsername
                ? 'Choose a username'
                : isSignUp ? 'Create Account' : 'Welcome to GitBucket'}
          </h1>
          <p style={{ color: '#94a3b8', fontSize: '0.95rem' }}>
            {mfaChallenging
              ? 'Two-factor authentication is required for this account.'
              : pickingUsername
                ? `Signed in as ${pendingUser?.email ?? ''}. Pick a username to finish setup.`
                : isSignUp
                  ? 'Sign up to host repositories on GCS'
                  : 'Sign in to access your git repositories'}
          </p>
        </div>

        {error && (
          <div style={{
            background: 'rgba(244, 63, 94, 0.12)',
            border: '1px solid rgba(244, 63, 94, 0.25)',
            color: '#fb7185',
            padding: '0.85rem 1rem',
            borderRadius: '8px',
            fontSize: '0.875rem',
            marginBottom: '1.5rem',
            lineHeight: 1.4
          }}>
            {error}
          </div>
        )}

        {!pickingUsername && !mfaChallenging && (
          <>
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
                    border: '1px solid #dadce0',
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

            <form onSubmit={handleSubmit}>
              {isSignUp && (
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
                      required
                    />
                  </div>
                  <span style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.25rem', display: 'block' }}>
                    Lowercase letters, numbers, and hyphens only (3-20 chars).
                  </span>
                </div>
              )}

              <div className="form-group">
                <label className="form-label">Email Address</label>
                <div style={{ position: 'relative' }}>
                  <Mail size={18} style={{
                    position: 'absolute',
                    left: '1rem',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    color: '#64748b'
                  }} />
                  <input
                    type="email"
                    className="text-input"
                    style={{ paddingLeft: '2.75rem' }}
                    placeholder="you@example.com"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    disabled={loading}
                    required
                  />
                </div>
              </div>

              <div className="form-group" style={{ marginBottom: '2rem' }}>
                <label className="form-label">Password</label>
                <div style={{ position: 'relative' }}>
                  <Lock size={18} style={{
                    position: 'absolute',
                    left: '1rem',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    color: '#64748b'
                  }} />
                  <input
                    type="password"
                    className="text-input"
                    style={{ paddingLeft: '2.75rem' }}
                    placeholder="••••••••"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    disabled={loading}
                    required
                  />
                </div>
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
                  isSignUp ? 'Sign Up' : 'Sign In'
                )}
              </button>
            </form>

            <div style={{
              textAlign: 'center',
              marginTop: '1.5rem',
              fontSize: '0.9rem',
              color: '#94a3b8'
            }}>
              {isSignUp ? 'Already have an account?' : "Don't have an account?"}{' '}
              <button
                className="btn-link"
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#38bdf8',
                  fontWeight: 600,
                  cursor: 'pointer',
                  padding: 0
                }}
                onClick={() => {
                  setIsSignUp(!isSignUp);
                  setError('');
                }}
                disabled={loading}
              >
                {isSignUp ? 'Sign In' : 'Sign Up'}
              </button>
            </div>
          </>
        )}

        {pickingUsername && !mfaChallenging && (
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

        {mfaChallenging && (
          <form onSubmit={handleMfaSubmit}>
            <p style={{ color: '#94a3b8', marginBottom: '1.25rem', lineHeight: 1.5 }}>
              Enter the 6-digit code from your authenticator app to finish signing in.
            </p>

            <div className="form-group" style={{ marginBottom: '1.5rem' }}>
              <label className="form-label">Authenticator code</label>
              <input
                type="text"
                className="text-input"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="123456"
                disabled={loading}
                autoFocus
                required
              />
            </div>

            <button type="submit" className="btn btn-primary" style={{ width: '100%', padding: '0.85rem' }} disabled={loading}>
              {loading ? (
                <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
              ) : (
                'Verify and continue'
              )}
            </button>

            <div style={{ textAlign: 'center', marginTop: '1.5rem', fontSize: '0.9rem', color: '#94a3b8' }}>
              <button
                type="button"
                className="btn-link"
                style={{ background: 'none', border: 'none', color: '#38bdf8', fontWeight: 600, cursor: 'pointer', padding: 0 }}
                onClick={handleCancelMfa}
                disabled={loading}
              >
                Cancel
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
