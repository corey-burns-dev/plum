import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import * as api from './api'
import App from './App'

describe('App library and player wiring', () => {
  it('renders media items from the API and shows empty state when none', async () => {
    vi.spyOn(api, 'fetchMediaList').mockResolvedValueOnce([])

    render(<App />)

    // Empty state should be visible initially when no items are returned.
    expect(
      await screen.findByText(/No media found in Plum/i),
    ).toBeInTheDocument()
  })

  it('renders media list, allows selection, and wires player src', async () => {
    const items: api.MediaItem[] = [
      {
        id: 42,
        title: 'Test Show S01E01',
        path: '/tv/TestShow/S01E01.mkv',
        duration: 1800,
        type: 'tv',
      },
    ]

    vi.spyOn(api, 'fetchMediaList').mockResolvedValueOnce(items)

    render(<App />)

    // Library item appears.
    const row = await screen.findByText('Test Show S01E01')
    expect(row).toBeInTheDocument()

    // Select the item.
    fireEvent.click(row)

    // Player panel should now show title and correct stream URL.
    expect(
      await screen.findByText('Test Show S01E01'),
    ).toBeInTheDocument()

    const video = screen.getByRole('video', { hidden: true }) as HTMLVideoElement | undefined
    // Fallback: query by selector if role lookup fails in jsdom.
    const el = video ?? (document.querySelector('video.player-video') as HTMLVideoElement)

    expect(el).not.toBeNull()
    // The src attribute should point at the backend stream endpoint.
    expect(el?.getAttribute('src')).toMatch(/\/api\/stream\/42$/)
  })
})

