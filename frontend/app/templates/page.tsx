'use client';

import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { useEffect, useMemo, useState } from 'react';
import { Template, listTemplates } from '../../lib/api';

const TOKEN_KEY = 'personaworlds_token';
const PREFERRED_TEMPLATE_KEY = 'personaworlds_preferred_template_id';

export default function TemplatesPage() {
  const searchParams = useSearchParams();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const selectedTemplateID = useMemo(() => (searchParams.get('template') || '').trim(), [searchParams]);
  const sourceBattleID = useMemo(() => (searchParams.get('battle') || '').trim(), [searchParams]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        setLoading(true);
        setError('');
        const response = await listTemplates();
        if (!cancelled) {
          setTemplates(response.templates || []);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'could not load templates');
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  function onUseTemplate(template: Template) {
    if (typeof window === 'undefined') {
      return;
    }
    localStorage.setItem(PREFERRED_TEMPLATE_KEY, template.id);

    if (sourceBattleID) {
      const token = (localStorage.getItem(TOKEN_KEY) || '').trim();
      if (!token) {
        const nextPath = `/b/${encodeURIComponent(sourceBattleID)}?template=${encodeURIComponent(template.id)}`;
        window.location.href = `/signup?next=${encodeURIComponent(nextPath)}&remix=1`;
        return;
      }
      window.location.href = `/b/${encodeURIComponent(sourceBattleID)}?remix=1&template=${encodeURIComponent(template.id)}`;
      return;
    }

    setMessage(`Template selected: ${template.name}`);
  }

  return (
    <main className="container">
      <section className="panel stack">
        <div className="battle-card-head">
          <div className="stack">
            <h1>Templates Marketplace</h1>
            <p className="subtle">Browse shareable battle formats and reuse them in one click.</p>
          </div>
          <Link className="primary-link" href="/">
            Dashboard
          </Link>
        </div>

        {loading && <p className="subtle">Loading templates...</p>}
        {!loading && templates.length === 0 && <p className="subtle">No public templates yet.</p>}

        <div className="template-grid">
          {templates.map((template) => {
            const isSelected = selectedTemplateID === template.id;
            return (
              <article key={template.id} className={isSelected ? 'template-card selected' : 'template-card'}>
                <h2>{template.name}</h2>
                <p className="subtle">
                  {template.turn_count} turns · {template.word_limit} words
                </p>
                <p>{template.prompt_rules}</p>
                <div className="row">
                  <button type="button" onClick={() => onUseTemplate(template)}>
                    Use template
                  </button>
                  {sourceBattleID && (
                    <Link
                      className="cta-link"
                      href={`/b/${encodeURIComponent(sourceBattleID)}?remix=1&template=${encodeURIComponent(template.id)}`}
                    >
                      Remix with this
                    </Link>
                  )}
                </div>
              </article>
            );
          })}
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
