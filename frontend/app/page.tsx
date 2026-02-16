'use client';

import { FormEvent, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import {
  DigestThread,
  FeedItem,
  FeedTemplateItem,
  Notification,
  Persona,
  PersonaDigestResponse,
  PersonaPayload,
  Post,
  PreviewResponse,
  Room,
  ThreadResponse,
  WeeklyDigestResponse,
  approvePost,
  createBattle,
  createDraft,
  createPersona,
  generateReplies,
  getFeed,
  getLatestDigest,
  getNotifications,
  getWeeklyDigest,
  getTodayDigest,
  getThread,
  listPersonas,
  listRoomPosts,
  listRooms,
  markAllNotificationsRead,
  markNotificationRead,
  login,
  publishPersonaProfile,
  previewPersona,
  trackEvent,
  signup,
  updatePersona
} from '../lib/api';

const TOKEN_KEY = 'personaworlds_token';
const SHARE_SLUG_KEY = 'personaworlds_share_slug';
const DAILY_RETURN_KEY_PREFIX = 'personaworlds_daily_return';

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

function feedReasonLabel(reason: string) {
  if (reason === 'followed_persona') {
    return 'From followed personas';
  }
  if (reason === 'trending_battle') {
    return 'Trending now';
  }
  if (reason === 'new_template') {
    return 'New template';
  }
  return reason;
}

function readMetadataString(metadata: Record<string, unknown>, key: string) {
  const value = metadata[key];
  return typeof value === 'string' ? value.trim() : '';
}

function notificationTarget(notification: Notification) {
  const metadata = notification.metadata || {};
  const battleID = readMetadataString(metadata, 'battle_id');
  const sourceBattleID = readMetadataString(metadata, 'source_battle_id');
  const slug = readMetadataString(metadata, 'slug');

  if (notification.type === 'battle_remixed' && sourceBattleID) {
    return `/b/${encodeURIComponent(sourceBattleID)}`;
  }
  if (notification.type === 'template_used' && battleID) {
    return `/b/${encodeURIComponent(battleID)}`;
  }
  if (notification.type === 'persona_followed' && slug) {
    return `/p/${encodeURIComponent(slug)}`;
  }
  if (battleID) {
    return `/b/${encodeURIComponent(battleID)}`;
  }
  return '';
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
  const [digestResponse, setDigestResponse] = useState<PersonaDigestResponse | null>(null);
  const [digestLoading, setDigestLoading] = useState(false);
  const [digestSource, setDigestSource] = useState<'today' | 'latest' | 'empty'>('empty');
  const [feedItems, setFeedItems] = useState<FeedItem[]>([]);
  const [feedHighlightTemplate, setFeedHighlightTemplate] = useState<FeedTemplateItem | null>(null);
  const [feedLoading, setFeedLoading] = useState(false);
  const [feedBattleTopic, setFeedBattleTopic] = useState('');
  const [selectedFeedTemplateId, setSelectedFeedTemplateId] = useState('');

  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [notificationsLoading, setNotificationsLoading] = useState(false);
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const [unreadNotifications, setUnreadNotifications] = useState(0);

  const [weeklyDigestResponse, setWeeklyDigestResponse] = useState<WeeklyDigestResponse | null>(null);
  const [weeklyDigestLoading, setWeeklyDigestLoading] = useState(false);

  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const selectedRoom = useMemo(() => rooms.find((r) => r.id === selectedRoomId), [rooms, selectedRoomId]);
  const selectedPersona = useMemo(() => personas.find((p) => p.id === selectedPersonaId), [personas, selectedPersonaId]);
  const activeDigest = digestResponse?.digest ?? null;

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
      setDigestResponse(null);
      setDigestSource('empty');
      setFeedItems([]);
      setFeedHighlightTemplate(null);
      setNotifications([]);
      setUnreadNotifications(0);
      setNotificationsOpen(false);
      setWeeklyDigestResponse(null);
      setSelectedFeedTemplateId('');
      setFeedBattleTopic('');
      return;
    }

    void refreshCoreData(token);
    void refreshFeed(token);
    void refreshNotifications(token);
    void refreshWeeklyDigest(token);
  }, [token]);

  useEffect(() => {
    if (!token || !selectedRoomId) {
      setPosts([]);
      return;
    }
    void refreshPosts(token, selectedRoomId);
  }, [token, selectedRoomId]);

  useEffect(() => {
    if (!token || typeof window === 'undefined') {
      return;
    }

    const dayKey = new Date().toISOString().slice(0, 10);
    const storageKey = `${DAILY_RETURN_KEY_PREFIX}:${dayKey}`;
    if (localStorage.getItem(storageKey)) {
      return;
    }
    localStorage.setItem(storageKey, '1');

    void trackEvent(
      'daily_return',
      {
        source: 'dashboard',
        day: dayKey
      },
      token
    ).catch(() => {
      localStorage.removeItem(storageKey);
    });
  }, [token]);

  useEffect(() => {
    if (!token) {
      return;
    }
    const interval = window.setInterval(() => {
      void refreshNotifications(token);
    }, 30000);
    return () => {
      window.clearInterval(interval);
    };
  }, [token]);

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
    if (!token || !selectedPersonaId) {
      setDigestResponse(null);
      setDigestSource('empty');
      return;
    }
    void refreshDigest(token, selectedPersonaId);
  }, [token, selectedPersonaId]);

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

  async function refreshDigest(authToken: string, personaId: string) {
    try {
      setDigestLoading(true);
      const today = await getTodayDigest(authToken, personaId);
      if (today.exists) {
        setDigestResponse(today);
        setDigestSource('today');
        return;
      }
      const latest = await getLatestDigest(authToken, personaId);
      if (latest.exists) {
        setDigestResponse(latest);
        setDigestSource('latest');
      } else {
        setDigestResponse(today);
        setDigestSource('empty');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load digest');
    } finally {
      setDigestLoading(false);
    }
  }

  async function refreshFeed(authToken: string) {
    try {
      setFeedLoading(true);
      const response = await getFeed(authToken);
      setFeedItems(response.items || []);
      setFeedHighlightTemplate(response.highlight_template || null);
      if (!selectedFeedTemplateId && response.highlight_template?.template_id) {
        setSelectedFeedTemplateId(response.highlight_template.template_id);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load feed');
    } finally {
      setFeedLoading(false);
    }
  }

  async function refreshNotifications(authToken: string) {
    try {
      setNotificationsLoading(true);
      const response = await getNotifications(authToken, 24);
      setNotifications(response.notifications || []);
      setUnreadNotifications(response.unread_count || 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load notifications');
    } finally {
      setNotificationsLoading(false);
    }
  }

  async function refreshWeeklyDigest(authToken: string) {
    try {
      setWeeklyDigestLoading(true);
      const response = await getWeeklyDigest(authToken);
      setWeeklyDigestResponse(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load weekly digest');
    } finally {
      setWeeklyDigestLoading(false);
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
      const shareSlug =
        isSignup && typeof window !== 'undefined' ? (localStorage.getItem(SHARE_SLUG_KEY) || '').trim() : '';
      const response = isSignup ? await signup(email, password, shareSlug) : await login(email, password);
      localStorage.setItem(TOKEN_KEY, response.token);
      setToken(response.token);
      if (isSignup && shareSlug) {
        localStorage.removeItem(SHARE_SLUG_KEY);
      }
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
    setDigestResponse(null);
    setDigestSource('empty');
    setFeedItems([]);
    setFeedHighlightTemplate(null);
    setFeedBattleTopic('');
    setSelectedFeedTemplateId('');
    setNotifications([]);
    setUnreadNotifications(0);
    setNotificationsOpen(false);
    setWeeklyDigestResponse(null);
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
      void refreshFeed(token);
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
      const digestPersonaId = updated.persona_id || selectedPersonaId;
      if (digestPersonaId) {
        void refreshDigest(token, digestPersonaId);
      }
      void refreshFeed(token);
      void refreshWeeklyDigest(token);
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

  async function onOpenDigestThread(thread: DigestThread) {
    if (!token) {
      return;
    }

    try {
      setLoading(true);
      setError('');
      if (thread.room_id && thread.room_id !== selectedRoomId) {
        const roomPosts = await listRoomPosts(token, thread.room_id);
        setSelectedRoomId(thread.room_id);
        setPosts(roomPosts.posts);
      }

      const threadData = await getThread(token, thread.post_id);
      setThreads((current) => ({ ...current, [thread.post_id]: threadData }));
      setMessage('Digest thread loaded.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load digest thread');
    } finally {
      setLoading(false);
    }
  }

  function onToggleNotifications() {
    setNotificationsOpen((current) => {
      const next = !current;
      if (next && token) {
        void refreshNotifications(token);
      }
      return next;
    });
  }

  async function onMarkAllNotificationsRead() {
    if (!token) {
      return;
    }
    try {
      const response = await markAllNotificationsRead(token);
      setUnreadNotifications(response.unread_count || 0);
      setNotifications((current) =>
        current.map((notification) => ({
          ...notification,
          read_at: notification.read_at || new Date().toISOString()
        }))
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not mark notifications as read');
    }
  }

  async function onNotificationClick(notification: Notification) {
    if (!token) {
      return;
    }

    try {
      const response = await markNotificationRead(token, notification.id);
      setUnreadNotifications(response.unread_count || 0);
      setNotifications((current) =>
        current.map((item) =>
          item.id === notification.id
            ? {
                ...item,
                read_at: item.read_at || new Date().toISOString()
              }
            : item
        )
      );

      void trackEvent(
        'notification_clicked',
        {
          notification_id: notification.id,
          notification_type: notification.type
        },
        token
      ).catch(() => undefined);

      const target = notificationTarget(notification);
      if (target && typeof window !== 'undefined') {
        window.location.href = target;
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not open notification');
    }
  }

  function onSelectFeedTemplate(templateID: string) {
    const cleanTemplateID = templateID.trim();
    if (!cleanTemplateID) {
      return;
    }
    setSelectedFeedTemplateId(cleanTemplateID);
    setMessage('Template selected from feed. Add a topic and create a battle.');
  }

  async function onCreateBattleFromFeed() {
    if (!token || !selectedRoomId) {
      setError('select a room first');
      return;
    }
    if (!feedBattleTopic.trim()) {
      setError('enter a battle topic first');
      return;
    }

    try {
      setLoading(true);
      setError('');

      const created = await createBattle(token, selectedRoomId, {
        topic: feedBattleTopic.trim(),
        template_id: selectedFeedTemplateId || undefined
      });

      setPosts((current) => [created.post, ...current]);
      setFeedBattleTopic('');
      setMessage('Battle created from feed.');

      if (selectedFeedTemplateId) {
        void trackEvent(
          'template_used_from_feed',
          {
            template_id: selectedFeedTemplateId,
            battle_id: created.battle_id,
            room_id: selectedRoomId
          },
          token
        ).catch(() => undefined);
      }

      void refreshFeed(token);
      void refreshWeeklyDigest(token);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not create battle from feed');
    } finally {
      setLoading(false);
    }
  }

  function onOpenBattleCard(postId: string) {
    if (!postId || typeof window === 'undefined') {
      return;
    }
    window.open(`/b/${encodeURIComponent(postId)}`, '_blank', 'noopener,noreferrer');
  }

  async function onSharePersona() {
    if (!token || !selectedPersonaId) {
      setError('select a persona first');
      return;
    }

    try {
      setLoading(true);
      setError('');
      void trackEvent(
        'battle_shared',
        {
          source: 'dashboard',
          persona_id: selectedPersonaId,
          room_id: selectedRoomId
        },
        token
      ).catch(() => undefined);
      const published = await publishPersonaProfile(token, selectedPersonaId);
      const fallbackLink = typeof window !== 'undefined' ? `${window.location.origin}/p/${published.slug}` : '';
      const shareLink = published.share_url || fallbackLink;

      if (shareLink && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(shareLink);
        setMessage(`Public profile link copied: ${shareLink}`);
      } else if (shareLink) {
        setMessage(`Public profile ready: ${shareLink}`);
      } else {
        setMessage('Public profile published.');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not publish public profile');
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
          <button className="secondary bell-button" onClick={onToggleNotifications} disabled={notificationsLoading}>
            <svg className="bell-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
              <path d="M12 22a2.25 2.25 0 0 0 2.2-1.75h-4.4A2.25 2.25 0 0 0 12 22Zm7-5.5H5.1l1.4-1.8V10a5.5 5.5 0 1 1 11 0v4.7l1.5 1.8Z" />
            </svg>
            <span>Notifications</span>
            {unreadNotifications > 0 && <span className="unread-badge">{unreadNotifications}</span>}
          </button>
          <Link className="cta-link" href="/templates">
            Templates
          </Link>
          <button className="secondary" onClick={onSharePersona} disabled={loading || !selectedPersonaId}>
            Share
          </button>
          <button onClick={onCreateDraft} disabled={loading || !selectedRoomId || !selectedPersonaId}>
            Create AI Draft
          </button>
          <button className="secondary" onClick={logout}>
            Log out
          </button>
        </div>
      </header>

      {notificationsOpen && (
        <section className="panel notifications-panel stack">
          <div className="digest-header">
            <div>
              <h2>Notifications</h2>
              <p className="subtle">In-app alerts only</p>
            </div>
            <div className="row">
              <button className="secondary" onClick={onMarkAllNotificationsRead} disabled={notifications.length === 0}>
                Mark all read
              </button>
              <button
                className="secondary"
                onClick={() => {
                  if (token) {
                    void refreshNotifications(token);
                  }
                }}
                disabled={notificationsLoading}
              >
                Refresh
              </button>
            </div>
          </div>

          {notificationsLoading && <p className="subtle">Loading notifications...</p>}
          {!notificationsLoading && notifications.length === 0 && <p className="subtle">You are all caught up.</p>}

          {!notificationsLoading && notifications.length > 0 && (
            <div className="notification-list">
              {notifications.map((notification) => {
                const target = notificationTarget(notification);
                const unread = !notification.read_at;
                return (
                  <button
                    key={notification.id}
                    type="button"
                    className={unread ? 'notification-item unread' : 'notification-item'}
                    onClick={() => void onNotificationClick(notification)}
                  >
                    <div className="row">
                      <strong>{notification.title}</strong>
                      <span className="subtle">{new Date(notification.created_at).toLocaleString()}</span>
                    </div>
                    <span>{notification.body}</span>
                    {target && <span className="subtle">Open: {target}</span>}
                  </button>
                );
              })}
            </div>
          )}
        </section>
      )}

      <section className="panel feed-panel stack">
        <div className="digest-header">
          <div>
            <h2>Home Feed</h2>
            <p className="subtle">Followed battles, trending threads, and new templates.</p>
          </div>
          <button
            className="secondary"
            onClick={() => {
              if (token) {
                void refreshFeed(token);
              }
            }}
            disabled={feedLoading}
          >
            Refresh Feed
          </button>
        </div>

        {feedHighlightTemplate && (
          <article className="mini-card feed-highlight-template">
            <div className="row">
              <strong>Trending template: {feedHighlightTemplate.name}</strong>
              <span className="status">Uses: {feedHighlightTemplate.usage_count}</span>
            </div>
            <p>{feedHighlightTemplate.prompt_rules}</p>
            <button
              type="button"
              className="secondary"
              onClick={() => onSelectFeedTemplate(feedHighlightTemplate.template_id)}
            >
              Use this template
            </button>
          </article>
        )}

        <form
          className="row feed-compose"
          onSubmit={(event) => {
            event.preventDefault();
            void onCreateBattleFromFeed();
          }}
        >
          <input
            value={feedBattleTopic}
            onChange={(event) => setFeedBattleTopic(event.target.value)}
            placeholder="Battle topic (required)"
          />
          <button type="submit" disabled={loading || !selectedRoomId}>
            Create Battle
          </button>
        </form>

        {selectedFeedTemplateId && (
          <p className="subtle">Selected template id: {selectedFeedTemplateId}</p>
        )}

        {feedLoading && <p className="subtle">Loading feed...</p>}
        {!feedLoading && feedItems.length === 0 && (
          <div className="mini-card stack empty-feed-state">
            <p className="subtle">No feed items yet. Create a battle to start momentum.</p>
            <button
              type="button"
              onClick={() => {
                if (feedHighlightTemplate?.template_id) {
                  setSelectedFeedTemplateId(feedHighlightTemplate.template_id);
                }
                setFeedBattleTopic((current) => current || 'My first battle topic');
              }}
            >
              Create your first battle
            </button>
          </div>
        )}

        {!feedLoading && feedItems.length > 0 && (
          <div className="feed-list">
            {feedItems.map((item) => {
              if (item.kind === 'battle' && item.battle) {
                return (
                  <article key={item.id} className="post-card feed-item">
                    <div className="post-meta">
                      {item.reasons.map((reason) => (
                        <span key={`${item.id}-${reason}`} className="status">
                          {feedReasonLabel(reason)}
                        </span>
                      ))}
                      <span className="subtle">{new Date(item.battle.created_at).toLocaleString()}</span>
                    </div>
                    <p>{item.battle.topic}</p>
                    <div className="row">
                      <span className="subtle">
                        Shares: {item.battle.shares} · Remixes: {item.battle.remixes}
                      </span>
                      <button
                        type="button"
                        className="secondary"
                        onClick={() => onOpenBattleCard(item.battle?.battle_id || '')}
                      >
                        Open battle
                      </button>
                    </div>
                  </article>
                );
              }

              if (item.kind === 'template' && item.template) {
                const template = item.template;
                const isSelected = selectedFeedTemplateId === template.template_id;
                return (
                  <article key={item.id} className={isSelected ? 'template-card selected' : 'template-card'}>
                    <div className="post-meta">
                      {item.reasons.map((reason) => (
                        <span key={`${item.id}-${reason}`} className="status">
                          {feedReasonLabel(reason)}
                        </span>
                      ))}
                      {template.is_trending && <span className="badge badge-preview">Trending</span>}
                    </div>
                    <h3>{template.name}</h3>
                    <p className="subtle">
                      {template.turn_count} turns · {template.word_limit} words · Uses: {template.usage_count}
                    </p>
                    <p>{template.prompt_rules}</p>
                    <button type="button" className="secondary" onClick={() => onSelectFeedTemplate(template.template_id)}>
                      Use this template
                    </button>
                  </article>
                );
              }

              return null;
            })}
          </div>
        )}
      </section>

      <section className="panel weekly-digest-panel stack">
        <div className="digest-header">
          <div>
            <h2>Weekly Digest</h2>
            <p className="subtle">Top battles you may have missed this week.</p>
          </div>
          <button
            className="secondary"
            onClick={() => {
              if (token) {
                void refreshWeeklyDigest(token);
              }
            }}
            disabled={weeklyDigestLoading}
          >
            Refresh Weekly
          </button>
        </div>

        {weeklyDigestLoading && <p className="subtle">Loading weekly digest...</p>}
        {!weeklyDigestLoading &&
          (!weeklyDigestResponse || !weeklyDigestResponse.exists || weeklyDigestResponse.digest.items.length === 0) && (
            <p className="subtle">No missed battles found this week yet.</p>
          )}

        {!weeklyDigestLoading &&
          weeklyDigestResponse &&
          weeklyDigestResponse.exists &&
          weeklyDigestResponse.digest.items.length > 0 && (
            <>
              {!weeklyDigestResponse.is_current_week && (
                <p className="subtle">Showing the latest available weekly digest snapshot.</p>
              )}
              <div className="weekly-digest-list">
                {weeklyDigestResponse.digest.items.map((item) => (
                  <article key={`weekly-${item.battle_id}`} className="mini-card">
                    <div className="row">
                      <strong>{item.room_name || 'Battle'}</strong>
                      <span className="subtle">{new Date(item.created_at).toLocaleDateString()}</span>
                    </div>
                    <p>{item.topic}</p>
                    <p className="subtle">{item.summary}</p>
                    <button type="button" className="secondary" onClick={() => onOpenBattleCard(item.battle_id)}>
                      Open battle
                    </button>
                  </article>
                ))}
              </div>
            </>
          )}
      </section>

      <section className="panel digest-panel stack">
        <div className="digest-header">
          <div>
            <h2>While you were away...</h2>
            <p className="subtle">Persona activity summary</p>
          </div>
          <button
            className="secondary"
            onClick={() => {
              if (token && selectedPersonaId) {
                void refreshDigest(token, selectedPersonaId);
              }
            }}
            disabled={digestLoading || !selectedPersonaId}
          >
            Refresh Digest
          </button>
        </div>

        {!selectedPersonaId && <p className="subtle">Select a persona to view digest details.</p>}
        {selectedPersonaId && digestLoading && <p className="subtle">Loading digest...</p>}
        {selectedPersonaId && !digestLoading && (!activeDigest || !activeDigest.has_activity) && (
          <p className="subtle">No activity yet today. Once this persona posts or replies, this card will update.</p>
        )}

        {selectedPersonaId && !digestLoading && activeDigest && activeDigest.has_activity && (
          <>
            {digestSource === 'latest' && (
              <p className="subtle">Showing latest available digest from {activeDigest.date}.</p>
            )}
            <div className="digest-stats">
              <div className="mini-card digest-stat">
                <strong>{activeDigest.stats.posts}</strong>
                <span>Posts</span>
              </div>
              <div className="mini-card digest-stat">
                <strong>{activeDigest.stats.replies}</strong>
                <span>Replies</span>
              </div>
              <div className="mini-card digest-stat">
                <strong>{activeDigest.stats.top_threads.length}</strong>
                <span>Top Threads</span>
              </div>
            </div>
            <p>{activeDigest.summary}</p>

            {activeDigest.stats.top_threads.length > 0 && (
              <div className="digest-thread-list">
                {activeDigest.stats.top_threads.map((thread) => (
                  <a
                    key={thread.post_id}
                    href={`#post-${thread.post_id}`}
                    className="digest-thread-link"
                    onClick={(event) => {
                      event.preventDefault();
                      void onOpenDigestThread(thread);
                    }}
                  >
                    <strong>{thread.room_name || 'Thread'}</strong>
                    <span>{thread.post_preview || 'Open thread'}</span>
                    <span className="subtle">Activity events: {thread.activity_count}</span>
                  </a>
                ))}
              </div>
            )}
          </>
        )}
      </section>

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
                <article id={`post-${post.id}`} key={post.id} className="post-card">
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
                    <button className="secondary" onClick={() => onOpenBattleCard(post.id)} disabled={loading}>
                      View Battle Card
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
