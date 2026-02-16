const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080';

type RequestOptions = {
  method?: string;
  token?: string;
  body?: unknown;
  keepalive?: boolean;
};

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method || 'GET',
    keepalive: options.keepalive,
    headers: {
      'Content-Type': 'application/json',
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {})
    },
    body: options.body ? JSON.stringify(options.body) : undefined
  });

  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `request failed with status ${response.status}`);
  }
  return data as T;
}

export type Persona = {
  id: string;
  name: string;
  bio: string;
  tone: string;
  writing_samples: string[];
  do_not_say: string[];
  catchphrases: string[];
  preferred_language: 'tr' | 'en';
  formality: number;
  daily_draft_quota: number;
  daily_reply_quota: number;
  created_at: string;
  updated_at: string;
};

export type PersonaPayload = {
  name: string;
  bio: string;
  tone: string;
  writing_samples: string[];
  do_not_say: string[];
  catchphrases: string[];
  preferred_language: 'tr' | 'en';
  formality: number;
  daily_draft_quota: number;
  daily_reply_quota: number;
};

export type Room = {
  id: string;
  slug: string;
  name: string;
  description: string;
  created_at: string;
};

export type Post = {
  id: string;
  room_id: string;
  persona_id?: string;
  persona_name?: string;
  authored_by: 'AI' | 'HUMAN' | 'AI_DRAFT_APPROVED';
  status: 'DRAFT' | 'PUBLISHED';
  content: string;
  created_at: string;
  updated_at: string;
};

export type Reply = {
  id: string;
  post_id: string;
  persona_id?: string;
  persona_name?: string;
  authored_by: 'AI' | 'HUMAN' | 'AI_DRAFT_APPROVED';
  content: string;
  created_at: string;
  updated_at: string;
};

export type ThreadResponse = {
  post: Post;
  replies: Reply[];
  ai_summary: string;
};

export type PreviewResponse = {
  drafts: Array<{
    label: string;
    content: string;
    authored_by: 'AI' | 'HUMAN' | 'AI_DRAFT_APPROVED';
  }>;
  quota: {
    used: number;
    limit: number;
  };
};

export type DigestThread = {
  post_id: string;
  room_id?: string;
  room_name?: string;
  post_preview?: string;
  activity_count: number;
  last_activity_at: string;
};

export type PersonaDigest = {
  persona_id: string;
  date: string;
  summary: string;
  has_activity: boolean;
  updated_at: string;
  stats: {
    posts: number;
    replies: number;
    top_threads: DigestThread[];
  };
};

export type PersonaDigestResponse = {
  digest: PersonaDigest;
  exists: boolean;
};

export type PublicPersonaProfile = {
  persona_id: string;
  slug: string;
  name: string;
  bio: string;
  tone: string;
  preferred_language: string;
  formality: number;
  is_public: boolean;
  followers: number;
  posts_count: number;
  badges: string[];
  created_at: string;
};

export type PublicPersonaPost = {
  id: string;
  room_id: string;
  room_name: string;
  authored_by: 'AI' | 'HUMAN' | 'AI_DRAFT_APPROVED';
  content: string;
  created_at: string;
};

export type PublicPersonaRoom = {
  room_id: string;
  room_name: string;
  post_count: number;
};

export type PublicPersonaProfileResponse = {
  profile: PublicPersonaProfile;
  latest_posts: PublicPersonaPost[];
  top_rooms: PublicPersonaRoom[];
  next_cursor: string;
};

export type PublicPersonaPostsResponse = {
  posts: PublicPersonaPost[];
  next_cursor: string;
};

export type PublishPersonaProfileResponse = {
  persona_id: string;
  slug: string;
  is_public: boolean;
  bio: string;
  created_at?: string;
  share_url: string;
};

export type FollowPublicPersonaResponse = {
  followed: boolean;
  followers: number;
};

export type Template = {
  id: string;
  owner_user_id?: string;
  name: string;
  prompt_rules: string;
  turn_count: number;
  word_limit: number;
  created_at: string;
  is_public: boolean;
};

export type RemixTemplateSummary = {
  id: string;
  name: string;
  turn_count: number;
  word_limit: number;
};

export type RemixIntentResponse = {
  battle_id: string;
  room_id: string;
  room_name: string;
  topic: string;
  pro_style: string;
  con_style: string;
  suggested_templates: RemixTemplateSummary[];
  remix_token: string;
  remix_token_expires: string;
};

export type PublicBattleMeta = {
  battle_id: string;
  room_id: string;
  room_name: string;
  topic: string;
  created_at: string;
  template?: {
    id: string;
    name: string;
  };
  share_url: string;
  card_url: string;
};

export type CreateBattlePayload = {
  topic: string;
  template_id?: string;
  remix_token?: string;
  pro_style?: string;
  con_style?: string;
};

export type CreateBattleResponse = {
  battle_id: string;
  post: Post;
  room_name: string;
  template: Template;
  enqueued_replies: number;
  remix_used: boolean;
  suggested_next_url: string;
};

export type FeedBattleItem = {
  battle_id: string;
  room_id: string;
  room_name: string;
  persona_id?: string;
  persona_name?: string;
  topic: string;
  created_at: string;
  shares: number;
  remixes: number;
  template?: {
    id: string;
    name: string;
  };
};

export type FeedTemplateItem = {
  template_id: string;
  name: string;
  prompt_rules: string;
  turn_count: number;
  word_limit: number;
  created_at: string;
  usage_count: number;
  is_trending: boolean;
};

export type FeedItem = {
  id: string;
  kind: 'battle' | 'template';
  reason: string;
  reasons: string[];
  score: number;
  battle?: FeedBattleItem;
  template?: FeedTemplateItem;
};

export type FeedResponse = {
  items: FeedItem[];
  highlight_template?: FeedTemplateItem;
};

export type Notification = {
  id: number;
  actor_user_id?: string;
  type: 'battle_remixed' | 'template_used' | 'persona_followed';
  title: string;
  body: string;
  metadata: Record<string, unknown>;
  read_at?: string;
  created_at: string;
};

export type NotificationsResponse = {
  notifications: Notification[];
  unread_count: number;
};

export type WeeklyDigestItem = {
  battle_id: string;
  room_id: string;
  room_name: string;
  topic: string;
  summary: string;
  score: number;
  created_at: string;
};

export type WeeklyDigest = {
  week_start: string;
  generated_at: string;
  items: WeeklyDigestItem[];
};

export type WeeklyDigestResponse = {
  digest: WeeklyDigest;
  exists: boolean;
  is_current_week: boolean;
};

export async function signup(email: string, password: string, shareSlug = '') {
  const normalizedShareSlug = shareSlug.trim();
  return request<{ token: string; user_id: string }>('/auth/signup', {
    method: 'POST',
    body: {
      email,
      password,
      ...(normalizedShareSlug ? { share_slug: normalizedShareSlug } : {})
    }
  });
}

export async function login(email: string, password: string) {
  return request<{ token: string; user_id: string }>('/auth/login', {
    method: 'POST',
    body: { email, password }
  });
}

export async function listPersonas(token: string) {
  return request<{ personas: Persona[] }>('/personas', { token });
}

export async function createPersona(token: string, payload: PersonaPayload) {
  return request<Persona>('/personas', {
    method: 'POST',
    token,
    body: payload
  });
}

export async function updatePersona(token: string, personaId: string, payload: PersonaPayload) {
  return request<Persona>(`/personas/${personaId}`, {
    method: 'PUT',
    token,
    body: payload
  });
}

export async function previewPersona(token: string, personaId: string, roomId: string) {
  const query = new URLSearchParams({ room_id: roomId }).toString();
  return request<PreviewResponse>(`/personas/${personaId}/preview?${query}`, {
    method: 'POST',
    token,
    body: {}
  });
}

export async function getTodayDigest(token: string, personaId: string) {
  return request<PersonaDigestResponse>(`/personas/${personaId}/digest/today`, { token });
}

export async function getLatestDigest(token: string, personaId: string) {
  return request<PersonaDigestResponse>(`/personas/${personaId}/digest/latest`, { token });
}

export async function publishPersonaProfile(
  token: string,
  personaId: string,
  payload: { slug?: string; bio?: string } = {}
) {
  return request<PublishPersonaProfileResponse>(`/personas/${personaId}/publish-profile`, {
    method: 'POST',
    token,
    body: payload
  });
}

export async function unpublishPersonaProfile(token: string, personaId: string) {
  return request<PublishPersonaProfileResponse>(`/personas/${personaId}/unpublish-profile`, {
    method: 'POST',
    token,
    body: {}
  });
}

export async function getPublicPersonaProfile(slug: string) {
  return request<PublicPersonaProfileResponse>(`/p/${encodeURIComponent(slug)}`);
}

export async function getPublicPersonaPosts(slug: string, cursor = '') {
  const query = new URLSearchParams();
  if (cursor.trim()) {
    query.set('cursor', cursor.trim());
  }
  const suffix = query.toString() ? `?${query.toString()}` : '';
  return request<PublicPersonaPostsResponse>(`/p/${encodeURIComponent(slug)}/posts${suffix}`);
}

export async function followPublicPersona(slug: string, token?: string) {
  return request<FollowPublicPersonaResponse>(`/p/${encodeURIComponent(slug)}/follow`, {
    method: 'POST',
    token,
    body: {}
  });
}

export async function getPublicBattleMeta(battleId: string) {
  return request<PublicBattleMeta>(`/b/${encodeURIComponent(battleId)}/meta`);
}

export async function createBattleRemixIntent(battleId: string, token?: string) {
  return request<RemixIntentResponse>(`/battles/${encodeURIComponent(battleId)}/remix-intent`, {
    method: 'POST',
    token,
    body: {}
  });
}

export async function listTemplates() {
  return request<{ templates: Template[] }>('/templates');
}

export async function createTemplate(
  token: string,
  payload: {
    name: string;
    prompt_rules: string;
    turn_count: number;
    word_limit: number;
    is_public: boolean;
  }
) {
  return request<Template>('/templates', {
    method: 'POST',
    token,
    body: payload
  });
}

export async function getFeed(token: string) {
  return request<FeedResponse>('/feed', { token });
}

export async function getNotifications(token: string, limit = 20) {
  const query = new URLSearchParams({ limit: String(limit) }).toString();
  return request<NotificationsResponse>(`/notifications?${query}`, { token });
}

export async function markNotificationRead(token: string, notificationId: number) {
  return request<{ updated: boolean; unread_count: number }>(`/notifications/${notificationId}/read`, {
    method: 'POST',
    token,
    body: {}
  });
}

export async function markAllNotificationsRead(token: string) {
  return request<{ updated: number; unread_count: number }>('/notifications/read-all', {
    method: 'POST',
    token,
    body: {}
  });
}

export async function getWeeklyDigest(token: string) {
  return request<WeeklyDigestResponse>('/digest/weekly', { token });
}

export async function listRooms(token: string) {
  return request<{ rooms: Room[] }>('/rooms', { token });
}

export async function listRoomPosts(token: string, roomId: string) {
  return request<{ posts: Post[] }>(`/rooms/${roomId}/posts`, { token });
}

export async function createDraft(token: string, roomId: string, personaId: string) {
  return request<Post>(`/rooms/${roomId}/posts/draft`, {
    method: 'POST',
    token,
    body: { persona_id: personaId }
  });
}

export async function createBattle(token: string, roomId: string, payload: CreateBattlePayload) {
  return request<CreateBattleResponse>(`/rooms/${roomId}/battles`, {
    method: 'POST',
    token,
    body: payload
  });
}

export async function approvePost(token: string, postId: string) {
  return request<Post>(`/posts/${postId}/approve`, {
    method: 'POST',
    token,
    body: {}
  });
}

export async function generateReplies(token: string, postId: string, personaIds: string[]) {
  return request<{ enqueued: number; skipped: number }>(`/posts/${postId}/generate-replies`, {
    method: 'POST',
    token,
    body: { persona_ids: personaIds }
  });
}

export async function getThread(token: string, postId: string) {
  return request<ThreadResponse>(`/posts/${postId}/thread`, { token });
}

export async function trackEvent(eventName: string, metadata: Record<string, unknown> = {}, token?: string) {
  return request<{ ok: boolean }>('/events', {
    method: 'POST',
    token,
    keepalive: true,
    body: {
      event_name: eventName,
      metadata
    }
  });
}
