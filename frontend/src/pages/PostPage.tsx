import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router'
import { api } from '../api'
import { useAuth } from '../auth'
import { EmptyState, ErrorState, LoadingState } from '../components'
import type { Comment, PagedComments, Post } from '../types'

function CommentItem({ comment, onReply, onChanged, reply = false }: { comment: Comment; onReply: (id: string, name: string) => void; onChanged: () => void; reply?: boolean }) {
  const { user } = useAuth()
  const owner = user && (user.role === 'admin' || user.public_id === comment.author?.public_id)
  async function remove() { if (window.confirm('Delete this comment?')) { await api.deleteComment(comment.public_id); onChanged() } }
  async function edit() {
    const body = window.prompt('Edit your comment', comment.body_markdown)
    if (!body || body === comment.body_markdown) return
    await api.updateComment(comment.public_id, body)
    onChanged()
  }
  return <article className="comment">
    <div className="comment-head"><span className="avatar small">{comment.author?.username?.[0]?.toUpperCase() || '?'}</span><strong>{comment.author?.username || 'Reader'}</strong><time>{new Date(comment.created_at).toLocaleDateString()}</time></div>
    <div className="comment-body" dangerouslySetInnerHTML={{ __html: comment.body_html }} />
    <div className="comment-actions">{!reply && <button onClick={() => onReply(comment.public_id, comment.author?.username || 'reader')}>Reply</button>}{owner && <button onClick={() => void edit()}>Edit</button>}{owner && <button onClick={() => void remove()}>Delete</button>}</div>
    {comment.children?.map((child) => <div className="comment-reply" key={child.public_id}><CommentItem comment={child} onReply={onReply} onChanged={onChanged} reply /></div>)}
  </article>
}

export function PostPage() {
  const { slug = '' } = useParams()
  const { user } = useAuth()
  const [post, setPost] = useState<Post | null>(null)
  const [comments, setComments] = useState<PagedComments | null>(null)
  const [commentPage, setCommentPage] = useState(1)
  const [commentsError, setCommentsError] = useState('')
  const [error, setError] = useState('')
  const [body, setBody] = useState('')
  const [reply, setReply] = useState<{ id: string; name: string } | null>(null)
  const [notice, setNotice] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      const postResult = await api.getPost(slug)
      setPost(postResult.post)
      setCommentsError('')
      try { setComments(await api.listComments(slug, commentPage)) }
      catch (err) {
        setComments(null)
        setCommentsError(err instanceof Error ? err.message : 'Unable to load comments')
      }
    } catch (err) { setError(err instanceof Error ? err.message : 'Unable to load story') }
  }, [slug, commentPage])

  useEffect(() => { void load() }, [load])

  async function submit(event: FormEvent) {
    event.preventDefault(); setNotice(''); setSubmitting(true)
    try {
      const result = await api.createComment(slug, { body_markdown: body, parent_id: reply?.id })
      setBody(''); setReply(null)
      setNotice(result.comment.status === 'pending' ? 'Comment submitted and awaiting automatic moderation.' : 'Comment published.')
      window.setTimeout(() => void load(), 2500)
    } catch (err) { setNotice(err instanceof Error ? err.message : 'Unable to submit comment') }
    finally { setSubmitting(false) }
  }

  if (error) return <main className="page-shell"><ErrorState message={error} retry={() => void load()} /></main>
  if (!post) return <main className="page-shell"><LoadingState label="Opening story…" /></main>

  return <main>
    <article className="article-shell">
      <Link className="back-link" to="/">← All stories</Link>
      <header className="article-header">
        <div className="post-meta"><span>{post.categories?.[0]?.name || 'Essay'}</span><time>{new Date(post.published_at || post.created_at).toLocaleDateString()}</time></div>
        <h1>{post.title}</h1>
        {post.summary && <p className="article-deck">{post.summary}</p>}
        <div className="article-byline"><span className="avatar">{post.author?.username?.[0]?.toUpperCase() || 'B'}</span><div><strong>{post.author?.username || 'Blog author'}</strong><span>{post.tags?.map((tag) => tag.name).join(' · ') || 'Independent writer'}</span></div></div>
        {user && (user.role === 'admin' || user.public_id === post.author?.public_id) && <Link className="button ghost compact edit-story" to={`/write?edit=${encodeURIComponent(post.slug)}`}>Edit story</Link>}
      </header>
      {post.cover_url && <img className="article-cover" src={post.cover_url} alt="" />}
      <div className="article-content" dangerouslySetInnerHTML={{ __html: post.content_html }} />
    </article>
    <section className="comments-section">
      <div className="article-shell narrow">
        <div className="section-heading"><div><span className="eyebrow">Conversation</span><h2>Comments</h2></div><span>{comments?.pagination.total || 0}</span></div>
        {notice && <p className="notice" role="status">{notice}</p>}
        {user && post.status === 'published' && post.visibility === 'public' ? <form className="comment-form" onSubmit={submit}>
          {reply && <div className="reply-banner">Replying to {reply.name}<button type="button" onClick={() => setReply(null)}>Cancel</button></div>}
          <textarea value={body} onChange={(e) => setBody(e.target.value)} placeholder="Add to the conversation… Markdown is supported." required maxLength={5000} />
          <button className="button" type="submit" disabled={submitting}>{submitting ? 'Posting…' : 'Post comment'}</button>
        </form> : !user ? <p className="signin-callout"><Link to="/login" state={{ from: `/posts/${slug}` }}>Sign in</Link> to join the conversation.</p> : <p className="signin-callout">Comments open when this story is published publicly.</p>}
        {commentsError && <ErrorState message={commentsError} retry={() => void load()} />}
        {comments?.comments.length === 0 && <EmptyState title="Start the conversation">Be the first reader to leave a thoughtful response.</EmptyState>}
        <div className="comment-list">{comments?.comments.map((comment) => <CommentItem key={comment.public_id} comment={comment} onReply={(id, name) => setReply({ id, name })} onChanged={() => void load()} />)}</div>
        {comments && comments.pagination.total_pages > 1 && <nav className="pagination" aria-label="Comment pages"><button disabled={commentPage <= 1} onClick={() => setCommentPage((page) => page - 1)}>← Older</button><span>{commentPage} / {comments.pagination.total_pages}</span><button disabled={commentPage >= comments.pagination.total_pages} onClick={() => setCommentPage((page) => page + 1)}>Newer →</button></nav>}
      </div>
    </section>
  </main>
}
