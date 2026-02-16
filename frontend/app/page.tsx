'use client';

import { FormEvent, useEffect, useMemo, useState } from 'react';
import {
  Persona,
  Post,
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
  signup
} from '../lib/api';

const TOKEN_KEY = 'personaworlds_token';

function Badge({ authoredBy }: { authoredBy: Post['authored_by'] | ThreadResponse['replies'][number]['authored_by'] }) {
  const className =
    authoredBy === 'AI'
      ? 'badge badge-ai'
      : authoredBy === 'HUMAN'
        ? 'badge badge-human'
        : 'badge badge-approved';

  return <span className={className}>{authoredBy}</span>;
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
  const [draftQuota, setDraftQuota] = useState(5);
  const [replyQuota, setReplyQuota] = useState(25);

  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const selectedRoom = useMemo(() => rooms.find((r) => r.id === selectedRoomId), [rooms, selectedRoomId]);

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
      const created = await createPersona(token, {
        name: personaName,
        bio: personaBio,
        tone: personaTone,
        daily_draft_quota: draftQuota,
        daily_reply_quota: replyQuota
      });
      setPersonas((current) => [created, ...current]);
      setSelectedPersonaId(created.id);
      setMessage(`Persona created: ${created.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not create persona');
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
            <button type="submit" disabled={loading}>
              Create Persona
            </button>
          </form>

          <div className="subtle">
            {personas.map((persona) => (
              <div key={persona.id} className="mini-card">
                <strong>{persona.name}</strong>
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
