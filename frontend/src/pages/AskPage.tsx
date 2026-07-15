import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router'
import { api, ApiError } from '../api'
import type { AIAnswer } from '../types'

export function AskPage() {
  const [question, setQuestion] = useState('')
  const [result, setResult] = useState<AIAnswer | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    const value = question.trim()
    if (!value || loading) return
    setLoading(true)
    setError('')
    setResult(null)
    try {
      setResult(await api.askAI(value))
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The article assistant is temporarily unavailable.')
    } finally {
      setLoading(false)
    }
  }

  return <main className="page-shell ask-page">
    <section className="ask-hero">
      <div className="eyebrow">Article assistant</div>
      <h1>Ask the published stories.</h1>
      <p>Answers are grounded in the public articles and include links back to their sources.</p>
      <form className="ask-form" onSubmit={(event) => void submit(event)}>
        <label htmlFor="rag-question">Your question</label>
        <textarea id="rag-question" value={question} maxLength={2000} rows={5}
          onChange={(event) => setQuestion(event.target.value)}
          placeholder="What do these articles say about…?" />
        <div className="ask-actions"><span>{question.length} / 2000</span><button className="button" disabled={loading || !question.trim()}>{loading ? 'Searching…' : 'Ask'}</button></div>
      </form>
    </section>

    {error && <section className="error-state" role="alert"><strong>Question failed</strong><p>{error}</p></section>}
    {result && <section className="answer-panel" aria-live="polite">
      <div className="eyebrow">Grounded answer</div>
      <p className="answer-text">{result.answer}</p>
      <h2>Sources</h2>
      {result.sources.length === 0 ? <p className="muted">No sufficiently relevant published sources were found.</p> :
        <div className="source-list">{result.sources.map((source) => <Link className="source-card" key={source.post_id} to={`/posts/${source.slug}`}>
          <div><strong>{source.title}</strong><span>Similarity {Math.round(source.score * 100)}%</span></div>
          <p>{source.excerpt}</p>
        </Link>)}</div>}
    </section>}
  </main>
}
