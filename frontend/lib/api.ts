const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080';

type RequestOptions = {
  method?: string;
  token?: string;
  body?: unknown;
};

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method || 'GET',
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
  daily_draft_quota: number;
  daily_reply_quota: number;
  created_at: string;
  updated_at: string;
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

export async function signup(email: string, password: string) {
  return request<{ token: string; user_id: string }>('/auth/signup', {
    method: 'POST',
    body: { email, password }
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

export async function createPersona(
  token: string,
  payload: Pick<Persona, 'name' | 'bio' | 'tone' | 'daily_draft_quota' | 'daily_reply_quota'>
) {
  return request<Persona>('/personas', {
    method: 'POST',
    token,
    body: payload
  });
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
