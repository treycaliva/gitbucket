import { useState, useEffect, useRef, useCallback } from 'react';
import { authService } from '../authService';
import {
  multiFactor,
  TotpMultiFactorGenerator,
} from 'firebase/auth';
import QRCode from 'qrcode';
import { Shield, ShieldCheck, ShieldAlert, Copy, Check } from 'lucide-react';

export default function Security({ user }) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [enrolledFactors, setEnrolledFactors] = useState([]);

  // Enrollment-in-progress state
  const [enrolling, setEnrolling] = useState(false);
  const [totpSecret, setTotpSecret] = useState(null);
  const [secretText, setSecretText] = useState('');
  const [qrUrl, setQrUrl] = useState('');
  const [verifyCode, setVerifyCode] = useState('');
  const [enrollSubmitting, setEnrollSubmitting] = useState(false);
  const [secretCopied, setSecretCopied] = useState(false);

  // Unenroll-confirmation state
  const [unenrollTarget, setUnenrollTarget] = useState(null);
  const [unenrollSubmitting, setUnenrollSubmitting] = useState(false);

  const qrCanvasRef = useRef(null);
  const copyTimerRef = useRef(null);

  const auth = authService.getAuthInstance();

  const refreshFactors = useCallback(async () => {
    if (!auth?.currentUser) {
      setEnrolledFactors([]);
      return;
    }
    try {
      await auth.currentUser.reload();
      const mf = multiFactor(auth.currentUser);
      setEnrolledFactors(mf.enrolledFactors || []);
    } catch (err) {
      console.error(err);
      setError(err.message || 'Failed to load MFA status.');
    }
  }, [auth]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      await refreshFactors();
      if (!cancelled) setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [refreshFactors]);

  // QR rendering effect — runs when qrUrl changes.
  useEffect(() => {
    if (qrUrl && qrCanvasRef.current) {
      QRCode.toCanvas(qrCanvasRef.current, qrUrl, { width: 200, margin: 1 }, (err) => {
        if (err) console.error('QR render failed:', err);
      });
    }
  }, [qrUrl]);

  useEffect(() => {
    return () => {
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
    };
  }, []);

  const handleStartEnroll = async () => {
    setError('');
    if (!auth?.currentUser) {
      setError('You must be signed in.');
      return;
    }
    setEnrollSubmitting(true);
    try {
      const session = await multiFactor(auth.currentUser).getSession();
      const secret = await TotpMultiFactorGenerator.generateSecret(session);
      const url = secret.generateQrCodeUrl(user?.email || auth.currentUser.email || 'gitbucket-user', 'GitBucket');
      setTotpSecret(secret);
      setSecretText(secret.secretKey);
      setQrUrl(url);
      setEnrolling(true);
    } catch (err) {
      console.error(err);
      if (err.code === 'auth/requires-recent-login') {
        setError('Please sign out and sign in again before enrolling two-factor authentication.');
      } else {
        setError(err.message || 'Failed to start enrollment.');
      }
    } finally {
      setEnrollSubmitting(false);
    }
  };

  const handleVerifyEnroll = async (e) => {
    e.preventDefault();
    setError('');
    if (!/^\d{6}$/.test(verifyCode)) {
      setError('Enter the 6-digit code from your authenticator app.');
      return;
    }
    setEnrollSubmitting(true);
    try {
      const assertion = TotpMultiFactorGenerator.assertionForEnrollment(totpSecret, verifyCode);
      await multiFactor(auth.currentUser).enroll(assertion, 'Authenticator app');
      // Clear enrollment state
      setEnrolling(false);
      setTotpSecret(null);
      setSecretText('');
      setQrUrl('');
      setVerifyCode('');
      await refreshFactors();
    } catch (err) {
      console.error(err);
      if (err.code === 'auth/invalid-verification-code') {
        setError('That code did not match. Try again with a fresh code from your app.');
      } else if (err.code === 'auth/requires-recent-login') {
        handleCancelEnroll();
        setError('Please sign out and sign in again before enrolling two-factor authentication.');
      } else {
        setError(err.message || 'Enrollment failed.');
      }
    } finally {
      setEnrollSubmitting(false);
    }
  };

  const handleCancelEnroll = () => {
    setEnrolling(false);
    setTotpSecret(null);
    setSecretText('');
    setQrUrl('');
    setVerifyCode('');
    setError('');
  };

  const handleCopySecret = async () => {
    try {
      await navigator.clipboard.writeText(secretText);
      setSecretCopied(true);
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => setSecretCopied(false), 2000);
    } catch (err) {
      console.error(err);
    }
  };

  const handleConfirmUnenroll = async () => {
    if (!unenrollTarget) return;
    setUnenrollSubmitting(true);
    setError('');
    try {
      await multiFactor(auth.currentUser).unenroll(unenrollTarget);
      setUnenrollTarget(null);
      await refreshFactors();
    } catch (err) {
      console.error(err);
      if (err.code === 'auth/requires-recent-login') {
        setUnenrollTarget(null);
        setError('Please sign out and sign in again before changing two-factor authentication.');
      } else {
        setError(err.message || 'Failed to remove second factor.');
      }
    } finally {
      setUnenrollSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div style={{ padding: '2rem', textAlign: 'center' }}>
        <span className="loader" style={{ width: '24px', height: '24px', borderWidth: '3px' }}></span>
      </div>
    );
  }

  const hasFactor = enrolledFactors.length > 0;

  return (
    <div style={{ padding: '2rem', maxWidth: '720px', margin: '0 auto' }}>
      <div style={{ marginBottom: '2rem' }}>
        <h1 style={{ fontSize: '1.75rem', fontWeight: 800, marginBottom: '0.5rem', display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
          <Shield size={26} style={{ color: '#38bdf8' }} />
          Security
        </h1>
        <p style={{ color: '#94a3b8' }}>
          Manage two-factor authentication for your GitBucket account.
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
          marginBottom: '1.5rem'
        }}>
          {error}
        </div>
      )}

      <div className="glass-card" style={{ padding: '1.75rem' }}>
        <h2 style={{ fontSize: '1.1rem', fontWeight: 700, marginBottom: '0.75rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          {hasFactor ? <ShieldCheck size={20} style={{ color: '#22c55e' }} /> : <ShieldAlert size={20} style={{ color: '#fbbf24' }} />}
          Authenticator app (TOTP)
        </h2>

        {!hasFactor && !enrolling && (
          <>
            <p style={{ color: '#94a3b8', marginBottom: '1rem', lineHeight: 1.5 }}>
              Add a second sign-in step using an authenticator app like Google Authenticator, 1Password, or Authy.
            </p>
            <p style={{ color: '#94a3b8', marginBottom: '1.25rem', lineHeight: 1.5, fontSize: '0.85rem' }}>
              Two-factor authentication only protects email/password sign-in. If you sign in with Google, that path is not affected.
            </p>
            <button className="btn btn-primary" disabled={enrollSubmitting} onClick={handleStartEnroll}>
              {enrollSubmitting ? 'Starting…' : 'Set up authenticator'}
            </button>
          </>
        )}

        {enrolling && (
          <form onSubmit={handleVerifyEnroll}>
            <p style={{ color: '#94a3b8', marginBottom: '1rem', lineHeight: 1.5 }}>
              Scan this QR code with your authenticator app, or type the secret manually. Then enter the 6-digit code below to confirm.
            </p>

            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', marginBottom: '1.25rem' }}>
              <canvas ref={qrCanvasRef} style={{ borderRadius: '8px', background: '#ffffff', padding: '8px' }} />
            </div>

            <div className="form-group">
              <label className="form-label" style={{ fontSize: '0.8rem' }}>Secret (if you can't scan)</label>
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <input
                  type="text"
                  className="text-input"
                  readOnly
                  value={secretText}
                  style={{ fontFamily: 'monospace', fontSize: '0.85rem', flex: 1 }}
                />
                <button type="button" className="btn btn-secondary" onClick={handleCopySecret} style={{ padding: '0 0.85rem' }}>
                  {secretCopied ? <Check size={16} /> : <Copy size={16} />}
                </button>
              </div>
            </div>

            <div className="form-group" style={{ marginBottom: '1.5rem' }}>
              <label className="form-label">6-digit code</label>
              <input
                type="text"
                className="text-input"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                value={verifyCode}
                onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="123456"
                disabled={enrollSubmitting}
                autoFocus
                required
              />
            </div>

            <div style={{ display: 'flex', gap: '0.75rem' }}>
              <button type="submit" className="btn btn-primary" disabled={enrollSubmitting}>
                {enrollSubmitting ? 'Verifying…' : 'Verify and enroll'}
              </button>
              <button type="button" className="btn btn-secondary" onClick={handleCancelEnroll} disabled={enrollSubmitting}>
                Cancel
              </button>
            </div>
          </form>
        )}

        {hasFactor && !enrolling && (
          <>
            <p style={{ color: '#94a3b8', marginBottom: '1rem', lineHeight: 1.5 }}>
              Two-factor authentication is on. You'll be asked for a code from your authenticator app whenever you sign in with email and password.
            </p>
            <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 1.25rem 0' }}>
              {enrolledFactors.map((f) => (
                <li
                  key={f.uid}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    background: 'rgba(255,255,255,0.03)',
                    border: '1px solid rgba(255,255,255,0.06)',
                    borderRadius: '8px',
                    padding: '0.85rem 1rem'
                  }}
                >
                  <div>
                    <div style={{ fontWeight: 600 }}>{f.displayName || 'Authenticator app'}</div>
                    <div style={{ color: '#64748b', fontSize: '0.8rem' }}>
                      Enrolled {f.enrollmentTime ? new Date(f.enrollmentTime).toLocaleDateString() : ''}
                    </div>
                  </div>
                  <button className="btn btn-secondary" onClick={() => setUnenrollTarget(f)}>
                    Remove
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>

      {unenrollTarget && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="unenroll-modal-title"
          onClick={(e) => { if (e.target === e.currentTarget) setUnenrollTarget(null); }}
          style={{
            position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
            display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 100
          }}
        >
          <div className="glass-card modal-content" style={{ padding: '2rem', maxWidth: '440px' }}>
            <h3 id="unenroll-modal-title" style={{ fontSize: '1.2rem', fontWeight: 700, marginBottom: '0.75rem' }}>Remove two-factor authentication?</h3>
            <p style={{ color: '#94a3b8', marginBottom: '1.5rem', lineHeight: 1.5 }}>
              Removing two-factor authentication makes your account less secure. You'll only need your password to sign in.
            </p>
            <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" disabled={unenrollSubmitting} onClick={() => setUnenrollTarget(null)}>
                Cancel
              </button>
              <button className="btn btn-primary" disabled={unenrollSubmitting} onClick={handleConfirmUnenroll}>
                {unenrollSubmitting ? 'Removing…' : 'Remove'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
