import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { EmptyState } from './components'

describe('EmptyState', () => {
  it('renders an accessible empty message', () => {
    render(<MemoryRouter><EmptyState title="No stories">Publish the first one.</EmptyState></MemoryRouter>)
    expect(screen.getByText('No stories')).toBeInTheDocument()
    expect(screen.getByText('Publish the first one.')).toBeInTheDocument()
  })
})
