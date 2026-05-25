import { useState, useEffect, useRef, useCallback } from 'react';
import { authService } from '../authService';
import {
  multiFactor,
  TotpMultiFactorGenerator,
} from 'firebase/auth';
import QRCode from 'qrcode';
import { Shield, ShieldCheck, ShieldAlert, Copy, Check } from 'lucide-react';
import Card from '../components/Card';

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
      <div className="loader-container" style={{ padding: '2rem 0' }}>
        <div className="loader"></div>
      </div>
    );
  }

  const hasFactor = enrolledFactors.length > 0;

  return (
    <div style={{ maxWidth: '720px', margin: '0 auto' }}>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0, display: 'flex', alignItems: 'center', gap: 9 }}>
          <Shield size={18} style={{ color: 'var(--gb-accent)' }} />
          Security
        </h1>
        <p style={{ fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6, maxWidth: 760, lineHeight: 1.5 }}>
          Manage two-factor authentication for your GitBucket account.
        </p>
      </div>

      {error && (
        <div style={{
          background: 'var(--gb-err-dim)',
          border: '1px solid rgba(248,113,113,0.25)',
          color: 'var(--gb-err)',
          padding: '12px 14px',
          borderRadius: 8,
          fontSize: 13,
          marginBottom: 22,
        }}>
          {error}
        </div>
      )}

      <Card style={{ padding: 20 }}>
        <h2 style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginBottom: 12, display: 'flex', alignItems: 'center', gap: 8 }}>
          {hasFactor ? <ShieldCheck size={18} style={{ color: 'var(--gb-ok)' }} /> : <ShieldAlert size={18} style={{ color: 'var(--gb-warn)' }} />}
          Authenticator app (TOTP)
        </h2>

        {!hasFactor && !enrolling && (
          <>
            <p style={{ color: 'var(--gb-fg-3)', marginBottom: 16, lineHeight: 1.5, fontSize: 13 }}>
              Add a second sign-in step using an authenticator app like Google Authenticator, 1Password, or Authy.
            </p>
            <p style={{ color: 'var(--gb-fg-3)', marginBottom: 20, lineHeight: 1.5, fontSize: 12.5 }}>
              Two-factor authentication only protects email/password sign-in. If you sign in with Google, that path is not affected.
            </p>
            <button className="btn btn-primary" disabled={enrollSubmitting} onClick={handleStartEnroll}>
              {enrollSubmitting ? 'Starting…' : 'Set up authenticator'}
            </button>
          </>
        )}

        {enrolling && (
          <form onSubmit={handleVerifyEnroll}>
            <p style={{ color: 'var(--gb-fg-3)', marginBottom: 16, lineHeight: 1.5, fontSize: 13 }}>
              Scan this QR code with your authenticator app, or type the secret manually. Then enter the 6-digit code below to confirm.
            </p>

            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', marginBottom: 20 }}>
              <canvas ref={qrCanvasRef} style={{ borderRadius: 8, background: '#ffffff', padding: '8px' }} />
            </div>

            <div className="form-group">
              <label className="form-label" style={{ fontSize: 12 }}>Secret (if you can't scan)</label>
              <div style={{ display: 'flex', gap: 8 }}>
                <input
                  type="text"
                  className="text-input"
                  readOnly
                  value={secretText}
                  style={{ fontFamily: 'var(--gb-mono)', fontSize: 13, flex: 1 }}
                />
                <button type="button" className="btn btn-secondary btn-icon" onClick={handleCopySecret}>
                  {secretCopied ? <Check size={16} /> : <Copy size={16} />}
                </button>
              </div>
            </div>

            <div className="form-group" style={{ marginBottom: 22 }}>
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

            <div style={{ display: 'flex', gap: 12 }}>
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
            <p style={{ color: 'var(--gb-fg-3)', marginBottom: 16, lineHeight: 1.5, fontSize: 13 }}>
              Two-factor authentication is on. You'll be asked for a code from your authenticator app whenever you sign in with email and password.
            </p>
            <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 20px 0', display: 'flex', flexDirection: 'column', gap: 10 }}>
              {enrolledFactors.map((f) => (
                <li
                  key={f.uid}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    background: 'var(--gb-surface-2)',
                    border: '1px solid var(--gb-line)',
                    borderRadius: 8,
                    padding: 14,
                  }}
                >
                  <div>
                    <div style={{ fontWeight: 600, fontSize: 13.5, color: 'var(--gb-fg)' }}>{f.displayName || 'Authenticator app'}</div>
                    <div style={{ color: 'var(--gb-fg-4)', fontSize: 11.5, marginTop: 4 }}>
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
      </Card>

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
          <Card className="modal-content" style={{ padding: 20, maxWidth: '440px' }}>
            <h3 id="unenroll-modal-title" style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginBottom: 12 }}>Remove two-factor authentication?</h3>
            <p style={{ color: 'var(--gb-fg-3)', marginBottom: 22, lineHeight: 1.5, fontSize: 13 }}>
              Removing two-factor authentication makes your account less secure. You'll only need your password to sign in.
            </p>
            <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" disabled={unenrollSubmitting} onClick={() => setUnenrollTarget(null)}>
                Cancel
              </button>
              <button className="btn btn-primary" disabled={unenrollSubmitting} onClick={handleConfirmUnenroll}>
                {unenrollSubmitting ? 'Removing…' : 'Remove'}
              </button>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
