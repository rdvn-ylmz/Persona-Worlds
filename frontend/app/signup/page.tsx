'use client';

import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { FormEvent, Suspense, useMemo, useState } from 'react';
import { login, signup } from '../../lib/api';
import { Spinner } from '../../components/spinner';
import { useToast } from '../../components/toast-provider';

const TOKEN_KEY = 'personaworlds_token';
const SHARE_SLUG_KEY = 'personaworlds_share_slug';

function SignupPageContent() {
  const toast = useToast();
  const searchParams = useSearchParams();
  const [isSignup, setIsSignup] = useState(true);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const redirectPath = useMemo(() => {
    const next = (searchParams.get('next') || '/').trim();
    const safePath = next.startsWith('/') ? next : '/';
    const remix = (searchParams.get('remix') || '').trim() === '1';
    if (!remix) {
      return safePath;
    }
    return `${safePath}${safePath.includes('?') ? '&' : '?'}remix=1`;
  }, [searchParams]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setLoading(true);
      setError('');
      setMessage('');

      const shareSlug = isSignup && typeof window !== 'undefined' ? (localStorage.getItem(SHARE_SLUG_KEY) || '').trim() : '';
      const response = isSignup ? await signup(email, password, shareSlug) : await login(email, password);
      localStorage.setItem(TOKEN_KEY, response.token);
      if (isSignup && shareSlug) {
        localStorage.removeItem(SHARE_SLUG_KEY);
      }
      setMessage(isSignup ? 'Account created, redirecting...' : 'Logged in, redirecting...');
      toast.success(isSignup ? 'Account created.' : 'Logged in.');
      window.location.href = redirectPath || '/';
    } catch (err) {
      const messageText = err instanceof Error ? err.message : 'auth failed';
      setError(messageText);
      toast.error(messageText);
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="container">
      <section className="panel auth-panel stack">
        <h1>{isSignup ? 'Create account' : 'Log in'}</h1>
        <p className="subtle">Continue to your remix flow in one step.</p>

        <form onSubmit={onSubmit} className="stack">
          <input type="email" placeholder="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
          <input
            type="password"
            placeholder="password (min 8 chars)"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
          <button type="submit" disabled={loading}>
            <span className="button-content">
              {loading && <Spinner />}
              <span>{loading ? 'Please wait...' : isSignup ? 'Sign up' : 'Log in'}</span>
            </span>
          </button>
        </form>

        <button type="button" className="link-like" onClick={() => setIsSignup((current) => !current)} disabled={loading}>
          {isSignup ? 'Already have an account? Log in' : 'Need an account? Sign up'}
        </button>

        <div className="row">
          <Link className="primary-link" href="/">
            Back to home
          </Link>
        </div>

        {(message || error) && (
          <div className="status-bar">
            {message && <span className="message">{message}</span>}
            {error && <span className="error">{error}</span>}
          </div>
        )}
      </section>
    </main>
  );
}

export default function SignupPage() {
  return (
    <Suspense
      fallback={
        <main className="container">
          <section className="panel auth-panel stack">
            <h1>Create account</h1>
            <p className="subtle">Loading signup flow...</p>
          </section>
        </main>
      }
    >
      <SignupPageContent />
    </Suspense>
  );
}
