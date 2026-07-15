import { NavLink, Outlet, Link, useNavigate } from 'react-router'
import { useAuth } from './auth'

export function AppLayout() {
  const { user, profile, logout } = useAuth()
  const navigate = useNavigate()

  async function signOut() {
    try { await logout() }
    finally { navigate('/') }
  }

  return (
    <div className="app-frame">
      <header className="site-header">
        <Link className="brand" to="/" aria-label="Blog home">
          <span className="brand-mark">B</span>
          <span>Blog<span className="brand-dot">.</span></span>
        </Link>
        <nav className="main-nav" aria-label="Main navigation">
          <NavLink to="/" end>Stories</NavLink>
          <NavLink to="/ask">Ask AI</NavLink>
          {user && <NavLink to="/write">Write</NavLink>}
          {user?.role === 'admin' && <NavLink to="/admin/taxonomy">Taxonomy</NavLink>}
        </nav>
        <div className="account-nav">
          {user ? (
            <>
              <span className="user-chip" title={user.email}>{profile?.display_name || user.username}</span>
              <button className="text-button" onClick={() => void signOut()}>Sign out</button>
            </>
          ) : (
            <>
              <Link className="text-link" to="/login">Sign in</Link>
              <Link className="button compact" to="/register">Join</Link>
            </>
          )}
        </div>
      </header>
      <Outlet />
      <footer className="site-footer">
        <div><strong>Blog.</strong><span>Thoughtful writing, openly shared.</span></div>
        <span>Stage 4 · Grounded by published stories</span>
      </footer>
    </div>
  )
}
