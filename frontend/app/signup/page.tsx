'use client';

import Link from 'next/link';
import { FormEvent, useState } from 'react';
import { useRouter } from 'next/navigation';
import { login, signup } from '../../lib/api';

const TOKEN_KEY = 'personaworlds_token';
const REMIX_PAYLOAD_KEY = 'personaworlds_remix_payload';

export default function SignupPage() {
  const router = useRouter();
  const [mode, setMode] = useState<'signup' | 'login'>('signup');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setLoading(true);
      setError('');
      const response = mode === 'signup' ? await signup(email, password) : await login(email, password);
      localStorage.setItem(TOKEN_KEY, response.token);

      const remixRaw = localStorage.getItem(REMIX_PAYLOAD_KEY);
      if (remixRaw) {
        try {
          const remixPayload = JSON.parse(remixRaw) as { room_id?: string; topic?: string };
          const roomID = (remixPayload.room_id || '').trim();
          const topic = (remixPayload.topic || '').trim();
          localStorage.removeItem(REMIX_PAYLOAD_KEY);
          if (roomID && topic) {
            const query = new URLSearchParams({
              open_battle_modal: '1',
              remix_room_id: roomID,
              remix_topic: topic
            }).toString();
            router.push(`/?${query}`);
            return;
          }
        } catch {
          localStorage.removeItem(REMIX_PAYLOAD_KEY);
          // ignore parse errors and continue default flow
        }
      }

      router.push('/');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'authentication failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="container">
      <section className="panel auth-panel stack">
        <h1>{mode === 'signup' ? 'Create your account' : 'Welcome back'}</h1>
        <p className="subtle">Create and share persona battles in minutes.</p>

        <form onSubmit={onSubmit} className="stack">
          <input
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="email"
            required
          />
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder="password (min 8 chars)"
            required
            minLength={8}
          />
          <button type="submit" disabled={loading}>
            {loading ? 'Please wait...' : mode === 'signup' ? 'Sign up' : 'Log in'}
          </button>
        </form>

        <button className="link-like" onClick={() => setMode((current) => (current === 'signup' ? 'login' : 'signup'))}>
          {mode === 'signup' ? 'Already have an account? Log in' : 'Need an account? Sign up'}
        </button>

        <Link className="primary-link" href="/">
          Back to home
        </Link>

        {error && <p className="error">{error}</p>}
      </section>
    </main>
  );
}
