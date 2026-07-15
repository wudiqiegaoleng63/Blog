import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components'
import type { PagedPosts } from '../types'

function formatDate(value?: string) {
  if (!value) return 'Draft'
  return new Intl.DateTimeFormat('en', { dateStyle: 'medium' }).format(new Date(value))
}

export function HomePage() {
  const [params, setParams] = useSearchParams()
  const requestedPage = Number(params.get('page'))
  const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1
  const [result, setResult] = useState<PagedPosts | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try { setResult(await api.listPosts(page)) }
    catch (err) { setError(err instanceof Error ? err.message : 'Unable to load stories') }
    finally { setLoading(false) }
  }, [page])

  useEffect(() => { void load() }, [load])

  return (
    <main>
      <section className="hero-section page-shell">
        <div className="eyebrow">Independent ideas · Carefully edited</div>
        <h1>Stories worth<br /><em>slowing down</em> for.</h1>
        <p>Essays, notes, and practical explorations from people who care about the details.</p>
        <a className="button" href="#latest">Explore the latest</a>
      </section>
      <section id="latest" className="page-shell section-block">
        <div className="section-heading"><div><span className="eyebrow">The journal</span><h2>Latest stories</h2></div>{result && <span>{result.pagination.total} published</span>}</div>
        {loading && <LoadingState label="Gathering stories…" />}
        {error && <ErrorState message={error} retry={() => void load()} />}
        {!loading && !error && result?.posts.length === 0 && <EmptyState title="No stories yet">The first published story will appear here.</EmptyState>}
        <div className="post-grid">
          {result?.posts.map((post, index) => (
            <article className={`post-card ${index === 0 ? 'featured' : ''}`} key={post.public_id}>
              {post.cover_url ? <img src={post.cover_url} alt="" /> : <div className="cover-placeholder"><span>{String(index + 1).padStart(2, '0')}</span></div>}
              <div className="post-card-body">
                <div className="post-meta"><span>{post.categories?.[0]?.name || 'Essay'}</span><time>{formatDate(post.published_at)}</time></div>
                <h3><Link to={`/posts/${post.slug}`}>{post.title}</Link></h3>
                <p>{post.summary || 'Open this story to read the full piece.'}</p>
                <div className="post-author"><span className="avatar">{post.author?.username?.[0]?.toUpperCase() || 'B'}</span><span>{post.author?.username || 'Blog author'}</span></div>
              </div>
            </article>
          ))}
        </div>
        {result && result.pagination.total_pages > 1 && <nav className="pagination" aria-label="Story pages">
          <button disabled={page <= 1} onClick={() => setParams({ page: String(page - 1) })}>← Newer</button>
          <span>{page} / {result.pagination.total_pages}</span>
          <button disabled={page >= result.pagination.total_pages} onClick={() => setParams({ page: String(page + 1) })}>Older →</button>
        </nav>}
      </section>
    </main>
  )
}
