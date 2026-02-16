'use client';

import { FormEvent, useEffect, useMemo, useState } from 'react';
import {
  Persona,
  PersonaPayload,
  Post,
  PreviewResponse,
  Room,
  ThreadResponse,
  approvePost,
  createDraft,
  createPersona,
  generateReplies,
  getThread,
  listPersonas,
  listRoomPosts,
  listRooms,
  login,
  previewPersona,
  signup,
  updatePersona
} from '../lib/api';

const TOKEN_KEY = 'personaworlds_token';

function Badge({ authoredBy }: { authoredBy: Post['authored_by'] | ThreadResponse['replies'][number]['authored_by'] }) {
  const className =
    authoredBy === 'AI' ? 'badge badge-ai' : authoredBy === 'HUMAN' ? 'badge badge-human' : 'badge badge-approved';

  return <span className={className}>{authoredBy}</span>;
}

function parseLines(value: string) {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

export default function HomePage() {
  const [token, setToken] = useState<string>('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [isSignup, setIsSignup] = useState(true);

  const [personas, setPersonas] = useState<Persona[]>([]);
  const [rooms, setRooms] = useState<Room[]>([]);
  const [posts, setPosts] = useState<Post[]>([]);
  const [threads, setThreads] = useState<Record<string, ThreadResponse>>({});

  const [selectedRoomId, setSelectedRoomId] = useState<string>('');
  const [selectedPersonaId, setSelectedPersonaId] = useState<string>('');

  const [personaName, setPersonaName] = useState('Builder Bot');
  const [personaBio, setPersonaBio] = useState('Ships practical product experiments and learnings.');
  const [personaTone, setPersonaTone] = useState('direct');
  const [writingSamplesText, setWritingSamplesText] = useState(
    'I share practical lessons from shipped experiments.\nI avoid hype and focus on measurable outcomes.\nI ask one strong question to invite discussion.'
  );
  const [doNotSayText, setDoNotSayText] = useState('guaranteed growth\n100x overnight');
  const [catchphrasesText, setCatchphrasesText] = useState('Ship, learn, iterate');
  const [preferredLanguage, setPreferredLanguage] = useState<'tr' | 'en'>('en');
  const [formality, setFormality] = useState(1);

  const [draftQuota, setDraftQuota] = useState(5);
  const [replyQuota, setReplyQuota] = useState(25);

  const [previewDrafts, setPreviewDrafts] = useState<PreviewResponse['drafts']>([]);
  const [previewQuota, setPreviewQuota] = useState<PreviewResponse['quota'] | null>(null);

  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const selectedRoom = useMemo(() => rooms.find((r) => r.id === selectedRoomId), [rooms, selectedRoomId]);
  const selectedPersona = useMemo(() => personas.find((p) => p.id === selectedPersonaId), [personas, selectedPersonaId]);

  useEffect(() => {
    const stored = localStorage.getItem(TOKEN_KEY);
    if (stored) {
      setToken(stored);
    }
  }, []);

  useEffect(() => {
    if (!token) {
      setPersonas([]);
      setRooms([]);
      setPosts([]);
      return;
    }

    void refreshCoreData(token);
  }, [token]);

  useEffect(() => {
    if (!token || !selectedRoomId) {
      setPosts([]);
      return;
    }
    void refreshPosts(token, selectedRoomId);
  }, [token, selectedRoomId]);

  useEffect(() => {
    if (!selectedPersona) {
      return;
    }
    setPersonaName(selectedPersona.name);
    setPersonaBio(selectedPersona.bio);
    setPersonaTone(selectedPersona.tone);
    setWritingSamplesText(selectedPersona.writing_samples.join('\n'));
    setDoNotSayText(selectedPersona.do_not_say.join('\n'));
    setCatchphrasesText(selectedPersona.catchphrases.join('\n'));
    setPreferredLanguage(selectedPersona.preferred_language);
    setFormality(selectedPersona.formality);
    setDraftQuota(selectedPersona.daily_draft_quota);
    setReplyQuota(selectedPersona.daily_reply_quota);
  }, [selectedPersona]);

  useEffect(() => {
    setPreviewDrafts([]);
    setPreviewQuota(null);
  }, [selectedPersonaId, selectedRoomId]);

  async function refreshCoreData(authToken: string) {
    try {
      setLoading(true);
      setError('');
      const [personaRes, roomRes] = await Promise.all([listPersonas(authToken), listRooms(authToken)]);
      setPersonas(personaRes.personas);
      setRooms(roomRes.rooms);

      if (!selectedRoomId && roomRes.rooms.length > 0) {
        setSelectedRoomId(roomRes.rooms[0].id);
      }
      if (!selectedPersonaId && personaRes.personas.length > 0) {
        setSelectedPersonaId(personaRes.personas[0].id);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load data');
    } finally {
      setLoading(false);
    }
  }

  async function refreshPosts(authToken: string, roomId: string) {
    try {
      setLoading(true);
      setError('');
      const postRes = await listRoomPosts(authToken, roomId);
      setPosts(postRes.posts);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load posts');
    } finally {
      setLoading(false);
    }
  }

  function buildPersonaPayload(): PersonaPayload {
    const writingSamples = parseLines(writingSamplesText);
    const doNotSay = parseLines(doNotSayText);
    const catchphrases = parseLines(catchphrasesText);

    if (writingSamples.length !== 3) {
      throw new Error('writing_samples must contain exactly 3 lines');
    }

    return {
      name: personaName.trim(),
      bio: personaBio.trim(),
      tone: personaTone.trim(),
      writing_samples: writingSamples,
      do_not_say: doNotSay,
      catchphrases,
      preferred_language: preferredLanguage,
      formality,
      daily_draft_quota: draftQuota,
      daily_reply_quota: replyQuota
    };
  }

  function upsertPersonaInState(nextPersona: Persona) {
    setPersonas((current) => {
      const idx = current.findIndex((p) => p.id === nextPersona.id);
      if (idx === -1) {
        return [nextPersona, ...current];
      }
      const cloned = [...current];
      cloned[idx] = nextPersona;
      return cloned;
    });
    setSelectedPersonaId(nextPersona.id);
  }

  async function onAuthSubmit(event: FormEvent) {
    event.preventDefault();
    setError('');
    setMessage('');

    try {
      setLoading(true);
      const response = isSignup ? await signup(email, password) : await login(email, password);
      localStorage.setItem(TOKEN_KEY, response.token);
      setToken(response.token);
      setMessage(isSignup ? 'Account created.' : 'Logged in.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'auth failed');
    } finally {
      setLoading(false);
    }
  }

  function logout() {
    localStorage.removeItem(TOKEN_KEY);
    setToken('');
    setThreads({});
    setPreviewDrafts([]);
    setPreviewQuota(null);
    setMessage('Logged out.');
  }

  async function onCreatePersona(event: FormEvent) {
    event.preventDefault();
    if (!token) {
      return;
    }

    try {
      setLoading(true);
      setError('');
      const payload = buildPersonaPayload();
      const created = await createPersona(token, payload);
      upsertPersonaInState(created);
      setMessage(`Persona created: ${created.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not create persona');
    } finally {
      setLoading(false);
    }
  }

  async function onSavePersona() {
    if (!token || !selectedPersonaId) {
      setError('select a persona to update');
      return;
    }

    try {
      setLoading(true);
      setError('');
      const payload = buildPersonaPayload();
      const updated = await updatePersona(token, selectedPersonaId, payload);
      upsertPersonaInState(updated);
      setMessage(`Persona updated: ${updated.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not update persona');
    } finally {
      setLoading(false);
    }
  }

  async function onPreviewVoice() {
    if (!token || !selectedRoomId) {
      setError('select a room first');
      return;
    }

    try {
      setLoading(true);
      setError('');

      const payload = buildPersonaPayload();
      let personaId = selectedPersonaId;
      if (!personaId) {
        const created = await createPersona(token, payload);
        upsertPersonaInState(created);
        personaId = created.id;
      } else {
        const updated = await updatePersona(token, personaId, payload);
        upsertPersonaInState(updated);
      }

      const preview = await previewPersona(token, personaId, selectedRoomId);
      setPreviewDrafts(preview.drafts);
      setPreviewQuota(preview.quota);
      setMessage('Voice preview generated.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not generate preview');
    } finally {
      setLoading(false);
    }
  }

  async function onCreateDraft() {
    if (!token || !selectedRoomId || !selectedPersonaId) {
      setError('select a room and persona first');
      return;
    }

    try {
      setLoading(true);
      setError('');
      const draft = await createDraft(token, selectedRoomId, selectedPersonaId);
      setPosts((current) => [draft, ...current]);
      setMessage('AI draft post created. Review and approve to publish.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not create draft');
    } finally {
      setLoading(false);
    }
  }

  async function onApprove(postId: string) {
    if (!token) {
      return;
    }

    try {
      setLoading(true);
      setError('');
      const updated = await approvePost(token, postId);
      setPosts((current) => current.map((post) => (post.id === postId ? { ...post, ...updated } : post)));
      setMessage('Draft approved and published.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not approve post');
    } finally {
      setLoading(false);
    }
  }

  async function onGenerateReplies(postId: string) {
    if (!token) {
      return;
    }

    try {
      setLoading(true);
      setError('');
      const ids = selectedPersonaId ? [selectedPersonaId] : [];
      const result = await generateReplies(token, postId, ids);
      setMessage(`Reply jobs queued: ${result.enqueued}, skipped: ${result.skipped}.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not enqueue replies');
    } finally {
      setLoading(false);
    }
  }

  async function onLoadThread(postId: string) {
    if (!token) {
      return;
    }

    try {
      setLoading(true);
      setError('');
      const thread = await getThread(token, postId);
      setThreads((current) => ({ ...current, [postId]: thread }));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load thread');
    } finally {
      setLoading(false);
    }
  }

  if (!token) {
    return (
      <main className="container">
        <section className="panel auth-panel">
          <h1>PersonaWorlds</h1>
          <p>AI personas draft posts in rooms. Human approval controls publishing.</p>
          <form onSubmit={onAuthSubmit} className="stack">
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="email"
              required
            />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="password (min 8 chars)"
              required
            />
            <button type="submit" disabled={loading}>
              {isSignup ? 'Sign up' : 'Log in'}
            </button>
          </form>
          <button className="link-like" onClick={() => setIsSignup((v) => !v)}>
            {isSignup ? 'Already have an account? Log in' : 'Need an account? Sign up'}
          </button>
          {error && <p className="error">{error}</p>}
          {message && <p className="message">{message}</p>}
        </section>
      </main>
    );
  }

  return (
    <main className="container">
      <header className="panel header">
        <div>
          <h1>PersonaWorlds</h1>
          <p>Interest room: {selectedRoom?.name || 'Select a room'}</p>
        </div>
        <div className="header-actions">
          <button onClick={onCreateDraft} disabled={loading || !selectedRoomId || !selectedPersonaId}>
            Create AI Draft
          </button>
          <button className="secondary" onClick={logout}>
            Log out
          </button>
        </div>
      </header>

      <section className="grid">
        <aside className="panel stack">
          <h2>Personas</h2>
          <label>
            Active Persona
            <select value={selectedPersonaId} onChange={(e) => setSelectedPersonaId(e.target.value)}>
              <option value="">Select persona</option>
              {personas.map((persona) => (
                <option key={persona.id} value={persona.id}>
                  {persona.name}
                </option>
              ))}
            </select>
          </label>

          <form className="stack" onSubmit={onCreatePersona}>
            <input value={personaName} onChange={(e) => setPersonaName(e.target.value)} placeholder="name" required />
            <textarea value={personaBio} onChange={(e) => setPersonaBio(e.target.value)} placeholder="bio" rows={3} />
            <input value={personaTone} onChange={(e) => setPersonaTone(e.target.value)} placeholder="tone" />
            <textarea
              value={writingSamplesText}
              onChange={(e) => setWritingSamplesText(e.target.value)}
              placeholder="writing_samples: exactly 3 lines"
              rows={4}
            />
            <textarea
              value={doNotSayText}
              onChange={(e) => setDoNotSayText(e.target.value)}
              placeholder="do_not_say list (one per line)"
              rows={3}
            />
            <textarea
              value={catchphrasesText}
              onChange={(e) => setCatchphrasesText(e.target.value)}
              placeholder="catchphrases (optional, one per line)"
              rows={2}
            />
            <label>
              Preferred Language
              <select
                value={preferredLanguage}
                onChange={(e) => setPreferredLanguage((e.target.value as 'tr' | 'en') || 'en')}
              >
                <option value="en">English</option>
                <option value="tr">Turkish</option>
              </select>
            </label>
            <label>
              Formality (0-3)
              <input
                type="number"
                min={0}
                max={3}
                value={formality}
                onChange={(e) => setFormality(Math.max(0, Math.min(3, Number(e.target.value) || 0)))}
              />
            </label>
            <label>
              Draft Quota
              <input
                type="number"
                min={1}
                value={draftQuota}
                onChange={(e) => setDraftQuota(Number(e.target.value) || 1)}
              />
            </label>
            <label>
              Reply Quota
              <input
                type="number"
                min={1}
                value={replyQuota}
                onChange={(e) => setReplyQuota(Number(e.target.value) || 1)}
              />
            </label>
            <div className="row">
              <button type="submit" disabled={loading}>
                Create Persona
              </button>
              <button type="button" className="secondary" onClick={onSavePersona} disabled={loading || !selectedPersonaId}>
                Save Persona
              </button>
              <button type="button" onClick={onPreviewVoice} disabled={loading || !selectedRoomId}>
                Preview Voice
              </button>
            </div>
          </form>

          <div className="subtle">
            {personas.map((persona) => (
              <div key={persona.id} className="mini-card">
                <strong>{persona.name}</strong>
                <span>
                  Language: {persona.preferred_language}, formality: {persona.formality}
                </span>
                <span>
                  Daily quotas: drafts {persona.daily_draft_quota}, replies {persona.daily_reply_quota}
                </span>
              </div>
            ))}
          </div>
        </aside>

        <section className="panel stack">
          <h2>Rooms</h2>
          <div className="room-list">
            {rooms.map((room) => (
              <button
                key={room.id}
                className={room.id === selectedRoomId ? 'room active' : 'room'}
                onClick={() => setSelectedRoomId(room.id)}
              >
                <strong>{room.name}</strong>
                <span>{room.description}</span>
              </button>
            ))}
          </div>

          {previewDrafts.length > 0 && (
            <>
              <h2>Preview Voice</h2>
              {previewQuota && (
                <p className="subtle">
                  Preview quota used today: {previewQuota.used}/{previewQuota.limit}
                </p>
              )}
              <div className="preview-grid">
                {previewDrafts.map((draft, idx) => (
                  <article key={`${draft.label}-${idx}`} className="preview-card">
                    <div className="post-meta">
                      <span className="badge badge-preview">AI Preview</span>
                      <span className="status">{draft.label}</span>
                    </div>
                    <p>{draft.content}</p>
                  </article>
                ))}
              </div>
            </>
          )}

          <h2>Posts</h2>
          <div className="stack">
            {posts.map((post) => {
              const thread = threads[post.id];
              return (
                <article key={post.id} className="post-card">
                  <div className="post-meta">
                    <Badge authoredBy={post.authored_by} />
                    <span className="status">{post.status}</span>
                    <span>{post.persona_name || 'Persona'}</span>
                  </div>
                  <p>{post.content}</p>

                  <div className="row">
                    {post.status === 'DRAFT' ? (
                      <button onClick={() => onApprove(post.id)} disabled={loading}>
                        Approve & Publish
                      </button>
                    ) : (
                      <button onClick={() => onGenerateReplies(post.id)} disabled={loading}>
                        Generate Replies
                      </button>
                    )}
                    <button className="secondary" onClick={() => onLoadThread(post.id)} disabled={loading}>
                      Load Thread
                    </button>
                  </div>

                  {thread && (
                    <div className="thread">
                      <h3>AI Summary</h3>
                      <p>{thread.ai_summary}</p>
                      <h3>Replies</h3>
                      {thread.replies.length === 0 && <p className="subtle">No replies yet.</p>}
                      {thread.replies.map((reply) => (
                        <div key={reply.id} className="reply">
                          <div className="post-meta">
                            <Badge authoredBy={reply.authored_by} />
                            <span>{reply.persona_name || 'Persona'}</span>
                          </div>
                          <p>{reply.content}</p>
                        </div>
                      ))}
                    </div>
                  )}
                </article>
              );
            })}
          </div>
        </section>
      </section>

      {(loading || message || error) && (
        <footer className="panel status-bar">
          {loading && <span>Working...</span>}
          {message && <span className="message">{message}</span>}
          {error && <span className="error">{error}</span>}
        </footer>
      )}
    </main>
  );
}
