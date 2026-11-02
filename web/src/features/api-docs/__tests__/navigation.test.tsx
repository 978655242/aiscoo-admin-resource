/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { ThemeProvider } from '@/context/theme-provider'
import { Hero } from '@/features/home/components/sections/hero'
import zh from '@/i18n/locales/zh.json'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { ApiDocs } from '..'

const clients: QueryClient[] = []
const initialClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')

function renderPage(initial = '/docs') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  clients.push(client)
  client.setQueryData(['status'], {
    docs_link: 'https://docs.newapi.pro',
    server_address: 'https://configured.example:8443',
  })
  client.setQueryData(['notice'], { success: true, data: '' })
  const root = createRootRoute({ component: Outlet })
  const docs = createRoute({
    getParentRoute: () => root,
    path: '/docs',
    component: ApiDocs,
  })
  const detail = createRoute({
    getParentRoute: () => root,
    path: '/docs/$page',
    component: Detail,
  })
  const home = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: Home,
  })
  const router = createRouter({
    routeTree: root.addChildren([docs, detail, home]),
    history: createMemoryHistory({ initialEntries: [initial] }),
  })
  function Detail() {
    return <ApiDocs page={detail.useParams().page} />
  }
  function Home() {
    return <Hero />
  }
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <RouterProvider router={router} />
      </ThemeProvider>
    </QueryClientProvider>
  )
  return router
}

beforeEach(() => {
  localStorage.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useSystemConfigStore.setState({ loading: false })
})

afterEach(async () => {
  clients.splice(0).forEach((client) => client.clear())
  await i18next.changeLanguage('en')
  vi.restoreAllMocks()
  if (initialClipboard) {
    Object.defineProperty(navigator, 'clipboard', initialClipboard)
  } else {
    Reflect.deleteProperty(navigator, 'clipboard')
  }
  i18next.removeResourceBundle('zhCN', 'translation')
  localStorage.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

test('legacy external documentation settings keep the header and hero on the built-in docs route', async () => {
  const router = renderPage('/')
  const heroLink = await screen.findByRole('link', { name: 'Docs' })
  expect(heroLink).toHaveAttribute('href', '/docs')
  expect(heroLink).not.toHaveAttribute('target')
  await userEvent.click(heroLink)
  expect(
    await screen.findByRole('heading', { name: 'API overview', level: 1 })
  ).toBeVisible()
  expect(router.state.location.pathname).toBe('/docs')
  const headerLinks = screen.getAllByRole('link', {
    name: 'Docs',
    hidden: true,
  })
  for (const link of headerLinks) {
    expect(link).toHaveAttribute('href', '/docs')
    expect(link).not.toHaveAttribute('target')
  }
})

test('mobile directory supports keyboard expansion, internal navigation, and closes after selection', async () => {
  const user = userEvent.setup()
  const router = renderPage()
  const toggle = await screen.findByRole('button', {
    name: 'API documentation navigation',
  })
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  toggle.focus()
  await user.keyboard('{Enter}')
  expect(toggle).toHaveAttribute('aria-expanded', 'true')
  const nav = screen.getByRole('navigation', { name: 'Model API reference' })
  await user.click(within(nav).getByRole('link', { name: 'Chat Completions' }))
  expect(
    await screen.findByRole('heading', { name: 'Chat Completions', level: 1 })
  ).toBeVisible()
  expect(router.state.location.pathname).toBe('/docs/chat-completions')
  const currentToggle = screen.getByRole('button', {
    name: 'API documentation navigation',
  })
  expect(currentToggle).toHaveAttribute('aria-expanded', 'false')
  await user.click(currentToggle)
  expect(
    within(
      await screen.findByRole('navigation', { name: 'Model API reference' })
    ).getByRole('link', { name: 'Chat Completions' })
  ).toHaveAttribute('aria-current', 'page')
})

test.each([
  ['chat-completions', 'Text and multimodal', 'Chat Completions'],
  ['images', 'Media and utilities', 'Images'],
  ['errors', 'API basics', 'Errors and retries'],
  ['tasks', 'API basics', 'Tasks and artifacts'],
  ['responses-compact', 'Text and multimodal', 'Responses compaction'],
])(
  'opening %s shows its category and title in the breadcrumb',
  async (page, group, title) => {
    renderPage(`/docs/${page}`)
    const breadcrumb = await screen.findByRole('navigation', { name: 'Docs' })
    expect(within(breadcrumb).getByText(group)).toBeVisible()
    expect(
      within(breadcrumb).getByRole('link', { name: title, current: 'page' })
    ).toBeVisible()
    expect(
      within(breadcrumb).getByRole('link', { name: 'Docs' })
    ).toHaveAttribute('href', '/docs')
  }
)

test('navigating between documents updates the breadcrumb and its docs link returns to the overview', async () => {
  const user = userEvent.setup()
  renderPage('/docs/chat-completions')
  await screen.findByRole('heading', { name: 'Chat Completions', level: 1 })
  await user.click(
    within(
      screen.getByRole('navigation', { name: 'Documentation pages' })
    ).getByRole('link', { name: /Next page\s*Responses/ })
  )
  const breadcrumb = await screen.findByRole('navigation', { name: 'Docs' })
  expect(
    within(breadcrumb).getByRole('link', { name: 'Responses', current: 'page' })
  ).toBeVisible()
  expect(
    within(breadcrumb).queryByText('Chat Completions')
  ).not.toBeInTheDocument()
  await user.click(within(breadcrumb).getByRole('link', { name: 'Docs' }))
  expect(
    await screen.findByRole('heading', { name: 'API overview', level: 1 })
  ).toBeVisible()
  expect(
    within(screen.getByRole('navigation', { name: 'Docs' })).getByRole('link', {
      name: 'API overview',
      current: 'page',
    })
  ).toBeVisible()
})

test('document layout reserves space below the fixed header and aligns sticky directories', async () => {
  renderPage()
  const main = await screen.findByRole('main')
  expect(main.parentElement).toHaveClass('pt-28', 'lg:pt-32', 'lg:items-start')
  expect(
    screen.getByRole('complementary', { name: 'API documentation navigation' })
  ).toHaveClass('lg:top-32', 'lg:max-h-[calc(100svh-10rem)]')
  expect(
    screen.getByRole('complementary', { name: 'On this page' })
  ).toHaveClass('top-32')
})

test('overview lists compatible API addresses and copies the selected URL with its port', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText')
  renderPage()
  const table = await screen.findByRole('table', { name: 'Base URL' })
  expect(
    within(table)
      .getAllByRole('columnheader')
      .map((header) => header.textContent)
  ).toEqual(['API', 'API Base URL', 'Common clients'])
  const rows = within(table).getAllByRole('row')
  expect(rows).toHaveLength(3)
  const entries = [
    {
      row: rows[1],
      api: 'OpenAI-compatible API',
      url: `${window.location.origin}/v1`,
      clients: 'OpenAI SDK, LangChain, and most agent frameworks',
    },
    {
      row: rows[2],
      api: 'Anthropic-compatible API',
      url: window.location.origin,
      clients: 'Anthropic SDK, Claude Code',
    },
  ]
  for (const entry of entries) {
    expect(
      within(entry.row)
        .getAllByRole('cell')
        .map((cell) => cell.textContent)
    ).toEqual([entry.api, entry.url, entry.clients])
    expect(within(entry.row).getByText(entry.url)).toHaveClass('break-all')
    const button = within(entry.row).getByRole('button', {
      name: `Copy API URL: ${entry.url}`,
    })
    button.focus()
    await user.keyboard('{Enter}')
    expect(copy).toHaveBeenLastCalledWith(entry.url)
    expect(
      await within(entry.row).findByRole('button', { name: 'Copied' })
    ).toBeVisible()
  }
  expect(table).toHaveClass('min-w-[36rem]', 'table-fixed')
  expect(table.parentElement).toHaveClass('overflow-x-auto')
})

test('endpoint directory shows a final purpose column with readable descriptions', async () => {
  renderPage()
  const table = await screen.findByRole('table', {
    name: 'Model API endpoints',
  })
  expect(
    within(table)
      .getAllByRole('columnheader')
      .map((header) => header.textContent)
  ).toEqual(['API', 'Method', 'Endpoint', 'Purpose'])

  for (const [title, description] of [
    ['Models', 'List the models available to the current API key.'],
    [
      'Audio',
      'Generate speech audio from text with a compatible speech model.',
    ],
    ['Web search', 'Search the web through a compatible search-tools channel.'],
  ]) {
    const row = within(table).getByRole('row', {
      name: new RegExp(`^${title} (GET|POST) `),
    })
    const cells = within(row).getAllByRole('cell')
    expect(cells).toHaveLength(4)
    expect(cells[3]).toHaveTextContent(description)
    expect(cells[3]).toHaveClass('whitespace-normal')
  }
  expect(table).toHaveClass('min-w-[48rem]')
  expect(table.parentElement).toHaveClass('overflow-x-auto')
})

test.each([
  [
    'Text generation',
    ['chat-completions', 'responses', 'messages', 'gemini', 'completions'],
  ],
  ['Multimodal understanding', ['multimodal']],
  ['Image generation and editing', ['images', 'image-edits', 'midjourney']],
  ['Video generation and editing', ['videos']],
  [
    'Speech and realtime audio',
    ['audio', 'transcriptions', 'translations', 'realtime'],
  ],
  ['Music and lyrics', ['music']],
  ['Embeddings', ['embeddings']],
  ['Rerank', ['rerank']],
  ['Content moderation', ['moderations']],
  ['Search and web fetching', ['search', 'fetch']],
] as const)(
  'overview capability %s links to reachable endpoint references',
  async (capability, pages) => {
    const user = userEvent.setup()
    const router = renderPage()
    let table = await screen.findByRole('table', { name: 'Model capabilities' })
    expect(within(table).getAllByRole('row')).toHaveLength(11)
    const row = within(table).getByRole('row', {
      name: new RegExp(`^${capability} `),
    })
    const links = within(row).getAllByRole('link')
    expect(links.map((link) => link.getAttribute('href'))).toEqual(
      pages.map((page) => `/docs/${page}`)
    )

    for (const [index, page] of pages.entries()) {
      const title = links[index].textContent ?? ''
      await user.click(within(table).getByRole('link', { name: title }))
      expect(
        await screen.findByRole('heading', { name: title, level: 1 })
      ).toBeVisible()
      expect(router.state.location.pathname).toBe(`/docs/${page}`)
      expect(
        screen.getByRole('heading', { name: 'Request example', level: 2 })
      ).toBeVisible()
      await user.click(
        within(screen.getByRole('navigation', { name: 'Docs' })).getByRole(
          'link',
          { name: 'Docs' }
        )
      )
      table = await screen.findByRole('table', { name: 'Model capabilities' })
    }
  }
)

test.each([
  {
    page: 'transcriptions',
    title: 'Audio transcription',
    path: '/v1/audio/transcriptions',
    parameters: ['model', 'file', 'language', 'response_format'],
    formFields: ['-F "model=YOUR_MODEL_ID"', '-F "file=@audio.mp3"'],
    payload: undefined,
  },
  {
    page: 'translations',
    title: 'Audio translation',
    path: '/v1/audio/translations',
    parameters: ['model', 'file', 'response_format'],
    formFields: ['-F "model=YOUR_MODEL_ID"', '-F "file=@speech.mp3"'],
    payload: undefined,
  },
  {
    page: 'music',
    title: 'Music and lyrics',
    path: '/suno/submit/music',
    parameters: ['action', 'gpt_description_prompt', 'prompt'],
    formFields: [],
    payload: {
      gpt_description_prompt: 'An upbeat acoustic song about spring',
    },
  },
  {
    page: 'search',
    title: 'Web search',
    path: '/v1/tools/search',
    parameters: ['query'],
    formFields: [],
    payload: { query: 'DeepSeek API documentation' },
  },
])(
  'opening $page from the endpoint directory exposes its parameters and usable request payload',
  async (entry) => {
    const user = userEvent.setup()
    const copy = vi.spyOn(navigator.clipboard, 'writeText')
    const router = renderPage()
    const directory = await screen.findByRole('table', {
      name: 'Model API endpoints',
    })
    const link = within(directory).getByRole('link', { name: entry.title })
    expect(link).toHaveAttribute('href', `/docs/${entry.page}`)
    await user.click(link)
    expect(
      await screen.findByRole('heading', { name: entry.title, level: 1 })
    ).toBeVisible()
    expect(router.state.location.pathname).toBe(`/docs/${entry.page}`)
    const parameters = screen.getByRole('table')
    for (const parameter of entry.parameters) {
      expect(
        within(parameters).getByText(parameter, {
          selector: 'code',
          exact: true,
        })
      ).toBeVisible()
    }
    const request = screen.getAllByRole('tabpanel')[0]
    await user.click(within(request).getByRole('button', { name: 'Copy code' }))
    await waitFor(() =>
      expect(copy).toHaveBeenLastCalledWith(
        expect.stringContaining(`curl "${window.location.origin}${entry.path}"`)
      )
    )
    const code = copy.mock.lastCall?.[0] ?? ''
    for (const field of entry.formFields) {
      expect(code).toContain(field)
    }
    if (entry.payload) {
      const payload = code.match(/--json\s+'([^']+)'/)?.[1]
      expect(payload).toBeDefined()
      expect(JSON.parse(payload ?? '')).toEqual(entry.payload)
    }
  }
)

test('multimodal scenario tabs wrap and keep same-language examples distinct through keyboard selection and copying', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText')
  renderPage('/docs/multimodal')
  const tabs = await screen.findByRole('tablist', { name: 'Example language' })
  expect(tabs).toHaveClass('flex-wrap', 'h-auto', 'max-w-full')
  expect(
    within(tabs)
      .getAllByRole('tab')
      .map((tab) => tab.textContent)
  ).toEqual(['Image input', 'Audio input', 'Video input'])
  const image = within(tabs).getByRole('tab', { name: 'Image input' })
  const audio = within(tabs).getByRole('tab', { name: 'Audio input' })
  const video = within(tabs).getByRole('tab', { name: 'Video input' })
  expect(image).toHaveAttribute('aria-selected', 'true')
  expect(video).toHaveAttribute('aria-selected', 'false')
  const imagePanel = screen.getByRole('tabpanel', { name: 'Image input' })
  expect(
    within(imagePanel).getByText('Image input', { exact: true })
  ).toBeVisible()
  await user.click(
    within(imagePanel).getByRole('button', { name: 'Copy code' })
  )
  await waitFor(() =>
    expect(copy).toHaveBeenLastCalledWith(
      expect.stringContaining(
        `curl "${window.location.origin}/v1/chat/completions"`
      )
    )
  )
  const imageCode = copy.mock.lastCall?.[0]
  expect(imageCode).toMatch(/"type"\s*:\s*"image_url"/)

  image.focus()
  await user.keyboard('{ArrowRight}{Enter}')
  expect(audio).toHaveFocus()
  expect(audio).toHaveAttribute('aria-selected', 'true')
  expect(
    screen.getByRole('tabpanel', { name: 'Audio input' })
  ).toHaveTextContent('input_audio')
  await user.keyboard('{ArrowRight}{Enter}')
  expect(video).toHaveFocus()
  expect(video).toHaveAttribute('aria-selected', 'true')
  expect(image).toHaveAttribute('aria-selected', 'false')
  expect(
    screen.queryByRole('tabpanel', { name: 'Image input' })
  ).not.toBeInTheDocument()
  const videoPanel = screen.getByRole('tabpanel', { name: 'Video input' })
  expect(
    within(videoPanel).getByText('Video input', { exact: true })
  ).toBeVisible()
  await user.click(
    within(videoPanel).getByRole('button', { name: 'Copy code' })
  )
  await waitFor(() =>
    expect(copy).toHaveBeenLastCalledWith(
      expect.stringContaining(
        `curl "${window.location.origin}/v1beta/models/YOUR_MODEL_ID:generateContent"`
      )
    )
  )
  const videoCode = copy.mock.lastCall?.[0]
  expect(window.location.port).not.toBe('')
  expect(videoCode).toContain('x-goog-api-key: $API_KEY')
  expect(videoCode).toContain('fileData')
  expect(videoCode).not.toContain('image_url')
  expect(videoCode).not.toContain('https://configured.example:8443')
  expect(videoCode).not.toEqual(imageCode)

  video.focus()
  await user.keyboard('{ArrowLeft}{ArrowLeft}{Enter}')
  expect(image).toHaveFocus()
  expect(image).toHaveAttribute('aria-selected', 'true')
  expect(
    screen.getByRole('tabpanel', { name: 'Image input' })
  ).toHaveTextContent(`${window.location.origin}/v1/chat/completions`)
})

test('video workflow tabs copy the selected bash operation with the current origin and port', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText')
  renderPage('/docs/videos')
  const curl = await screen.findByRole('tab', { name: 'cURL' })
  expect(curl).toHaveAttribute('aria-selected', 'true')
  for (const [label, path, payload] of [
    ['cURL', '/v1/videos', '"prompt"'],
    ['Poll task', '/v1/videos/YOUR_TASK_ID', 'Authorization: Bearer $API_KEY'],
    [
      'Download result',
      '/v1/videos/YOUR_TASK_ID/content',
      '--output video.mp4',
    ],
    ['Image to video', '/v1/videos', '-F "input_reference=@reference.png"'],
  ]) {
    const tab = screen.getByRole('tab', { name: label })
    await user.click(tab)
    expect(tab).toHaveAttribute('aria-selected', 'true')
    const panel = screen.getByRole('tabpanel', { name: label })
    await user.click(within(panel).getByRole('button', { name: 'Copy code' }))
    await waitFor(() =>
      expect(copy).toHaveBeenLastCalledWith(
        expect.stringContaining(`curl "${window.location.origin}${path}"`)
      )
    )
    expect(copy.mock.lastCall?.[0]).toContain(payload)
  }
})

test('example tabs switch language by keyboard and copy code using the current origin and port', async () => {
  const user = userEvent.setup()
  const copy = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: copy },
  })
  renderPage('/docs/chat-completions')
  const curl = await screen.findByRole('tab', { name: 'cURL' })
  curl.focus()
  await user.keyboard('{ArrowRight}{Enter}')
  const python = screen.getByRole('tab', { name: 'python' })
  expect(python).toHaveAttribute('aria-selected', 'true')
  const panel = screen.getByRole('tabpanel', { name: 'python' })
  expect(panel).toHaveTextContent(`${window.location.origin}/v1`)
  await user.click(within(panel).getByRole('button', { name: 'Copy code' }))
  await waitFor(() =>
    expect(copy).toHaveBeenCalledWith(
      expect.stringContaining(`base_url="${window.location.origin}/v1"`)
    )
  )
  expect(copy.mock.calls[0][0]).not.toContain('https://configured.example:8443')
})

test('switching the interface language updates the document heading and API address table', async () => {
  renderPage()
  expect(
    await screen.findByRole('heading', { name: 'API overview', level: 1 })
  ).toBeVisible()
  i18next.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
  await act(() => i18next.changeLanguage('zhCN'))
  expect(
    screen.getByRole('heading', { name: 'API 概览', level: 1 })
  ).toBeVisible()
  const table = screen.getByRole('table', { name: 'API 地址' })
  expect(
    within(table).getByRole('columnheader', { name: 'API 基础 URL' })
  ).toBeVisible()
  expect(
    within(table).getByRole('columnheader', { name: '常见客户端' })
  ).toBeVisible()
  expect(
    within(table).getByRole('cell', { name: 'OpenAI 兼容 API' })
  ).toBeVisible()
  expect(
    within(table).getByRole('cell', { name: 'Anthropic 兼容 API' })
  ).toBeVisible()
  const endpoints = screen.getByRole('table', { name: '模型 API 接口' })
  expect(
    within(endpoints).getByRole('columnheader', { name: '用途' })
  ).toBeVisible()
  expect(
    within(endpoints).getByRole('cell', {
      name: '列出当前 API 密钥可用的模型。',
    })
  ).toBeVisible()
})

test('switching to Chinese translates new document headings, scenario tabs, code titles, and capability links', async () => {
  const user = userEvent.setup()
  renderPage('/docs/multimodal')
  expect(
    await screen.findByRole('heading', { name: 'Multimodal inputs', level: 1 })
  ).toBeVisible()
  i18next.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
  await act(() => i18next.changeLanguage('zhCN'))
  expect(
    screen.getByRole('heading', { name: '多模态输入', level: 1 })
  ).toBeVisible()
  expect(screen.getByRole('tab', { name: '图片输入' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  await user.click(screen.getByRole('tab', { name: '视频输入' }))
  expect(
    within(screen.getByRole('tabpanel', { name: '视频输入' })).getByText(
      '视频输入',
      {
        exact: true,
      }
    )
  ).toBeVisible()
  await user.click(
    within(
      screen.getByRole('navigation', { name: i18next.t('Docs') })
    ).getByRole('link', { name: i18next.t('Docs') })
  )
  const table = await screen.findByRole('table', {
    name: i18next.t('Model capabilities'),
  })
  expect(
    within(table).getByRole('link', { name: '多模态输入' })
  ).toHaveAttribute('href', '/docs/multimodal')
  const music = within(table).getByRole('link', { name: '音乐与歌词' })
  expect(music).toHaveAttribute('href', '/docs/music')
  await user.click(music)
  expect(
    await screen.findByRole('heading', { name: '音乐与歌词', level: 1 })
  ).toBeVisible()
  await act(() => i18next.changeLanguage('en'))
  expect(
    screen.getByRole('heading', { name: 'Music and lyrics', level: 1 })
  ).toBeVisible()
})
