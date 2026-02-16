'use client';

import Link from 'next/link';
import { FormEvent, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Battle, getPublicBattle, login, remixBattle, signup } from '../../../lib/api';

type PublicBattleClientProps = {
  battleID: string;
};

const TOKEN_KEY = 'personaworlds_token';
const REMIX_PAYLOAD_KEY = 'personaworlds_remix_payload';

function battleQuality(turns: Battle['turns']) {
  const scores = turns
    .map((turn) => Number(turn.metadata?.quality_score ?? 0))
    .filter((score) => Number.isFinite(score) && score >= 0);
  if (scores.length === 0) {
    return { avg: 0, label: 'Low', className: 'quality-low' };
  }

  const avg = scores.reduce((sum, value) => sum + value, 0) / scores.length;
  if (avg >= 80) {
    return { avg, label: 'High', className: 'quality-high' };
  }
  if (avg >= 60) {
    return { avg, label: 'Med', className: 'quality-med' };
  }
  return { avg, label: 'Low', className: 'quality-low' };
}

export default function PublicBattleClient({ battleID }: PublicBattleClientProps) {
  const router = useRouter();
  const [battle, setBattle] = useState<Battle | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [remixing, setRemixing] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');

  const [signupModalOpen, setSignupModalOpen] = useState(false);
  const [authMode, setAuthMode] = useState<'signup' | 'login'>('signup');
  const [authEmail, setAuthEmail] = useState('');
  const [authPassword, setAuthPassword] = useState('');
  const [authLoading, setAuthLoading] = useState(false);

  useEffect(() => {
    if (!battleID) {
      setLoading(false);
      return;
    }
    void loadBattle(battleID, true);
  }, [battleID]);

  useEffect(() => {
    if (!battleID || !battle) {
      return;
    }
    if (battle.status !== 'PENDING' && battle.status !== 'PROCESSING') {
      return;
    }

    const timer = window.setTimeout(() => {
      void loadBattle(battleID, false);
    }, 3000);

    return () => window.clearTimeout(timer);
  }, [battle, battleID]);

  async function loadBattle(id: string, initial: boolean) {
    try {
      if (initial) {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError('');
      const response = await getPublicBattle(id);
      setBattle(response.battle);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load battle');
    } finally {
      if (initial) {
        setLoading(false);
      } else {
        setRefreshing(false);
      }
    }
  }

  function shareURL() {
    if (typeof window === 'undefined' || !battle) {
      return '';
    }
    return `${window.location.origin}/b/${battle.id}`;
  }

  async function onCopyLink() {
    const url = shareURL();
    if (!url) {
      return;
    }
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(url);
        setMessage('Link copied.');
      } else {
        setMessage(url);
      }
    } catch {
      setMessage(url);
    }
  }

  function onShareX() {
    if (!battle) {
      return;
    }
    const url = shareURL();
    if (!url) {
      return;
    }
    const text = `Persona Battle: ${battle.topic}`;
    const intent = `https://x.com/intent/tweet?url=${encodeURIComponent(url)}&text=${encodeURIComponent(text)}`;
    window.open(intent, '_blank', 'noopener,noreferrer');
  }

  function onShareLinkedIn() {
    const url = shareURL();
    if (!url) {
      return;
    }
    const intent = `https://www.linkedin.com/sharing/share-offsite/?url=${encodeURIComponent(url)}`;
    window.open(intent, '_blank', 'noopener,noreferrer');
  }

  function onFollowPersonas() {
    const token = typeof window !== 'undefined' ? localStorage.getItem(TOKEN_KEY) || '' : '';
    if (!token) {
      setSignupModalOpen(true);
      return;
    }
    setMessage('You are logged in. Open your dashboard to follow personas and start battles.');
  }

  async function onRemix() {
    if (!battle) {
      return;
    }

    try {
      setRemixing(true);
      setError('');
      setMessage('');

      const token = typeof window !== 'undefined' ? localStorage.getItem(TOKEN_KEY) || '' : '';
      const response = await remixBattle(battle.id, token || undefined);
      localStorage.setItem(REMIX_PAYLOAD_KEY, JSON.stringify(response.remix_payload));

      const query = new URLSearchParams({
        open_battle_modal: '1',
        remix_room_id: response.remix_payload.room_id,
        remix_topic: response.remix_payload.topic
      }).toString();

      if (response.requires_signup) {
        router.push('/signup');
        return;
      }

      router.push(`/?${query}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not remix battle');
    } finally {
      setRemixing(false);
    }
  }

  async function onAuthSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setAuthLoading(true);
      setError('');
      setMessage('');

      const response = authMode === 'signup' ? await signup(authEmail, authPassword) : await login(authEmail, authPassword);
      localStorage.setItem(TOKEN_KEY, response.token);
      setSignupModalOpen(false);
      setMessage(authMode === 'signup' ? 'Account created. You can now follow personas.' : 'Logged in. You can now follow personas.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'authentication failed');
    } finally {
      setAuthLoading(false);
    }
  }

  if (loading) {
    return (
      <main className="container">
        <section className="panel battle-panel stack">
          <h1>Persona Battle</h1>
          <p className="subtle">Loading shared battle...</p>
        </section>
      </main>
    );
  }

  if (!battle) {
    return (
      <main className="container">
        <section className="panel battle-panel stack">
          <h1>Persona Battle</h1>
          <p className="error">{error || 'battle not found'}</p>
          <Link className="primary-link" href="/signup">
            Create your own persona battles
          </Link>
        </section>
      </main>
    );
  }

  const quality = battleQuality(battle.turns);

  return (
    <main className="container public-battle-layout">
      <section className="panel battle-panel stack public-battle-main">
        <div className="battle-header">
          <div className="stack">
            <h1>{battle.topic}</h1>
            <div className="battle-turn-meta">
              <span className="status">Room: {battle.room_name || battle.room_id}</span>
              <span className="status">Status: {battle.status}</span>
              <span className={`status quality-badge ${quality.className}`}>Quality: {quality.label}</span>
              <span className="subtle">{new Date(battle.created_at).toLocaleString()}</span>
            </div>
          </div>
          <div className="battle-header-actions">
            <button className="secondary" onClick={onRemix} disabled={remixing}>
              {remixing ? 'Preparing...' : 'Remix this'}
            </button>
            <button className="secondary" onClick={onCopyLink}>
              Copy Link
            </button>
            <button className="secondary" onClick={onShareX}>
              Share to X
            </button>
            <button className="secondary" onClick={onShareLinkedIn}>
              Share to LinkedIn
            </button>
            <button
              className="secondary"
              onClick={() => {
                if (battleID) {
                  void loadBattle(battleID, false);
                }
              }}
              disabled={refreshing}
            >
              {refreshing ? 'Refreshing...' : 'Refresh'}
            </button>
          </div>
        </div>

        <div className="mini-card">
          <strong>{battle.persona_a.name} (FOR)</strong>
          <span>{battle.persona_a.bio || 'No bio.'}</span>
          <strong>{battle.persona_b.name} (AGAINST)</strong>
          <span>{battle.persona_b.bio || 'No bio.'}</span>
        </div>

        {battle.status === 'FAILED' && <p className="error">Battle generation failed.</p>}
        {battle.turns.length === 0 && battle.status !== 'FAILED' && (
          <p className="subtle">Battle turns are being generated...</p>
        )}

        {battle.turns.length > 0 && (
          <div className="stack">
            <h2>Turns</h2>
            {battle.turns.map((turn) => (
              <article key={`${turn.battle_id}-${turn.turn_index}`} className="battle-turn">
                <div className="battle-turn-meta">
                  <span className="status">Turn {turn.turn_index}</span>
                  <span>{turn.persona_name || 'Persona'}</span>
                </div>
                <p>{turn.content}</p>
              </article>
            ))}
          </div>
        )}

        {(battle.verdict.verdict || battle.verdict.takeaways.length > 0) && (
          <div className="stack">
            <h2>AI Verdict</h2>
            {battle.verdict.verdict && <p>{battle.verdict.verdict}</p>}
            {battle.verdict.takeaways.length > 0 && (
              <ul className="takeaway-list">
                {battle.verdict.takeaways.map((takeaway, index) => (
                  <li key={`${takeaway}-${index}`}>{takeaway}</li>
                ))}
              </ul>
            )}
          </div>
        )}

        {(message || error) && (
          <div className="status-bar">
            {message && <span className="message">{message}</span>}
            {error && <span className="error">{error}</span>}
          </div>
        )}
      </section>

      <aside className="panel cta-sticky-card stack">
        <h2>Create your own persona battles</h2>
        <p className="subtle">Launch debates, share links, and grow your audience with AI personas.</p>
        <Link className="primary-link" href="/signup">
          Start for free
        </Link>
        <button className="secondary" onClick={onFollowPersonas}>
          Follow personas
        </button>
      </aside>

      {signupModalOpen && (
        <div className="modal-backdrop" onClick={() => setSignupModalOpen(false)}>
          <section
            className="panel modal-card stack"
            onClick={(event) => {
              event.stopPropagation();
            }}
          >
            <div className="section-head">
              <h2>{authMode === 'signup' ? 'Sign up to follow personas' : 'Log in to follow personas'}</h2>
              <button className="secondary modal-close" onClick={() => setSignupModalOpen(false)}>
                Close
              </button>
            </div>

            <form className="stack" onSubmit={onAuthSubmit}>
              <input
                type="email"
                value={authEmail}
                onChange={(event) => setAuthEmail(event.target.value)}
                placeholder="email"
                required
              />
              <input
                type="password"
                value={authPassword}
                onChange={(event) => setAuthPassword(event.target.value)}
                placeholder="password (min 8 chars)"
                required
                minLength={8}
              />
              <button type="submit" disabled={authLoading}>
                {authLoading ? 'Please wait...' : authMode === 'signup' ? 'Sign up' : 'Log in'}
              </button>
            </form>

            <button className="link-like" onClick={() => setAuthMode((current) => (current === 'signup' ? 'login' : 'signup'))}>
              {authMode === 'signup' ? 'Already have an account? Log in' : 'Need an account? Sign up'}
            </button>
          </section>
        </div>
      )}
    </main>
  );
}
