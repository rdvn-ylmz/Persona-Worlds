'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useMemo, useState } from 'react';
import { trackEvent } from '../../../lib/api';

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080';
const TOKEN_KEY = 'personaworlds_token';

function getStoredToken() {
  if (typeof window === 'undefined') {
    return '';
  }
  return (localStorage.getItem(TOKEN_KEY) || '').trim();
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
  const battleID = useMemo(() => (params?.id || '').toString().trim(), [params]);
  const cardURL = useMemo(() => `${API_BASE}/b/${encodeURIComponent(battleID)}/card.png`, [battleID]);

  const [working, setWorking] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [previewError, setPreviewError] = useState(false);

  const battleURL = useMemo(() => {
    if (!battleID || typeof window === 'undefined') {
      return '';
    }
    return `${window.location.origin}/b/${encodeURIComponent(battleID)}`;
  }, [battleID]);

  async function onCopyImage() {
    if (!battleID) {
      return;
    }
    try {
      setWorking(true);
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
        getStoredToken() || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not copy battle card image');
    } finally {
      setWorking(false);
    }
  }

  async function onShare() {
    if (!battleID) {
      return;
    }

    try {
      setWorking(true);
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
          // Fall back to URL-only share if file share is not available.
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
        return
      }

      void trackEvent(
        'battle_shared',
        {
          source: 'battle_page_share',
          battle_id: battleID
        },
        getStoredToken() || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not share battle card');
    } finally {
      setWorking(false);
    }
  }

  async function onCopyLink() {
    if (!battleID || !battleURL || !navigator.clipboard?.writeText) {
      return;
    }

    try {
      setWorking(true);
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
        getStoredToken() || undefined
      ).catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not copy link');
    } finally {
      setWorking(false);
    }
  }

  if (!battleID) {
    return (
      <main className="container">
        <section className="panel stack">
          <h1>Battle Card</h1>
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
            <h1>Battle Card</h1>
            <p className="subtle">Battle ID: {battleID}</p>
          </div>
          <Link className="primary-link" href="/">
            Dashboard
          </Link>
        </div>

        <div className="battle-card-actions row">
          <button type="button" onClick={onCopyImage} disabled={working}>
            Copy image
          </button>
          <button type="button" className="secondary" onClick={onShare} disabled={working}>
            Share
          </button>
          <button type="button" className="secondary" onClick={onCopyLink} disabled={working || !battleURL}>
            Copy link
          </button>
          <a className="cta-link" href={cardURL} download={`battle-${battleID}.png`}>
            Download card.png
          </a>
        </div>

        <div className="battle-card-preview">
          {!previewError && (
            <img
              src={cardURL}
              alt="Battle card preview"
              loading="lazy"
              onError={() => setPreviewError(true)}
            />
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
    </main>
  );
}
