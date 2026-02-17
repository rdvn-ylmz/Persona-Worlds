'use client';

import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { FormEvent, Suspense, useEffect, useMemo, useState } from 'react';
import { Template, createTemplate, listTemplates } from '../../lib/api';
import { SkeletonList } from '../../components/skeleton';
import { Spinner } from '../../components/spinner';
import { useToast } from '../../components/toast-provider';

const TOKEN_KEY = 'personaworlds_token';
const PREFERRED_TEMPLATE_KEY = 'personaworlds_preferred_template_id';

function TemplatesPageContent() {
  const toast = useToast();
  const searchParams = useSearchParams();

  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [token, setToken] = useState('');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const [name, setName] = useState('');
  const [promptRules, setPromptRules] = useState('');
  const [turnCount, setTurnCount] = useState(6);
  const [wordLimit, setWordLimit] = useState(120);
  const [isPublic, setIsPublic] = useState(true);

  const selectedTemplateID = useMemo(() => (searchParams.get('template') || '').trim(), [searchParams]);
  const sourceBattleID = useMemo(() => (searchParams.get('battle') || '').trim(), [searchParams]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    setToken((localStorage.getItem(TOKEN_KEY) || '').trim());
  }, []);

  useEffect(() => {
    void loadTemplates();
  }, []);

  async function loadTemplates() {
    try {
      setLoading(true);
      setError('');
      const response = await listTemplates();
      setTemplates(response.templates || []);
    } catch (err) {
      const messageText = err instanceof Error ? err.message : 'could not load templates';
      setError(messageText);
      toast.error(messageText);
    } finally {
      setLoading(false);
    }
  }

  function onUseTemplate(template: Template) {
    if (typeof window === 'undefined') {
      return;
    }

    localStorage.setItem(PREFERRED_TEMPLATE_KEY, template.id);

    if (sourceBattleID) {
      const authToken = (localStorage.getItem(TOKEN_KEY) || '').trim();
      if (!authToken) {
        const nextPath = `/b/${encodeURIComponent(sourceBattleID)}?template=${encodeURIComponent(template.id)}`;
        window.location.href = `/signup?next=${encodeURIComponent(nextPath)}&remix=1`;
        return;
      }
      window.location.href = `/b/${encodeURIComponent(sourceBattleID)}?remix=1&template=${encodeURIComponent(template.id)}`;
      return;
    }

    setMessage(`Template selected: ${template.name}`);
    toast.info('Template selected. Redirecting to feed compose.');
    window.location.href = `/?template=${encodeURIComponent(template.id)}#feed`;
  }

  async function onCreateTemplate(event: FormEvent) {
    event.preventDefault();
    if (!token) {
      if (typeof window !== 'undefined') {
        window.location.href = '/signup?next=%2Ftemplates';
      }
      return;
    }

    try {
      setCreating(true);
      setError('');
      setMessage('');

      const created = await createTemplate(token, {
        name: name.trim(),
        prompt_rules: promptRules.trim(),
        turn_count: turnCount,
        word_limit: wordLimit,
        is_public: isPublic
      });

      setTemplates((current) => [created, ...current.filter((item) => item.id !== created.id)]);
      setName('');
      setPromptRules('');
      setTurnCount(6);
      setWordLimit(120);
      setIsPublic(true);

      const successText = `Template created: ${created.name}`;
      setMessage(successText);
      toast.success(successText);
    } catch (err) {
      const messageText = err instanceof Error ? err.message : 'could not create template';
      setError(messageText);
      toast.error(messageText);
    } finally {
      setCreating(false);
    }
  }

  return (
    <main className="container">
      <section className="panel stack">
        <div className="battle-card-head">
          <div className="stack">
            <h1>Templates Marketplace</h1>
            <p className="subtle">Browse and create battle formats.</p>
          </div>
          <div className="row">
            <button type="button" className="secondary" onClick={() => void loadTemplates()} disabled={loading}>
              <span className="button-content">
                {loading && <Spinner />}
                <span>Refresh</span>
              </span>
            </button>
            <Link className="primary-link" href="/">
              Dashboard
            </Link>
          </div>
        </div>

        <form className="template-card stack" onSubmit={onCreateTemplate}>
          <h2>Create Template</h2>
          {!token && <p className="subtle">Log in to create templates.</p>}
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Template name" maxLength={80} required />
          <textarea
            value={promptRules}
            onChange={(event) => setPromptRules(event.target.value)}
            placeholder="Prompt rules"
            rows={4}
            maxLength={1200}
            required
          />
          <div className="row">
            <label>
              Turn count
              <input
                type="number"
                min={2}
                max={20}
                value={turnCount}
                onChange={(event) => setTurnCount(Math.max(2, Math.min(20, Number(event.target.value) || 2)))}
              />
            </label>
            <label>
              Word limit
              <input
                type="number"
                min={40}
                max={500}
                value={wordLimit}
                onChange={(event) => setWordLimit(Math.max(40, Math.min(500, Number(event.target.value) || 40)))}
              />
            </label>
            <label>
              Visibility
              <select value={isPublic ? 'public' : 'private'} onChange={(event) => setIsPublic(event.target.value === 'public')}>
                <option value="public">Public</option>
                <option value="private">Private</option>
              </select>
            </label>
          </div>
          <button type="submit" disabled={creating || !token}>
            <span className="button-content">
              {creating && <Spinner />}
              <span>Create Template</span>
            </span>
          </button>
        </form>

        {loading && <SkeletonList rows={4} />}
        {!loading && templates.length === 0 && <p className="subtle">No public templates yet.</p>}

        {!loading && templates.length > 0 && (
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

export default function TemplatesPage() {
  return (
    <Suspense
      fallback={
        <main className="container">
          <section className="panel stack">
            <h1>Templates Marketplace</h1>
            <SkeletonList rows={3} />
          </section>
        </main>
      }
    >
      <TemplatesPageContent />
    </Suspense>
  );
}
