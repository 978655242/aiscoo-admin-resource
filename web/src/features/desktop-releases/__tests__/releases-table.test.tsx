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
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { useSidebarView } from '@/hooks/use-sidebar-view'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { ReleasesTable } from '../components/releases-table'
import type { DesktopRelease } from '../types'

vi.mock('@tanstack/react-router', () => ({
  useLocation: () => '/desktop-releases',
}))

const releases: DesktopRelease[] = [
  {
    id: 3,
    version: '3.17.3',
    platform: 'darwin-aarch64',
    status: 0,
    rollout_percentage: 0,
    rollout_disabled: false,
    device_mid_allowlist: [],
    minimum_system_version: '22.0.0',
    release_date: 1793500000,
    release_notes_by_locale: {},
    files: [],
    created_at: 1793500000,
    updated_at: 1793500000,
  },
  {
    id: 2,
    version: '3.17.2',
    platform: 'darwin-aarch64',
    status: 3,
    rollout_percentage: 10,
    rollout_disabled: true,
    device_mid_allowlist: ['device-a'],
    minimum_system_version: '22.0.0',
    release_date: 1793400000,
    release_notes_by_locale: {},
    files: [],
    created_at: 1793400000,
    updated_at: 1793400000,
  },
  {
    id: 1,
    version: '3.17.1',
    platform: 'darwin-aarch64',
    status: 1,
    rollout_percentage: 0,
    rollout_disabled: false,
    device_mid_allowlist: [],
    minimum_system_version: '22.0.0',
    release_date: 1793300000,
    release_notes_by_locale: {},
    files: [],
    created_at: 1793300000,
    updated_at: 1793300000,
  },
]

afterEach(() => {
  useAuthStore.getState().auth.reset()
  vi.unstubAllGlobals()
})

function renderTable() {
  const onDetails = vi.fn()
  render(
    <ReleasesTable
      releases={releases}
      currentStableId={1}
      isLoading={false}
      isFetching={false}
      disabled={false}
      onCreate={vi.fn()}
      onDetails={onDetails}
      onAction={vi.fn()}
    />
  )
  return { onDetails }
}

test('searching a version keeps matching rows and clearing the filter restores the list', async () => {
  const user = userEvent.setup()
  const { onDetails } = renderTable()
  await user.type(
    screen.getByPlaceholderText('Search version number...'),
    '3.17.2'
  )
  expect(
    screen.queryByRole('button', { name: '3.17.1' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '3.17.2' }))
  expect(onDetails).toHaveBeenCalledWith(releases[1])
  await user.clear(screen.getByPlaceholderText('Search version number...'))
  expect(screen.getByRole('button', { name: '3.17.1' })).toBeVisible()
})

test('an unmatched search shows a clear-filters action that restores versions', async () => {
  const user = userEvent.setup()
  renderTable()
  await user.type(
    screen.getByPlaceholderText('Search version number...'),
    '9.0.0'
  )
  expect(screen.getByText('No matching versions')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Clear filters' }))
  expect(screen.getByRole('button', { name: '3.17.1' })).toBeVisible()
})

test('paused releases show retained configuration and stable releases do not offer pause', async () => {
  const user = userEvent.setup()
  renderTable()
  expect(screen.getByText('Paused, including the allowlist')).toBeVisible()
  expect(screen.getByText('Plus 1 allowlisted devices')).toBeVisible()
  await user.click(
    screen.getByRole('button', { name: 'Actions for version 3.17.1' })
  )
  const menu = screen.getByRole('menu')
  expect(
    within(menu).queryByRole('menuitem', { name: 'Pause rollout' })
  ).not.toBeInTheDocument()
  expect(
    within(menu).getByRole('menuitem', { name: 'Stop distribution' })
  ).toBeVisible()
})

test('a narrow viewport uses the shared mobile list and retains version actions', () => {
  const native = window.matchMedia
  vi.stubGlobal('matchMedia', (query: string) => ({
    ...native(query),
    matches:
      query.includes('max-width: 640px') ||
      query.includes('prefers-reduced-motion'),
  }))
  renderTable()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '3.17.1' })).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Actions for version 3.17.2' })
  ).toBeVisible()
})

test('the root-only version entry follows task plugins and is hidden from other roles', () => {
  const client = new QueryClient({
    defaultOptions: { queries: { enabled: false, retry: false } },
  })
  client.setQueryData(['status'], {})
  function Navigation() {
    const groups = useSidebarView().navGroups
    return (
      <nav>
        {groups
          .flatMap((group) => group.items)
          .map((item) => (
            <span key={item.title}>{item.title}</span>
          ))}
      </nav>
    )
  }
  act(() =>
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
  )
  render(
    <QueryClientProvider client={client}>
      <Navigation />
    </QueryClientProvider>
  )
  expect(screen.getByText('Versions')).toBeVisible()
  expect(screen.getByRole('navigation').textContent).toMatch(
    /Task PluginsVersionsSystem Settings/
  )
  act(() =>
    useAuthStore
      .getState()
      .auth.setUser({ id: 2, username: 'admin', role: ROLE.ADMIN })
  )
  expect(screen.queryByText('Versions')).not.toBeInTheDocument()
  client.clear()
})
