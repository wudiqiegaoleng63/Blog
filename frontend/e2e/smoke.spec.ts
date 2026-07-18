import { expect, test, type Page, type Route } from '@playwright/test'

type SmokeState = {
  loggedIn: boolean
  post: { slug: string; title: string; content_html: string; content_markdown: string; status: string; visibility: string; published_at: string; created_at: string; author: { public_id: string; username: string } }
  category: { public_id: string; name: string; slug: string }
  comment: { public_id: string; body_html: string; created_at: string; author: { public_id: string; username: string } }
}

function success(data: unknown) {
  return { success: true, data }
}

async function json(route: Route, status: number, body: unknown) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function installAPI(page: Page): Promise<SmokeState> {
  const state: SmokeState = {
    loggedIn: false,
    post: {
      slug: 'smoke-story',
      title: 'A browser-tested story',
      content_html: '<p>The browser smoke story is public.</p>',
      content_markdown: 'The browser smoke story is public.',
      status: 'published',
      visibility: 'public',
      published_at: '2026-07-18T00:00:00Z',
      created_at: '2026-07-18T00:00:00Z',
      author: { public_id: 'user-smoke', username: 'smoke_admin' },
    },
    category: { public_id: 'cat-smoke', name: 'Essays', slug: 'essays' },
    comment: { public_id: 'comment-smoke', body_html: '<p>A thoughtful comment</p>', created_at: '2026-07-18T00:00:00Z', author: { public_id: 'user-smoke', username: 'smoke_admin' } },
  }

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname.replace('/api/v1', '')
    const method = request.method()

    if (method === 'GET' && path === '/auth/me') {
      if (!state.loggedIn) return json(route, 401, { success: false, request_id: 'smoke', error: { code: 'unauthorized', message: 'Sign in required' } })
      return json(route, 200, success({
        user: { public_id: 'user-smoke', username: 'smoke_admin', email: 'smoke@example.test', role: 'admin', created_at: '2026-07-18T00:00:00Z' },
        profile: { display_name: 'Smoke Admin' },
      }))
    }
    if (method === 'POST' && (path === '/auth/register' || path === '/auth/login')) {
      state.loggedIn = true
      return json(route, 200, success({ access_token: 'smoke-access-token', expires_in: 900, token_type: 'Bearer' }))
    }
    if (method === 'POST' && path === '/auth/refresh') {
      return state.loggedIn
        ? json(route, 200, success({ access_token: 'smoke-refreshed-token', expires_in: 900, token_type: 'Bearer' }))
        : json(route, 401, { success: false, error: { code: 'unauthorized', message: 'Refresh expired' } })
    }
    if (method === 'POST' && path === '/auth/logout') {
      state.loggedIn = false
      return json(route, 200, success({ message: 'logged out' }))
    }
    if (method === 'GET' && path === '/posts') {
      return json(route, 200, success({ posts: [state.post], pagination: { page: 1, page_size: 9, total: 1, total_pages: 1 } }))
    }
    if (method === 'GET' && path === `/posts/${state.post.slug}`) return json(route, 200, success({ post: state.post }))
    if (method === 'GET' && path === '/posts/missing') return json(route, 404, { success: false, error: { code: 'not_found', message: 'post not found' } })
    if (method === 'POST' && path === '/posts') return json(route, 201, success({ post: state.post }))
    if (method === 'GET' && path === `/posts/${state.post.slug}/comments`) {
      return json(route, 200, success({ comments: [{ ...state.comment, status: 'approved' }], pagination: { page: 1, page_size: 20, total: 1, total_pages: 1 } }))
    }
    if (method === 'POST' && path === `/posts/${state.post.slug}/comments`) {
      return json(route, 201, success({ comment: { public_id: state.comment.public_id, body_markdown: 'A thoughtful comment', body_html: state.comment.body_html, status: 'pending', author: { username: 'smoke_admin', public_id: 'user-smoke' } } }))
    }
    if (method === 'GET' && path === '/categories') return json(route, 200, success({ categories: [state.category] }))
    if (method === 'GET' && path === '/tags') return json(route, 200, success({ tags: [] }))
    if (method === 'POST' && path === '/categories') return json(route, 201, success({ category: state.category }))
    if (method === 'POST' && path === '/ai/ask') {
      return json(route, 200, success({ answer: 'The answer is grounded in the browser smoke story.', sources: [{ post_id: 'post-smoke', title: state.post.title, slug: state.post.slug, excerpt: 'The browser smoke story is public.', score: 0.99 }] }))
    }
    return json(route, 204, undefined)
  })

  return state
}

test('core reader and author journey survives a browser reload', async ({ page }) => {
  await installAPI(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /Stories worth/i })).toBeVisible()
  await expect(page.getByRole('link', { name: 'A browser-tested story' })).toBeVisible()

  await page.getByRole('link', { name: 'Join' }).click()
  await page.getByLabel('Email').fill('smoke@example.test')
  await page.getByLabel('Username').fill('smoke_admin')
  await page.getByLabel('Password').fill('safe-password-123')
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()

  await page.reload()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()

  await page.getByRole('link', { name: 'Write' }).click()
  await page.getByLabel('Title').fill('A browser-tested story')
  await page.getByLabel('Story').fill('The browser smoke story is public.')
  await page.getByLabel('Status').selectOption('published')
  await page.getByRole('button', { name: 'Publish story' }).click()
  await expect(page).toHaveURL(/\/posts\/smoke-story$/)
  await expect(page.getByRole('heading', { name: 'A browser-tested story' })).toBeVisible()

  await page.getByPlaceholder(/Add to the conversation/i).fill('A thoughtful comment')
  await page.getByRole('button', { name: 'Post comment' }).click()
  await expect(page.getByRole('status')).toContainText(/Comment submitted|Comment published/)

  await page.getByRole('link', { name: 'Ask AI' }).click()
  await page.getByLabel('Your question').fill('What is this story about?')
  await page.getByRole('button', { name: 'Ask' }).click()
  await expect(page.getByText('The answer is grounded in the browser smoke story.')).toBeVisible()
  await expect(page.getByRole('link', { name: 'A browser-tested story' }).last()).toBeVisible()

  await page.getByRole('link', { name: 'Taxonomy' }).click()
  await expect(page.getByRole('heading', { name: 'Organize the journal.' })).toBeVisible()
})

test('deep links and API errors render usable states', async ({ page }) => {
  await installAPI(page)
  await page.goto('/posts/smoke-story')
  await expect(page.getByRole('heading', { name: 'A browser-tested story' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'A browser-tested story' })).toBeVisible()

  await page.goto('/posts/missing')
  await expect(page.getByRole('alert')).toContainText('post not found')
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible()
})
