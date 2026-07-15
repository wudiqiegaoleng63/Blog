import type {
  ApiFailure,
  ApiSuccess,
  Comment,
  PagedComments,
  PagedPosts,
  Post,
  PostInput,
  Taxonomy,
  TokenPair,
  User,
  UserProfile,
} from './types'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: unknown
  readonly requestId?: string

  constructor(status: number, failure?: ApiFailure) {
    super(failure?.error?.message || `Request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.code = failure?.error?.code || 'request_failed'
    this.details = failure?.error?.details
    this.requestId = failure?.request_id
  }
}

let accessToken: string | null = null
let accessTokenExpiresAt = 0
let refreshPromise: Promise<boolean> | null = null
let authenticationLost: (() => void) | null = null

export function setAccessToken(token: string | null, expiresIn = 0) {
  accessToken = token
  accessTokenExpiresAt = token && expiresIn > 0 ? Date.now() + expiresIn * 1000 : 0
}

export function onAuthenticationLost(listener: (() => void) | null) {
  authenticationLost = listener
}

async function parseFailure(response: Response): Promise<ApiFailure | undefined> {
  try {
    return (await response.json()) as ApiFailure
  } catch {
    return undefined
  }
}

async function rawRequest<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  if (retry && !path.startsWith('/auth/') && accessToken && accessTokenExpiresAt > 0 && Date.now() >= accessTokenExpiresAt - 5_000) {
    await refreshAccessToken()
  }
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)

  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
    credentials: 'include',
  })

  if (response.status === 401 && retry && (path === '/auth/me' || !path.startsWith('/auth/'))) {
    const refreshed = await refreshAccessToken()
    if (refreshed) return rawRequest<T>(path, init, false)
  }

  if (!response.ok) throw new ApiError(response.status, await parseFailure(response))
  if (response.status === 204) return undefined as T
  const body = (await response.json()) as ApiSuccess<T>
  return body.data
}

export async function refreshAccessToken(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = rawRequest<TokenPair>('/auth/refresh', { method: 'POST' }, false)
      .then((pair) => {
        setAccessToken(pair.access_token, pair.expires_in)
        return true
      })
      .catch(() => {
        setAccessToken(null)
        authenticationLost?.()
        return false
      })
      .finally(() => {
        refreshPromise = null
      })
  }
  return refreshPromise
}

export const api = {
  register: (input: { email: string; username: string; password: string }) =>
    rawRequest<TokenPair>('/auth/register', { method: 'POST', body: JSON.stringify(input) }),
  login: (input: { email: string; password: string }) =>
    rawRequest<TokenPair>('/auth/login', { method: 'POST', body: JSON.stringify(input) }),
  logout: () => rawRequest<{ message: string }>('/auth/logout', { method: 'POST' }),
  me: () => rawRequest<{ user: User; profile: UserProfile | null }>('/auth/me'),

  listPosts: (page = 1) => rawRequest<PagedPosts>(`/posts?page=${page}&page_size=9`),
  getPost: (slug: string) => rawRequest<{ post: Post }>(`/posts/${encodeURIComponent(slug)}`),
  createPost: (input: PostInput) => rawRequest<{ post: Post }>('/posts', { method: 'POST', body: JSON.stringify(input) }),
  updatePost: (slug: string, input: Partial<PostInput>) =>
    rawRequest<{ post: Post }>(`/posts/${encodeURIComponent(slug)}`, { method: 'PUT', body: JSON.stringify(input) }),
  deletePost: (slug: string) => rawRequest<void>(`/posts/${encodeURIComponent(slug)}`, { method: 'DELETE' }),

  listCategories: () => rawRequest<{ categories: Taxonomy[] }>('/categories'),
  createCategory: (input: { name: string; description?: string }) =>
    rawRequest<{ category: Taxonomy }>('/categories', { method: 'POST', body: JSON.stringify(input) }),
  updateCategory: (slug: string, input: { name: string; description?: string }) =>
    rawRequest<{ category: Taxonomy }>(`/categories/${encodeURIComponent(slug)}`, { method: 'PUT', body: JSON.stringify(input) }),
  deleteCategory: (slug: string) => rawRequest<void>(`/categories/${encodeURIComponent(slug)}`, { method: 'DELETE' }),

  listTags: () => rawRequest<{ tags: Taxonomy[] }>('/tags'),
  createTag: (input: { name: string }) => rawRequest<{ tag: Taxonomy }>('/tags', { method: 'POST', body: JSON.stringify(input) }),
  updateTag: (slug: string, input: { name: string }) =>
    rawRequest<{ tag: Taxonomy }>(`/tags/${encodeURIComponent(slug)}`, { method: 'PUT', body: JSON.stringify(input) }),
  deleteTag: (slug: string) => rawRequest<void>(`/tags/${encodeURIComponent(slug)}`, { method: 'DELETE' }),

  listComments: (slug: string, page = 1) =>
    rawRequest<PagedComments>(`/posts/${encodeURIComponent(slug)}/comments?page=${page}&page_size=20`),
  createComment: (slug: string, input: { body_markdown: string; parent_id?: string }) =>
    rawRequest<{ comment: Comment }>(`/posts/${encodeURIComponent(slug)}/comments`, {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  updateComment: (id: string, bodyMarkdown: string) =>
    rawRequest<{ comment: Comment }>(`/comments/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify({ body_markdown: bodyMarkdown }),
    }),
  deleteComment: (id: string) => rawRequest<void>(`/comments/${encodeURIComponent(id)}`, { method: 'DELETE' }),
}
