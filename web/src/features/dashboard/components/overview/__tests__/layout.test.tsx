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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { OverviewDashboard } from '../overview-dashboard'

let client: QueryClient
let status: Record<string, unknown>
const getAnimationsDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)

beforeEach(() => {
  window.localStorage.clear()
  // jsdom has no Web Animations API; ScrollArea uses it after mounting.
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'dashboard-user',
    role: 1,
    quota: 1000000,
    used_quota: 1000,
    request_count: 12,
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  status = {
    api_info_enabled: true,
    announcements_enabled: true,
    faq_enabled: true,
    uptime_kuma_enabled: true,
    api_info: [
      {
        route: 'Primary API',
        url: 'https://api.example.com/v1',
        description: 'Main gateway',
      },
    ],
    announcements: [{ id: 1, content: 'Planned maintenance notice' }],
    faq: [
      {
        id: 1,
        question: 'How do I connect my application?',
        answer: 'Use the primary API URL with your API key.',
      },
    ],
  }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/token/?p=1&size=10':
        return {
          data: {
            success: true,
            data: {
              items: [{ id: 1, name: 'App key', key: 'masked', status: 1 }],
            },
          },
        }
      case '/api/status':
        return { data: { data: status } }
      case '/api/user/models':
        return { data: { success: true, data: ['gpt-4o-mini'] } }
      case '/api/data/self':
        return { data: { success: true, data: [] } }
      case '/api/uptime/status':
        return { data: { success: true, data: [] } }
      case '/api/perf-metrics/summary':
        return { data: { success: true, data: { models: [], summary: {} } } }
      default:
        throw new Error(`Unexpected dashboard request: ${url}`)
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  if (getAnimationsDescriptor) {
    Object.defineProperty(
      Element.prototype,
      'getAnimations',
      getAnimationsDescriptor
    )
  } else {
    Reflect.deleteProperty(Element.prototype, 'getAnimations')
  }
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

async function renderOverview() {
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('overview layout', () => {
  it('groups API and monitoring before updates and help in mobile reading order', async () => {
    await renderOverview()

    const apiPanel = await screen.findByRole('region', { name: 'API Info' })
    const uptime = screen.getByRole('region', { name: 'Uptime' })
    const announcements = screen.getByRole('region', { name: 'Announcements' })
    const faq = screen.getByRole('region', { name: 'FAQ' })
    const services = apiPanel.parentElement?.parentElement
    const updates = announcements.parentElement?.parentElement

    expect(services).toContainElement(uptime)
    expect(updates).toContainElement(faq)
    expect(services).not.toContainElement(announcements)
    expect(
      apiPanel.compareDocumentPosition(uptime) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      uptime.compareDocumentPosition(announcements) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(services?.parentElement).toHaveClass('grid-cols-1', 'items-start')
    expect(services?.parentElement).toHaveClass(
      'xl:grid-cols-[minmax(0,1fr)_20rem]'
    )
  })

  it('aligns the wallet with the desktop side column while keeping its navigation', async () => {
    await renderOverview()

    const wallet = await screen.findByRole('region', { name: 'Wallet' })
    expect(wallet.parentElement).toHaveClass(
      'grid-cols-1',
      'xl:grid-cols-[minmax(0,1fr)_20rem]'
    )
    expect(
      within(wallet).getByRole('button', { name: 'Wallet' })
    ).toHaveAttribute('href', '/wallet')
    expect(within(wallet).getByText('Credit remaining')).toBeVisible()
  })

  it('keeps unconfigured panels compact instead of reserving a full scroll area', async () => {
    status.api_info = []
    status.announcements = []
    status.faq = []
    await renderOverview()

    for (const message of [
      'No API routes configured',
      'No uptime monitoring configured',
      'No announcements at this time',
      'No FAQ entries available',
    ]) {
      const empty = await screen.findByText(message)
      expect(empty).toHaveClass('h-32')
    }
  })

  it('lets long FAQ answers scroll within their panel and remain keyboard accessible', async () => {
    status.faq = [
      {
        id: 1,
        question: 'How do I connect my application?',
        answer: 'Connect using the documented URL. '.repeat(40),
      },
      {
        id: 2,
        question: 'Where can I manage my API keys?',
        answer: 'Open API Keys in the console.',
      },
    ]
    const user = userEvent.setup()
    await renderOverview()

    const faq = await screen.findByRole('region', { name: 'FAQ' })
    const scrollArea = faq.querySelector('[data-slot="scroll-area"]')
    expect(scrollArea).toHaveClass('max-h-80')
    expect(scrollArea).not.toHaveClass('h-80')
    const question = within(faq).getByRole('button', {
      name: 'How do I connect my application?',
    })
    question.focus()
    await user.keyboard('{Enter}')
    expect(question).toHaveAttribute('aria-expanded', 'true')
    expect(
      within(faq).getByText(/Connect using the documented URL/)
    ).toBeVisible()
    await user.keyboard('{Enter}')
    expect(question).toHaveAttribute('aria-expanded', 'false')
  })

  it('uses the full content width when only the updates column is enabled', async () => {
    status.api_info_enabled = false
    status.uptime_kuma_enabled = false
    await renderOverview()

    const announcements = await screen.findByRole('region', {
      name: 'Announcements',
    })
    expect(
      screen.queryByRole('region', { name: 'API Info' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('region', { name: 'Uptime' })
    ).not.toBeInTheDocument()
    const grid = announcements.parentElement?.parentElement?.parentElement
    expect(grid).toHaveClass('grid-cols-1')
    expect(grid).not.toHaveClass('xl:grid-cols-[minmax(0,1fr)_20rem]')
  })

  it('keeps admin performance at full width when optional content panels are disabled', async () => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'dashboard-admin',
      role: 10,
      quota: 1000000,
      request_count: 12,
    })
    status.api_info_enabled = false
    status.uptime_kuma_enabled = false
    status.announcements_enabled = false
    status.faq_enabled = false
    await renderOverview()

    const performance = await screen.findByRole('heading', {
      name: 'Performance health',
    })
    const panel = performance.closest('section')
    expect(panel?.parentElement).not.toHaveClass('lg:col-span-2')
    expect(panel?.parentElement?.parentElement?.parentElement).toHaveClass(
      'grid-cols-1'
    )
    expect(panel?.parentElement?.parentElement?.parentElement).not.toHaveClass(
      'xl:grid-cols-[minmax(0,1fr)_20rem]'
    )
  })
})
