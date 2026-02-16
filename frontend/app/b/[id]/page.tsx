import type { Metadata } from 'next';
import PublicBattleClient from './PublicBattleClient';
import { PublicBattleSummary, getPublicBattleSummary } from '../../../lib/api';

type PageParams = {
  id: string;
};

type PageProps = {
  params: PageParams;
};

export const dynamic = 'force-dynamic';

const FALLBACK_TITLE = 'Persona Battle';
const FALLBACK_DESCRIPTION = 'A structured AI persona debate you can share publicly.';

function buildDescription(summary: PublicBattleSummary | null) {
  if (!summary) {
    return FALLBACK_DESCRIPTION;
  }
  const verdict = summary.verdict_text?.trim() || '';
  const takeaway = summary.top_takeaways?.[0]?.trim() || '';
  const parts = [verdict, takeaway ? `Takeaway: ${takeaway}` : ''].filter((value) => value.length > 0);
  if (parts.length === 0) {
    return FALLBACK_DESCRIPTION;
  }
  return parts.join(' ').slice(0, 260);
}

function siteOrigin() {
  return (
    process.env.NEXT_PUBLIC_FRONTEND_ORIGIN ||
    process.env.NEXT_PUBLIC_SITE_URL ||
    'http://localhost:3000'
  ).replace(/\/$/, '');
}

async function loadSummary(id: string): Promise<PublicBattleSummary | null> {
  try {
    const summary = await getPublicBattleSummary(id);
    return summary;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const battleID = (params?.id || '').trim();
  const summary = battleID ? await loadSummary(battleID) : null;

  const title = summary?.topic?.trim() || FALLBACK_TITLE;
  const description = buildDescription(summary);
  const url = `${siteOrigin()}/b/${encodeURIComponent(battleID)}`;

  return {
    title,
    description,
    openGraph: {
      title,
      description,
      type: 'article',
      url
    },
    twitter: {
      card: 'summary_large_image',
      title,
      description
    }
  };
}

export default function PublicBattlePage({ params }: PageProps) {
  const battleID = (params?.id || '').trim();
  return <PublicBattleClient battleID={battleID} />;
}
