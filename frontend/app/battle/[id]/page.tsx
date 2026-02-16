'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useEffect, useMemo, useState } from 'react';
import { Battle, getBattle } from '../../../lib/api';

const TOKEN_KEY = 'personaworlds_token';

export default function BattlePage() {
  const params = useParams<{ id: string }>();
  const battleID = useMemo(() => (params?.id || '').toString().trim(), [params]);

  const [token, setToken] = useState('');
  const [battle, setBattle] = useState<Battle | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    const stored = localStorage.getItem(TOKEN_KEY) || '';
    setToken(stored);
  }, []);

  useEffect(() => {
    if (!token || !battleID) {
      setLoading(false);
      return;
    }
    void loadBattle(token, battleID, true);
  }, [token, battleID]);

  useEffect(() => {
    if (!token || !battleID || !battle) {
      return;
    }
    if (battle.status !== 'PENDING' && battle.status !== 'PROCESSING') {
      return;
    }

    const timer = window.setTimeout(() => {
      void loadBattle(token, battleID, false);
    }, 2500);

    return () => window.clearTimeout(timer);
  }, [battle, token, battleID]);

  async function loadBattle(authToken: string, id: string, initial: boolean) {
    try {
      if (initial) {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError('');
      const response = await getBattle(authToken, id);
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

  async function onShare() {
    if (!battle) {
      return;
    }
    const shareURL = typeof window !== 'undefined' ? `${window.location.origin}/b/${battle.id}` : '';
    if (!shareURL) {
      return;
    }

    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(shareURL);
        setMessage(`Share link copied: ${shareURL}`);
      } else {
        setMessage(`Share link: ${shareURL}`);
      }
    } catch {
      setMessage(`Share link: ${shareURL}`);
    }
  }

  if (!token) {
    return (
      <main className="container">
        <section className="panel battle-panel stack">
          <h1>Battle</h1>
          <p className="subtle">Login required to view this battle.</p>
          <Link className="primary-link" href="/">
            Go to dashboard
          </Link>
        </section>
      </main>
    );
  }

  if (loading) {
    return (
      <main className="container">
        <section className="panel battle-panel stack">
          <h1>Battle</h1>
          <p className="subtle">Loading battle...</p>
        </section>
      </main>
    );
  }

  if (!battle) {
    return (
      <main className="container">
        <section className="panel battle-panel stack">
          <h1>Battle</h1>
          <p className="error">{error || 'battle not found'}</p>
          <Link className="primary-link" href="/">
            Back to dashboard
          </Link>
        </section>
      </main>
    );
  }

  return (
    <main className="container">
      <section className="panel battle-panel stack">
        <div className="battle-header">
          <div className="stack">
            <h1>{battle.topic}</h1>
            <div className="battle-turn-meta">
              <span className="status">Room: {battle.room_name || battle.room_id}</span>
              <span className="status">Status: {battle.status}</span>
              <span className="subtle">{new Date(battle.created_at).toLocaleString()}</span>
            </div>
          </div>
          <div className="battle-header-actions">
            <button className="secondary" onClick={onShare}>
              Share
            </button>
            <button
              className="secondary"
              onClick={() => {
                if (token && battleID) {
                  void loadBattle(token, battleID, false);
                }
              }}
              disabled={refreshing}
            >
              {refreshing ? 'Refreshing...' : 'Refresh'}
            </button>
            <Link className="primary-link" href="/">
              Dashboard
            </Link>
          </div>
        </div>

        <div className="mini-card">
          <strong>{battle.persona_a.name} (FOR)</strong>
          <span>{battle.persona_a.bio || 'No bio.'}</span>
          <strong>{battle.persona_b.name} (AGAINST)</strong>
          <span>{battle.persona_b.bio || 'No bio.'}</span>
        </div>

        {battle.status === 'FAILED' && <p className="error">{battle.error || 'battle generation failed'}</p>}

        {battle.turns.length === 0 && (battle.status === 'PENDING' || battle.status === 'PROCESSING') && (
          <p className="subtle">Battle is being generated...</p>
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
    </main>
  );
}
