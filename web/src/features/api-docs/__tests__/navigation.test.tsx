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

test('switching the interface language updates the document heading', async () => {
  renderPage()
  expect(
    await screen.findByRole('heading', { name: 'API overview', level: 1 })
  ).toBeVisible()
  i18next.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
  await act(() => i18next.changeLanguage('zhCN'))
  expect(
    screen.getByRole('heading', { name: 'API 概览', level: 1 })
  ).toBeVisible()
})
