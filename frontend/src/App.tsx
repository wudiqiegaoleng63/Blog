import { BrowserRouter, Route, Routes } from 'react-router'
import { AuthProvider } from './auth'
import { ErrorBoundary, ProtectedRoute } from './components'
import { AppLayout } from './layout'
import { AskPage } from './pages/AskPage'
import { AuthPage } from './pages/AuthPage'
import { EditorPage } from './pages/EditorPage'
import { HomePage } from './pages/HomePage'
import { PostPage } from './pages/PostPage'
import { TaxonomyPage } from './pages/TaxonomyPage'
import './App.css'

function NotFoundPage() {
  return <main className="page-shell state-page"><div className="eyebrow">404</div><h1>This page wandered off.</h1><p>The story or page you requested does not exist.</p><a className="button" href="/">Return home</a></main>
}

export default function App() {
  return <BrowserRouter><ErrorBoundary><AuthProvider><Routes>
    <Route element={<AppLayout />}>
      <Route index element={<HomePage />} />
      <Route path="ask" element={<AskPage />} />
      <Route path="posts/:slug" element={<PostPage />} />
      <Route element={<ProtectedRoute />}><Route path="write" element={<EditorPage />} /></Route>
      <Route element={<ProtectedRoute admin />}><Route path="admin/taxonomy" element={<TaxonomyPage />} /></Route>
      <Route path="*" element={<NotFoundPage />} />
    </Route>
    <Route path="login" element={<AuthPage mode="login" />} />
    <Route path="register" element={<AuthPage mode="register" />} />
  </Routes></AuthProvider></ErrorBoundary></BrowserRouter>
}
