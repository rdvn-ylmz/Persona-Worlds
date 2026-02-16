'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useEffect, useMemo, useState } from 'react';
import {
  PublicPersonaPost,
  PublicPersonaProfileResponse,
  followPublicPersona,
  getPublicPersonaPosts,
  getPublicPersonaProfile,
  trackEvent
} from '../../../lib/api';

const TOKEN_KEY = 'personaworlds_token';
const SHARE_SLUG_KEY = 'personaworlds_share_slug';

function publicPostBadge(post: PublicPersonaPost) {
  if (post.authored_by === 'AI_DRAFT_APPROVED') {
    return { label: 'AI generated / approved', className: 'badge badge-approved' };
  }
  if (post.authored_by === 'AI') {
    return { label: 'AI generated', className: 'badge badge-ai' };
  }
  return { label: 'Human', className: 'badge badge-human' };
}

export default function PublicPersonaPage() {
  const params = useParams<{ slug: string }>();
  const slug = useMemo(() => (params?.slug || '').toString().trim(), [params]);

  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [following, setFollowing] = useState(false);

  const [profileData, setProfileData] = useState<PublicPersonaProfileResponse | null>(null);
  const [posts, setPosts] = useState<PublicPersonaPost[]>([]);
  const [nextCursor, setNextCursor] = useState('');

  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    if (!slug) {
      setLoading(false);
      setError('profile not found');
      return;
    }
    void loadProfile(slug);
  }, [slug]);

  async function loadProfile(profileSlug: string) {
    try {
      setLoading(true);
      setError('');
      const response = await getPublicPersonaProfile(profileSlug);
      setProfileData(response);
      setPosts(response.latest_posts || []);
      setNextCursor(response.next_cursor || '');
      if (typeof window !== 'undefined') {
        localStorage.setItem(SHARE_SLUG_KEY, profileSlug);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load profile');
    } finally {
      setLoading(false);
    }
  }

  async function loadMorePosts() {
    if (!slug || !nextCursor) {
      return;
    }

    try {
      setLoadingMore(true);
      setError('');
      const response = await getPublicPersonaPosts(slug, nextCursor);
      setPosts((current) => [...current, ...response.posts]);
      setNextCursor(response.next_cursor || '');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not load more posts');
    } finally {
      setLoadingMore(false);
    }
  }

  async function onFollow() {
    if (!slug) {
      return;
    }

    try {
      setFollowing(true);
      setError('');
      setMessage('');
      const token = typeof window !== 'undefined' ? localStorage.getItem(TOKEN_KEY) || '' : '';
      void trackEvent(
        'follow_click',
        {
          slug,
          source: 'public_profile'
        },
        token || undefined
      ).catch(() => undefined);
      const response = await followPublicPersona(slug, token || undefined);
      setProfileData((current) => {
        if (!current) {
          return current;
        }
        return {
          ...current,
          profile: {
            ...current.profile,
            followers: response.followers
          }
        };
      });
      setMessage(response.followed ? 'You are now following this persona.' : 'You already follow this persona.');
    } catch (err) {
      const messageText = err instanceof Error ? err.message : 'follow failed';
      if (messageText.includes('signup_required')) {
        setError('Signup required to follow personas. Create your own persona to join.');
      } else {
        setError(messageText);
      }
    } finally {
      setFollowing(false);
    }
  }

  if (loading) {
    return (
      <main className="container">
        <section className="panel public-panel stack">
          <h1>Public Persona</h1>
          <p className="subtle">Loading profile...</p>
        </section>
      </main>
    );
  }

  if (!profileData) {
    return (
      <main className="container">
        <section className="panel public-panel stack">
          <h1>Public Persona</h1>
          <p className="error">{error || 'profile not found'}</p>
          <Link className="primary-link" href="/">
            Create your own persona
          </Link>
        </section>
      </main>
    );
  }

  const { profile, top_rooms: topRooms } = profileData;
  const onRemixClick = () => {
    const token = typeof window !== 'undefined' ? localStorage.getItem(TOKEN_KEY) || '' : '';
    void trackEvent(
      'remix_clicked',
      {
        slug: profile.slug,
        source: 'public_profile'
      },
      token || undefined
    ).catch(() => undefined);
  };

  return (
    <main className="container">
      <section className="panel public-panel stack">
        <div className="public-head">
          <div className="stack">
            <h1>{profile.name}</h1>
            <p>{profile.bio || 'No public bio yet.'}</p>
            <div className="badge-row">
              {profile.badges.map((badge) => (
                <span key={badge} className="status">
                  {badge}
                </span>
              ))}
            </div>
          </div>
          <div className="public-actions stack">
            <button onClick={onFollow} disabled={following}>
              {following ? 'Following...' : 'Follow'}
            </button>
            <Link className="cta-link" href="/" onClick={onRemixClick}>
              Create your own persona
            </Link>
          </div>
        </div>

        <div className="public-stats">
          <div className="mini-card">
            <strong>{profile.followers}</strong>
            <span>Followers</span>
          </div>
          <div className="mini-card">
            <strong>{profile.posts_count}</strong>
            <span>Published Posts</span>
          </div>
          <div className="mini-card">
            <strong>{topRooms.length}</strong>
            <span>Top Rooms</span>
          </div>
        </div>

        <div className="stack">
          <h2>Interests</h2>
          {topRooms.length === 0 && <p className="subtle">No room activity yet.</p>}
          {topRooms.length > 0 && (
            <div className="room-tags">
              {topRooms.map((room) => (
                <span key={room.room_id} className="status">
                  {room.room_name} · {room.post_count}
                </span>
              ))}
            </div>
          )}
        </div>

        <div className="stack">
          <h2>Latest Posts</h2>
          {posts.length === 0 && <p className="subtle">No published posts yet.</p>}
          {posts.map((post) => {
            const badge = publicPostBadge(post);
            return (
              <article key={post.id} className="post-card">
                <div className="post-meta">
                  <span className="status">{post.room_name || 'Room'}</span>
                  <span className={badge.className}>{badge.label}</span>
                  <span className="subtle">{new Date(post.created_at).toLocaleString()}</span>
                </div>
                <p>{post.content}</p>
              </article>
            );
          })}

          {nextCursor && (
            <button className="secondary" onClick={loadMorePosts} disabled={loadingMore}>
              {loadingMore ? 'Loading...' : 'Load More'}
            </button>
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
