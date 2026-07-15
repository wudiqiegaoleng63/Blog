export interface ApiSuccess<T> {
  success: true
  data: T
}

export interface ApiErrorBody {
  code: string
  message: string
  details?: unknown
}

export interface ApiFailure {
  success: false
  request_id: string
  error: ApiErrorBody
}

export interface User {
  public_id: string
  email?: string
  username: string
  role: 'user' | 'admin'
  status?: string
  created_at?: string
}

export interface UserProfile {
  display_name: string
  bio?: string
  avatar_url?: string
  website_url?: string
  location?: string
}

export interface Taxonomy {
  public_id: string
  name: string
  slug: string
  description?: string
  created_at: string
  updated_at: string
}

export interface Post {
  public_id: string
  title: string
  slug: string
  summary?: string
  content_markdown: string
  content_html: string
  cover_url?: string
  status: 'draft' | 'published' | 'archived'
  visibility: 'public' | 'private'
  content_version: number
  published_at?: string
  created_at: string
  updated_at: string
  author?: User
  categories?: Taxonomy[]
  tags?: Taxonomy[]
}

export interface Comment {
  public_id: string
  body_markdown: string
  body_html: string
  status: 'pending' | 'approved' | 'rejected'
  moderated_at?: string
  created_at: string
  updated_at: string
  author?: User
  children?: Comment[]
}

export interface Pagination {
  page: number
  page_size: number
  total: number
  total_pages: number
}

export interface PagedPosts {
  posts: Post[]
  pagination: Pagination
}

export interface PagedComments {
  comments: Comment[]
  pagination: Pagination
}

export interface TokenPair {
  access_token: string
  expires_in: number
  token_type: string
}

export interface PostInput {
  title: string
  content_markdown: string
  summary?: string
  cover_url?: string
  status: Post['status']
  visibility: Post['visibility']
  category_ids: string[]
  tag_ids: string[]
}

export interface AISource {
  post_id: string
  title: string
  slug: string
  excerpt: string
  score: number
}

export interface AIAnswer {
  answer: string
  sources: AISource[]
}
