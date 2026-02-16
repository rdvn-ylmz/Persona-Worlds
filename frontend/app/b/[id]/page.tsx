'use client';

import Link from 'next/link';
import { useParams, useSearchParams } from 'next/navigation';
import { useEffect, useMemo, useState } from 'react';
import {
  PublicBattleMeta,
  RemixIntentResponse,
  createBattle,
  createBattleRemixIntent,
  getPublicBattleMeta,
  trackEvent
} from '../../../lib/api';

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080';
const TOKEN_KEY = 'personaworlds_token';
const REMIX_INTENT_KEY = 'personaworlds_pending_remix_intent';
const PREFERRED_TEMPLATE_KEY = 'personaworlds_preferred_template_id';

type StoredRemixIntent = RemixIntentResponse & {
  saved_at: string;
};

function getStoredToken() {
  if (typeof window === 'undefined') {
    return '';
  }
  return (localStorage.getItem(TOKEN_KEY) || '').trim();
}

function readStoredRemixIntent(): StoredRemixIntent | null {
  if (typeof window === 'undefined') {
    return null;
  }
  const raw = localStorage.getItem(REMIX_INTENT_KEY);
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as StoredRemixIntent;
    if (!parsed || typeof parsed !== 'object') {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

function writeStoredRemixIntent(intent: RemixIntentResponse) {
  if (typeof window === 'undefined') {
    return;
  }
  const payload: StoredRemixIntent = {
    ...intent,
    saved_at: new Date().toISOString()
  };
  localStorage.setItem(REMIX_INTENT_KEY, JSON.stringify(payload));
}

function clearStoredRemixIntent() {
  if (typeof window === 'undefined') {
    return;
  }
  localStorage.removeItem(REMIX_INTENT_KEY);
}

function readPreferredTemplateID() {
  if (typeof window === 'undefined') {
    return '';
  }
  return (localStorage.getItem(PREFERRED_TEMPLATE_KEY) || '').trim();
}

function clearPreferredTemplateID() {
  if (typeof window === 'undefined') {
    return;
  }
  localStorage.removeItem(PREFERRED_TEMPLATE_KEY);
}

async function loadBattleCardBlob(cardURL: string) {
  const response = await fetch(cardURL, {
    method: 'GET',
    cache: 'no-store'
  });
  if (!response.ok) {
    throw new Error(`card request failed with status ${response.status}`);
  }
  return response.blob();
}

function downloadBlob(blob: Blob, filename: string) {
  const objectURL = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = objectURL;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  URL.revokeObjectURL(objectURL);
}

export default function BattleCardPage() {
  const params = useParams<{ id: string }>();
  const searchParams = useSearchParams();
  const battleID = useMemo(() => (params?.id || '').toString().trim(), [params]);
  const cardURL = useMemo(() => `${API_BASE}/b/${encodeURIComponent(battleID)}/card.png`, [battleID]);

  const [token, setToken] = useState('');
  const [meta, setMeta] = useState<PublicBattleMeta | null>(null);
  const [remixIntent, setRemixIntent] = useState<RemixIntentResponse | null>(null);

  const [loadingMeta, setLoadingMeta] = useState(true);
  const [working, setWorking] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [submittingRemix, setSubmittingRemix] = useState(false);
  const [showRemixModal, setShowRemixModal] = useState(false);
  const [autoRemixChecked, setAutoRemixChecked] = useState(false);

  const [topic, setTopic] = useState('');
  const [proStyle, setProStyle] = useState('');
  const [conStyle, setConStyle] = useState('');
  const [selectedTemplateID, setSelectedTemplateID] = useState('');

  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [previewError, setPreviewError] = useState(false);

  const battleURL = useMemo(() => {
    if (!battleID || typeof window === 'undefined') {
      return '';
    }
    return `${window.location.origin}/b/${encodeURIComponent(battleID)}`;
  }, [battleID]);

  useEffect(() => {
    setToken(getStoredToken());
  }, []);

  useEffect(() => {
    if (!battleID) {
      setLoadingMeta(false);
      setMeta(null);
      setError('Battle id is missing.');
      return;
    }

    let cancelled = false;
    const load = async () => {
      try {
        setLoadingMeta(true);
        setError('');
        const battleMeta = await getPublicBattleMeta(battleID);
        if (!cancelled) {
          setMeta(battleMeta);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'could not load battle');
        }
      } finally {
        if (!cancelled) {
          setLoadingMeta(false);
        }
      }
    };

    void load();
    return () => {
      cancelled = true;
    };
  }, [battleID]);

  useEffect(() => {
    if (autoRemixChecked) {
      return;
    }
    if (!battleID) {
      setAutoRemixChecked(true);
      return;
    }
    if ((searchParams.get('remix') || '').trim() !== '1') {
      setAutoRemixChecked(true);
      return;
    }
    if (!token) {
      setAutoRemixChecked(true);
      return;
    }

    const queryPreferredTemplate = (searchParams.get('template') || '').trim();
    const preferredTemplateID = queryPreferredTemplate || readPreferredTemplateID() || meta?.template?.id || '';

    setAutoRemixChecked(true);
    const stored = readStoredRemixIntent();
    if (stored && stored.battle_id === battleID) {
      applyRemixIntent(stored, true, preferredTemplateID);
      clearStoredRemixIntent();
      clearPreferredTemplateID();
      return;
    }
    void startRemixFlow({
      trackClick: false,
      preferredTemplateID,
      autoStart: true
    });
  }, [autoRemixChecked, battleID, meta?.template?.id, searchParams, token]);

  function applyRemixIntent(intent: RemixIntentResponse, openModal: boolean, preferredTemplateID = '') {
    setRemixIntent(intent);
    setTopic(intent.topic || '');
    setProStyle(intent.pro_style || '');
    setConStyle(intent.con_style || '');

    const suggested = intent.suggested_templates || [];
    const preferredExists = preferredTemplateID && suggested.some((item) => item.id === preferredTemplateID);
    if (preferredExists) {
      setSelectedTemplateID(preferredTemplateID);
    } else {
      setSelectedTemplateID(suggested[0]?.id || '');
    }

    if (openModal) {
      setShowRemixModal(true);
    }
  }

  async function startRemixFlow(options: { trackClick: boolean; preferredTemplateID?: string; autoStart?: boolean }) {
    if (!battleID) {
      return;
    }

    try {
      setWorking(true);
      setError('');
      setMessage('');

      if (options.trackClick) {
        void trackEvent(
          'remix_clicked',
          {
            battle_id: battleID,
            source: 'battle_page'
          },
          token || undefined
        ).catch(() => undefined);
      }

      const intent = await createBattleRemixIntent(battleID, token || undefined);
      if (!token) {
        writeStoredRemixIntent(intent);
        const nextValue = `/b/${encodeURIComponent(battleID)}`;
        window.location.href = `/signup?next=${encodeURIComponent(nextValue)}&remix=1`;
        return;
      }

      applyRemixIntent(intent, true, options.preferredTemplateID || '');
      void trackEvent(
        'remix_started',
        {
          battle_id: battleID,
          source: options.autoStart ? 'auto_resume' : 'battle_page'
        },
        token || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not start remix');
    } finally {
      setWorking(false);
    }
  }

  async function onCopyImage() {
    if (!battleID) {
      return;
    }
    try {
      setSharing(true);
      setError('');
      setMessage('');

      const blob = await loadBattleCardBlob(cardURL);
      const hasClipboardImageSupport =
        typeof window !== 'undefined' && 'ClipboardItem' in window && Boolean(navigator.clipboard?.write);

      if (hasClipboardImageSupport) {
        const item = new (window as any).ClipboardItem({
          'image/png': blob
        });
        await navigator.clipboard.write([item]);
        setMessage('Battle card image copied to clipboard.');
      } else {
        downloadBlob(blob, `battle-${battleID}.png`);
        setMessage('Clipboard image is unavailable, card downloaded instead.');
      }

      void trackEvent(
        'battle_shared',
        {
          source: 'battle_page_copy_image',
          battle_id: battleID
        },
        token || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not copy battle card image');
    } finally {
      setSharing(false);
    }
  }

  async function onShare() {
    if (!battleID) {
      return;
    }

    try {
      setSharing(true);
      setError('');
      setMessage('');

      const shareURL = battleURL || `${window.location.origin}/b/${encodeURIComponent(battleID)}`;
      const shareText = 'Battle card summary';

      if (navigator.share) {
        let shared = false;
        try {
          const blob = await loadBattleCardBlob(cardURL);
          const file = new File([blob], `battle-${battleID}.png`, { type: 'image/png' });
          if (navigator.canShare && navigator.canShare({ files: [file] })) {
            await navigator.share({
              title: 'Battle Card',
              text: shareText,
              url: shareURL,
              files: [file]
            });
            shared = true;
          }
        } catch {
          // Fall back to URL share.
        }

        if (!shared) {
          await navigator.share({
            title: 'Battle Card',
            text: shareText,
            url: shareURL
          });
        }

        setMessage('Share dialog opened.');
      } else if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(shareURL);
        setMessage('Share is not supported here, battle link copied instead.');
      } else {
        setError('Share is unavailable in this browser.');
        return;
      }

      void trackEvent(
        'battle_shared',
        {
          source: 'battle_page_share',
          battle_id: battleID
        },
        token || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not share battle card');
    } finally {
      setSharing(false);
    }
  }

  async function onCopyLink() {
    if (!battleID || !battleURL || !navigator.clipboard?.writeText) {
      return;
    }

    try {
      setSharing(true);
      setError('');
      setMessage('');
      await navigator.clipboard.writeText(battleURL);
      setMessage('Battle link copied.');

      void trackEvent(
        'battle_shared',
        {
          source: 'battle_page_copy_link',
          battle_id: battleID
        },
        token || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not copy link');
    } finally {
      setSharing(false);
    }
  }

  async function onSubmitRemix() {
    if (!battleID) {
      return;
    }
    if (!remixIntent) {
      setError('remix intent missing, retry remix');
      return;
    }
    if (!token) {
      writeStoredRemixIntent(remixIntent);
      const nextValue = `/b/${encodeURIComponent(battleID)}`;
      window.location.href = `/signup?next=${encodeURIComponent(nextValue)}&remix=1`;
      return;
    }
    if (!topic.trim()) {
      setError('topic is required');
      return;
    }

    try {
      setSubmittingRemix(true);
      setError('');
      setMessage('');

      const created = await createBattle(token, remixIntent.room_id, {
        topic: topic.trim(),
        template_id: selectedTemplateID || undefined,
        remix_token: remixIntent.remix_token,
        pro_style: proStyle.trim(),
        con_style: conStyle.trim()
      });

      void trackEvent(
        'remix_completed',
        {
          source_battle_id: battleID,
          battle_id: created.battle_id,
          room_id: remixIntent.room_id
        },
        token
      ).catch(() => undefined);

      clearStoredRemixIntent();
      clearPreferredTemplateID();
      setShowRemixModal(false);
      window.location.href = `/b/${encodeURIComponent(created.battle_id)}`;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not complete remix');
    } finally {
      setSubmittingRemix(false);
    }
  }

  if (!battleID) {
    return (
      <main className="container">
        <section className="panel stack">
          <h1>Battle</h1>
          <p className="error">Battle id is missing.</p>
          <Link className="primary-link" href="/">
            Go to dashboard
          </Link>
        </section>
      </main>
    );
  }

  return (
    <main className="container">
      <section className="panel battle-card-page stack">
        <div className="battle-card-head">
          <div className="stack">
            <h1>Battle</h1>
            <p className="subtle">Battle ID: {battleID}</p>
            {loadingMeta && <p className="subtle">Loading battle details...</p>}
            {!loadingMeta && meta && (
              <>
                <p>{meta.topic}</p>
                <p className="subtle">Room: {meta.room_name}</p>
                {meta.template && (
                  <p className="subtle">
                    Made with template:{' '}
                    <Link href={`/templates?template=${encodeURIComponent(meta.template.id)}&battle=${encodeURIComponent(battleID)}`}>
                      {meta.template.name}
                    </Link>
                  </p>
                )}
              </>
            )}
          </div>
          <Link className="primary-link" href="/">
            Dashboard
          </Link>
        </div>

        <div className="battle-card-actions row">
          <button
            type="button"
            className="battle-remix-primary"
            onClick={() =>
              void startRemixFlow({
                trackClick: true,
                preferredTemplateID: meta?.template?.id || ''
              })
            }
            disabled={working}
          >
            Remix this battle
          </button>
          {meta?.template && (
            <button
              type="button"
              className="secondary"
              onClick={() =>
                void startRemixFlow({
                  trackClick: true,
                  preferredTemplateID: meta.template?.id || ''
                })
              }
              disabled={working}
            >
              Use this template
            </button>
          )}
          <button type="button" className="secondary" onClick={onCopyImage} disabled={sharing}>
            Copy image
          </button>
          <button type="button" className="secondary" onClick={onShare} disabled={sharing}>
            Share
          </button>
          <button type="button" className="secondary" onClick={onCopyLink} disabled={sharing || !battleURL}>
            Copy link
          </button>
        </div>

        <div className="battle-card-preview">
          {!previewError && (
            <img src={cardURL} alt="Battle card preview" loading="lazy" onError={() => setPreviewError(true)} />
          )}
          {previewError && (
            <p className="error">
              Could not load preview. You can still download directly from <code>/b/{battleID}/card.png</code>.
            </p>
          )}
        </div>

        {(message || error) && (
          <div className="status-bar">
            {message && <span className="message">{message}</span>}
            {error && <span className="error">{error}</span>}
          </div>
        )}
      </section>

      {showRemixModal && remixIntent && (
        <div className="modal-backdrop" onClick={() => setShowRemixModal(false)}>
          <section className="modal-panel stack" onClick={(event) => event.stopPropagation()}>
            <h2>Remix Battle</h2>
            <p className="subtle">Room: {remixIntent.room_name}</p>
            <label>
              Topic
              <textarea value={topic} onChange={(event) => setTopic(event.target.value)} rows={3} />
            </label>
            <label>
              Pro stance style
              <input value={proStyle} onChange={(event) => setProStyle(event.target.value)} />
            </label>
            <label>
              Con stance style
              <input value={conStyle} onChange={(event) => setConStyle(event.target.value)} />
            </label>
            <label>
              Template
              <select value={selectedTemplateID} onChange={(event) => setSelectedTemplateID(event.target.value)}>
                {remixIntent.suggested_templates.map((template) => (
                  <option key={template.id} value={template.id}>
                    {template.name} ({template.turn_count} turns, {template.word_limit} words)
                  </option>
                ))}
              </select>
            </label>
            <div className="row">
              <button type="button" onClick={onSubmitRemix} disabled={submittingRemix}>
                {submittingRemix ? 'Creating...' : 'Create remixed battle'}
              </button>
              <button type="button" className="secondary" onClick={() => setShowRemixModal(false)} disabled={submittingRemix}>
                Cancel
              </button>
            </div>
          </section>
        </div>
      )}
    </main>
  );
}
