import { useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { useAuth } from '../auth'

export function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const { login, register } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault(); setError(''); setBusy(true)
    try {
      if (mode === 'login') await login(email, password)
      else await register(email, username, password)
      const destination = (location.state as { from?: string } | null)?.from || '/'
      navigate(destination, { replace: true })
    } catch (err) { setError(err instanceof Error ? err.message : 'Authentication failed') }
    finally { setBusy(false) }
  }

  const loginMode = mode === 'login'
  return <main className="auth-page">
    <section className="auth-panel">
      <Link className="brand auth-brand" to="/"><span className="brand-mark">B</span><span>Blog<span className="brand-dot">.</span></span></Link>
      <div className="eyebrow">{loginMode ? 'Welcome back' : 'Create your account'}</div>
      <h1>{loginMode ? 'Return to your reading.' : 'Start writing your story.'}</h1>
      <p>{loginMode ? 'Sign in to write, comment, and continue where you left off.' : 'Join a small community built around clear ideas and generous discussion.'}</p>
      {error && <div className="form-error" role="alert">{error}</div>}
      <form className="form-stack" onSubmit={submit}>
        <label>Email<input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required /></label>
        {!loginMode && <label>Username<input minLength={3} maxLength={32} autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required /></label>}
        <label>Password<input type="password" minLength={8} autoComplete={loginMode ? 'current-password' : 'new-password'} value={password} onChange={(e) => setPassword(e.target.value)} required /></label>
        <button className="button full" disabled={busy}>{busy ? 'Please wait…' : loginMode ? 'Sign in' : 'Create account'}</button>
      </form>
      <p className="auth-switch">{loginMode ? 'New here?' : 'Already have an account?'} <Link to={loginMode ? '/register' : '/login'} state={location.state}>{loginMode ? 'Create an account' : 'Sign in'}</Link></p>
    </section>
    <aside className="auth-aside"><blockquote>“The role of a writer is not to say what we can all say, but what we are unable to say.”</blockquote><span>— Anaïs Nin</span></aside>
  </main>
}
