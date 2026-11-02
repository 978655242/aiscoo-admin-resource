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
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { AuthenticatedLayout } from '../authenticated-layout'

const clients: QueryClient[] = []
const protectedQueryKey = ['protected-page'] as const

function ProtectedPage() {
  const query = useQuery({
    queryKey: protectedQueryKey,
    queryFn: async () => {
      const response = await api.get('/api/protected-page')
      return response.data.data as string
    },
  })
  return <section aria-label='Protected page'>{query.data}</section>
}

async function renderLayout() {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  })
  clients.push(client)
  client.setQueryData(['status'], { system_name: 'aiscoo' })
  client.setQueryData(['notice'], { success: true, data: '' })
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => (
        <AuthenticatedLayout>
          <ProtectedPage />
        </AuthenticatedLayout>
      ),
    }),
    history: createMemoryHistory({ initialEntries: ['/dashboard/overview'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return client
}

beforeEach(() => {
  localStorage.clear()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useSystemConfigStore.setState({ loading: false })
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  localStorage.clear()
})

test('signing out immediately removes protected content and prevents its queries from refetching', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'alice', role: ROLE.USER })
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: 'Private account data' },
  })
  const client = await renderLayout()
  expect(await screen.findByText('Private account data')).toBeVisible()
  expect(screen.getByRole('link', { name: 'aiscoo' })).toBeVisible()

  act(() => useAuthStore.getState().auth.reset())

  expect(screen.queryByRole('region', { name: 'Protected page' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'aiscoo' })).toBeNull()
  await act(() => client.invalidateQueries({ queryKey: protectedQueryKey }))
  expect(
    get.mock.calls.filter(([url]) => url === '/api/protected-page')
  ).toHaveLength(1)
})

test('a signed-out session renders neither the console nor its protected queries', async () => {
  const get = vi.spyOn(api, 'get')
  await renderLayout()

  expect(screen.queryByRole('region', { name: 'Protected page' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'aiscoo' })).toBeNull()
  expect(get).not.toHaveBeenCalled()
})
