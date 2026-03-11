import { describe, expect, it, vi, beforeEach } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import * as api from './api'
import App from './App'

function renderApp() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  )
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('App library and player wiring', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.history.pushState({}, '', '/')
    vi.spyOn(api, 'getSetupStatus').mockResolvedValue({ hasAdmin: true })
    vi.spyOn(api, 'getMe').mockResolvedValue({
      id: 1,
      email: 'test@test.com',
      is_admin: true,
    })
    vi.spyOn(api, 'identifyLibrary').mockResolvedValue({ identified: 0, failed: 0 })
    vi.spyOn(api, 'startTranscode').mockResolvedValue()
  })

  it('renders library tab and show cards when TV library has media', async () => {
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 1, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([
      {
        id: 42,
        title: 'Test Show - S01E01 - Pilot',
        path: '/tv/TestShow/S01E01.mkv',
        duration: 1800,
        type: 'tv',
        tmdb_id: 100,
        poster_path: '/poster.jpg',
        season: 1,
        episode: 1,
      },
    ])

    renderApp()

    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledWith(1)
    })

    expect(await screen.findByRole('link', { name: /TV/i })).toBeTruthy()
    expect(await screen.findByText('Test Show')).toBeTruthy()
  })

  it('renders movie library as poster cards', async () => {
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 2, name: 'Movies', type: 'movie', path: '/movies', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([
      {
        id: 99,
        title: 'Die My Love',
        path: '/movies/Die My Love (2025)/Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio.mp4',
        duration: 7200,
        type: 'movie',
        poster_path: '/poster.jpg',
        release_date: '2025-01-01',
      },
    ])

    renderApp()

    const movieCard = await screen.findByRole('button', { name: /Die My Love/i })
    expect(movieCard).toBeTruthy()
    expect(screen.getByText(/2025/)).toBeTruthy()
  })

  it('shows identifying state only on incomplete TV cards while background identify is active', async () => {
    const identifyRequest = deferred<{ identified: number; failed: number }>()

    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 1, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([
      {
        id: 42,
        title: 'Searching Show - S01E01 - Pilot',
        path: '/tv/Searching Show/S01E01.mkv',
        duration: 1800,
        type: 'tv',
        match_status: 'local',
        season: 1,
        episode: 1,
      },
      {
        id: 99,
        title: 'Matched Show - S01E01 - Pilot',
        path: '/tv/Matched Show/S01E01.mkv',
        duration: 1800,
        type: 'tv',
        match_status: 'identified',
        tmdb_id: 200,
        poster_path: '/poster.jpg',
        season: 1,
        episode: 1,
      },
    ])
    vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise)

    renderApp()

    const searchingCard = await screen.findByRole('link', { name: /Searching Show/i })
    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1)
    })
    expect(vi.mocked(api.identifyLibrary).mock.calls[0]?.[0]).toBe(1)

    expect(within(searchingCard).getByText('Identifying…')).toBeVisible()
    expect(screen.getAllByText('Identifying…')).toHaveLength(1)
  })

  it('shows and clears identifying state on incomplete movie cards as metadata refreshes', async () => {
    const identifyRequest = deferred<{ identified: number; failed: number }>()

    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 2, name: 'Movies', type: 'movie', path: '/movies', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia')
      .mockResolvedValueOnce([
        {
          id: 99,
          title: 'Die My Love',
          path: '/movies/Die My Love (2025)/Die My Love.mp4',
          duration: 7200,
          type: 'movie',
          match_status: 'unmatched',
        },
        {
          id: 100,
          title: 'Already Matched',
          path: '/movies/Already Matched (2024)/Already Matched.mp4',
          duration: 7100,
          type: 'movie',
          match_status: 'identified',
          poster_path: '/poster.jpg',
          release_date: '2024-01-01',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 99,
          title: 'Die My Love',
          path: '/movies/Die My Love (2025)/Die My Love.mp4',
          duration: 7200,
          type: 'movie',
          match_status: 'identified',
          poster_path: '/poster-identified.jpg',
          release_date: '2025-01-01',
        },
        {
          id: 100,
          title: 'Already Matched',
          path: '/movies/Already Matched (2024)/Already Matched.mp4',
          duration: 7100,
          type: 'movie',
          match_status: 'identified',
          poster_path: '/poster.jpg',
          release_date: '2024-01-01',
        },
      ])
    vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise)

    renderApp()

    const movieCard = await screen.findByRole('button', { name: /Die My Love/i })
    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1)
    })
    expect(vi.mocked(api.identifyLibrary).mock.calls[0]?.[0]).toBe(2)

    expect(within(movieCard).getByText('Identifying…')).toBeVisible()
    expect(screen.getAllByText('Identifying…')).toHaveLength(1)

    identifyRequest.resolve({ identified: 1, failed: 0 })

    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => {
      expect(screen.queryByText('Identifying…')).not.toBeInTheDocument()
    })
  })

  it('runs background identify for all non-music libraries sequentially', async () => {
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 1, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
      { id: 2, name: 'Movies', type: 'movie', path: '/movies', user_id: 1 },
      { id: 3, name: 'Music', type: 'music', path: '/music', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([])
    const firstIdentify = deferred<{ identified: number; failed: number }>()
    const secondIdentify = deferred<{ identified: number; failed: number }>()
    vi.mocked(api.identifyLibrary)
      .mockImplementationOnce(() => firstIdentify.promise)
      .mockImplementationOnce(() => secondIdentify.promise)

    renderApp()

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1)
    })
    expect(vi.mocked(api.identifyLibrary).mock.calls[0]?.[0]).toBe(1)

    await Promise.resolve()
    expect(api.identifyLibrary).toHaveBeenCalledTimes(1)

    firstIdentify.resolve({ identified: 1, failed: 0 })

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2)
    })
    expect(vi.mocked(api.identifyLibrary).mock.calls[1]?.[0]).toBe(2)
    expect(vi.mocked(api.identifyLibrary).mock.calls.map((call) => call[0])).not.toContain(3)

    secondIdentify.resolve({ identified: 1, failed: 0 })
  })

  it('navigates to show detail and shows episode list with Play', async () => {
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 1, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([
      {
        id: 42,
        title: 'Test Show - S01E01 - Pilot',
        path: '/tv/TestShow/S01E01.mkv',
        duration: 1800,
        type: 'tv',
        tmdb_id: 100,
        poster_path: '/poster.jpg',
        season: 1,
        episode: 1,
      },
    ])

    renderApp()

    await screen.findByText('Test Show')
    fireEvent.click(screen.getByRole('link', { name: /Test Show/i }))

    expect(await screen.findByRole('link', { name: /Back to library/i })).toBeTruthy()
    const playButton = await screen.findByRole('button', { name: /Play/i })
    fireEvent.click(playButton)
    expect(api.startTranscode).toHaveBeenCalledWith(42)
  })

  it('renders music sections and opens the bottom player without transcoding', async () => {
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 3, name: 'Music', type: 'music', path: '/music', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([
      {
        id: 11,
        title: 'Track One',
        path: '/music/Artist/Album/01 - Track One.flac',
        duration: 245,
        type: 'music',
        artist: 'Artist',
        album: 'Album',
        album_artist: 'Artist',
        track_number: 1,
      },
      {
        id: 12,
        title: 'Track Two',
        path: '/music/Artist/Album/02 - Track Two.flac',
        duration: 255,
        type: 'music',
        artist: 'Artist',
        album: 'Album',
        album_artist: 'Artist',
        track_number: 2,
      },
    ])

    renderApp()

    expect(await screen.findByText('Tracks')).toBeTruthy()
    expect(screen.getByText('Albums')).toBeTruthy()
    expect(screen.getByText('Artists')).toBeTruthy()
    expect(screen.getByText('Genres')).toBeTruthy()
    expect(screen.getByText('Playlists')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: /Track One/i }))

    expect(await screen.findByLabelText('Music player')).toBeTruthy()
    expect(screen.getByRole('button', { name: /Enable shuffle/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Previous track/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Next track/i })).toBeTruthy()
    expect(api.startTranscode).not.toHaveBeenCalled()
  })

  it('retries auto-identify after a failed first attempt', async () => {
    const firstIdentify = deferred<{ identified: number; failed: number }>()
    const secondIdentify = deferred<{ identified: number; failed: number }>()

    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 1, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia')
      .mockResolvedValueOnce([
        {
          id: 42,
          title: 'Retry Show - S01E01 - Pilot',
          path: '/tv/Retry Show/S01E01.mkv',
          duration: 1800,
          type: 'tv',
          match_status: 'local',
          season: 1,
          episode: 1,
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 42,
          title: 'Retry Show - S01E01 - Pilot',
          path: '/tv/Retry Show/S01E01.mkv',
          duration: 1800,
          type: 'tv',
          match_status: 'identified',
          tmdb_id: 100,
          poster_path: '/poster.jpg',
          season: 1,
          episode: 1,
        },
      ])
    vi.mocked(api.identifyLibrary)
      .mockImplementationOnce(() => firstIdentify.promise)
      .mockImplementationOnce(() => secondIdentify.promise)

    renderApp()

    const retryCard = await screen.findByRole('link', { name: /Retry Show/i })
    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledWith(1)
    })
    expect(within(retryCard).getByText('Identifying…')).toBeVisible()

    firstIdentify.reject(new Error('temporary failure'))

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2)
    })

    secondIdentify.resolve({ identified: 1, failed: 0 })

    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => {
      expect(screen.queryByText('Identifying…')).not.toBeInTheDocument()
    })
  })

  it('finishes onboarding after scan-only import without waiting for identify', async () => {
    vi.spyOn(api, 'getSetupStatus')
      .mockResolvedValueOnce({ hasAdmin: false })
      .mockResolvedValueOnce({ hasAdmin: true })
    vi.spyOn(api, 'createAdmin').mockResolvedValue({
      id: 1,
      email: 'admin@example.com',
      is_admin: true,
    })
    vi.spyOn(api, 'createLibrary').mockResolvedValue({
      id: 10,
      name: 'TV',
      type: 'tv',
      path: '/tv',
      user_id: 1,
    })
    vi.spyOn(api, 'scanLibraryById').mockResolvedValue({
      added: 3,
      updated: 0,
      removed: 0,
      unmatched: 1,
      skipped: 0,
    })
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 10, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([])
    vi.mocked(api.identifyLibrary).mockImplementation(() => new Promise(() => {}))

    renderApp()

    expect(await screen.findByRole('heading', { name: /Create admin account/i })).toBeTruthy()

    fireEvent.change(screen.getByLabelText(/Email/i), {
      target: { value: 'admin@example.com' },
    })
    fireEvent.change(screen.getByLabelText(/^Password/i), {
      target: { value: 'passwordpassword' },
    })
    fireEvent.change(screen.getByLabelText(/Confirm password/i), {
      target: { value: 'passwordpassword' },
    })
    fireEvent.click(screen.getByRole('button', { name: /Create admin/i }))

    expect(await screen.findByRole('heading', { name: /Add libraries/i })).toBeTruthy()

    fireEvent.change(screen.getByLabelText(/Library name/i), {
      target: { value: 'TV' },
    })
    fireEvent.change(screen.getByLabelText(/Folder path/i), {
      target: { value: '/tv' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^Add library$/i }))

    await waitFor(() => {
      expect(api.scanLibraryById).toHaveBeenCalledWith(10, { identify: false })
    })

    fireEvent.click(screen.getByRole('button', { name: /Finish setup/i }))

    expect(await screen.findByText(/No media in this library yet/i)).toBeTruthy()
    expect(vi.mocked(api.identifyLibrary).mock.calls.map((call) => call[0])).toContain(10)
  })

  it('auto-enters the app after adding default libraries with scan-only import', async () => {
    vi.spyOn(api, 'getSetupStatus')
      .mockResolvedValueOnce({ hasAdmin: false })
      .mockResolvedValueOnce({ hasAdmin: true })
    vi.spyOn(api, 'createAdmin').mockResolvedValue({
      id: 1,
      email: 'admin@example.com',
      is_admin: true,
    })
    vi.spyOn(api, 'createLibrary')
      .mockResolvedValueOnce({ id: 11, name: 'TV', type: 'tv', path: '/tv', user_id: 1 })
      .mockResolvedValueOnce({ id: 12, name: 'Movies', type: 'movie', path: '/movies', user_id: 1 })
      .mockResolvedValueOnce({ id: 13, name: 'Anime', type: 'anime', path: '/anime', user_id: 1 })
      .mockResolvedValueOnce({ id: 14, name: 'Music', type: 'music', path: '/music', user_id: 1 })
    vi.spyOn(api, 'scanLibraryById').mockResolvedValue({
      added: 1,
      updated: 0,
      removed: 0,
      unmatched: 0,
      skipped: 0,
    })
    vi.spyOn(api, 'listLibraries').mockResolvedValue([
      { id: 11, name: 'TV', type: 'tv', path: '/tv', user_id: 1 },
      { id: 12, name: 'Movies', type: 'movie', path: '/movies', user_id: 1 },
      { id: 13, name: 'Anime', type: 'anime', path: '/anime', user_id: 1 },
      { id: 14, name: 'Music', type: 'music', path: '/music', user_id: 1 },
    ])
    vi.spyOn(api, 'fetchLibraryMedia').mockResolvedValue([])

    renderApp()

    expect(await screen.findByRole('heading', { name: /Create admin account/i })).toBeTruthy()

    fireEvent.change(screen.getByLabelText(/Email/i), {
      target: { value: 'admin@example.com' },
    })
    fireEvent.change(screen.getByLabelText(/^Password/i), {
      target: { value: 'passwordpassword' },
    })
    fireEvent.change(screen.getByLabelText(/Confirm password/i), {
      target: { value: 'passwordpassword' },
    })
    fireEvent.click(screen.getByRole('button', { name: /Create admin/i }))

    expect(await screen.findByRole('heading', { name: /Add libraries/i })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Add default libraries/i }))

    await waitFor(() => {
      expect(api.scanLibraryById).toHaveBeenNthCalledWith(1, 11, { identify: false })
      expect(api.scanLibraryById).toHaveBeenNthCalledWith(2, 12, { identify: false })
      expect(api.scanLibraryById).toHaveBeenNthCalledWith(3, 13, { identify: false })
      expect(api.scanLibraryById).toHaveBeenNthCalledWith(4, 14, { identify: false })
    })

    expect(await screen.findByText(/No media in this library yet/i)).toBeTruthy()
  })
})
