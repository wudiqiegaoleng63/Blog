import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Link, Navigate, Outlet, useLocation } from 'react-router'
import { useAuth } from './auth'

export function LoadingState({ label = 'Loading…' }: { label?: string }) {
  return <div className="state-card" role="status"><span className="spinner" />{label}</div>
}

export function EmptyState({ title, children }: { title: string; children: ReactNode }) {
  return <div className="state-card"><strong>{title}</strong><p>{children}</p></div>
}

export function ErrorState({ message, retry }: { message: string; retry?: () => void }) {
  return <div className="state-card error-state" role="alert"><strong>Something went wrong</strong><p>{message}</p>{retry && <button className="button ghost" onClick={retry}>Try again</button>}</div>
}

export function ProtectedRoute({ admin = false }: { admin?: boolean }) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return <LoadingState label="Restoring your session…" />
  if (!user) return <Navigate to="/login" replace state={{ from: `${location.pathname}${location.search}${location.hash}` }} />
  if (admin && user.role !== 'admin') return <Navigate to="/" replace />
  return <Outlet />
}

interface BoundaryState { error: Error | null }
export class ErrorBoundary extends Component<{ children: ReactNode }, BoundaryState> {
  state: BoundaryState = { error: null }
  static getDerivedStateFromError(error: Error): BoundaryState { return { error } }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error('Uncaught frontend error', error, info) }
  render() {
    if (this.state.error) {
      return <main className="page-shell"><ErrorState message={this.state.error.message} /><Link className="text-link" to="/">Return home</Link></main>
    }
    return this.props.children
  }
}

export function ConfirmButton({ children, onConfirm, label = 'Confirm this action?' }: { children: ReactNode; onConfirm: () => void | Promise<void>; label?: string }) {
  return <button className="button danger" onClick={() => { if (window.confirm(label)) void onConfirm() }}>{children}</button>
}
