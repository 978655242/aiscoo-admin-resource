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
import { QueryClient, type QueryFunctionContext } from '@tanstack/react-query'
import type { AxiosAdapter } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { AuthOperationError } from '@/lib/secure-verification'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  createDesktopRelease,
  listDesktopReleases,
  updateDesktopRelease,
} from '../api'
import type {
  DesktopRelease,
  DesktopReleaseCreate,
  DesktopReleasePatch,
} from '../types'

const input: DesktopReleaseCreate = {
  version: '3.10.0',
  platform: 'darwin-aarch64',
  status: 3,
  rollout_percentage: 10,
  rollout_disabled: false,
  device_mid_allowlist: ['test-device'],
  minimum_system_version: '13.0',
  release_date: 1793491200,
  release_notes_by_locale: { 'en-US': 'Release notes' },
  files: [
    { url: 'https://downloads.example.com/app.dmg', sha512: 'digest', size: 1 },
  ],
}
const saved: DesktopRelease = {
  ...input,
  id: 42,
  created_at: 1793491200,
  updated_at: 1793491200,
}
const patch: DesktopReleasePatch = {
  status: 0,
  rollout_disabled: false,
  rollout_percentage: 0,
  device_mid_allowlist: [],
  release_notes_by_locale: {},
}
const operations = [
  { name: 'list', method: 'get', run: () => listDesktopReleases() },
  { name: 'create', method: 'post', run: () => createDesktopRelease(input) },
  {
    name: 'update',
    method: 'patch',
    run: () => updateDesktopRelease(42, patch),
  },
] as const

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})

describe('desktop release API', () => {
  it('lists releases through the shared admin API and normalizes nullable fields', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: [
          {
            ...saved,
            device_mid_allowlist: null,
            release_notes_by_locale: null,
            files: null,
          },
        ],
      },
    })

    await expect(listDesktopReleases()).resolves.toEqual([
      {
        ...saved,
        device_mid_allowlist: [],
        release_notes_by_locale: {},
        files: [],
      },
    ])
    expect(get).toHaveBeenCalledWith('/api/v1/admin/releases', {
      signal: undefined,
      disableDuplicate: true,
    })
  })

  it('starts a fresh GET after cancelling a refresh so its old snapshot cannot overwrite a saved release', async () => {
    const updated: DesktopRelease = { ...saved, status: 1 }
    let releaseOldGet: () => void = () => undefined
    let markOldGetStarted: () => void = () => undefined
    const oldGetResponse = new Promise<void>((resolve) => {
      releaseOldGet = resolve
    })
    const oldGetStarted = new Promise<void>((resolve) => {
      markOldGetStarted = resolve
    })
    let requests = 0
    const adapter = vi.fn<AxiosAdapter>(async (config) => {
      const oldSnapshot = ++requests === 1
      if (oldSnapshot) {
        markOldGetStarted()
        await oldGetResponse
      }
      return {
        config,
        data: { success: true, data: [oldSnapshot ? saved : updated] },
        status: 200,
        statusText: 'OK',
        headers: {},
      }
    })
    api.defaults.adapter = adapter
    vi.spyOn(api, 'patch').mockResolvedValue({
      data: { success: true, data: updated },
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const queryKey = ['desktop-releases'] as const
    const queryFn = ({ signal }: QueryFunctionContext) =>
      listDesktopReleases(signal)
    client.setQueryData(queryKey, [saved])
    try {
      const oldFetch = client
        .fetchQuery({ queryKey, queryFn })
        .catch(() => undefined)
      await oldGetStarted
      const result = await updateDesktopRelease(saved.id, { status: 1 })
      await client.cancelQueries({ queryKey })
      client.setQueryData(queryKey, [result])
      const refreshed = client.fetchQuery({ queryKey, queryFn })
      // 旧 GET 故意晚于保存返回，真实共享去重逻辑不能把它交给新的刷新。
      releaseOldGet()
      await expect(refreshed).resolves.toEqual([updated])
      expect(client.getQueryData(queryKey)).toEqual([updated])
      expect(adapter).toHaveBeenCalledTimes(2)
      expect(adapter.mock.calls[0][0].signal?.aborted).toBe(true)
      expect(adapter.mock.calls[1][0].signal).toBeDefined()
      await oldFetch
    } finally {
      releaseOldGet()
      client.clear()
    }
  })

  it('treats a successful null release list as an empty list', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: null },
    })
    await expect(listDesktopReleases()).resolves.toEqual([])
  })

  it('creates a release with the backend JSON contract and disables mutation replay', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: saved },
    })

    await expect(createDesktopRelease(input)).resolves.toEqual(saved)
    expect(post).toHaveBeenCalledWith(
      '/api/v1/admin/releases',
      input,
      expect.objectContaining({ singleUseAuthorization: true })
    )
  })

  it('patches only the supplied fields including explicit zero, false and empty values', async () => {
    const updated = { ...saved, ...patch }
    const request = vi.spyOn(api, 'patch').mockResolvedValue({
      data: { success: true, data: updated },
    })

    await expect(updateDesktopRelease(42, patch)).resolves.toEqual(updated)
    expect(request).toHaveBeenCalledWith(
      '/api/v1/admin/releases/42',
      patch,
      expect.objectContaining({ singleUseAuthorization: true })
    )
  })

  it.each(operations)(
    'rejects a failed $name response even when data is present',
    async (operation) => {
      const failure = {
        success: false,
        message: 'desktop release already exists',
        data: operation.method === 'get' ? [saved] : saved,
      }
      vi.spyOn(api, operation.method).mockResolvedValue({ data: failure })

      await expect(operation.run()).rejects.toMatchObject({
        message: 'desktop release already exists',
        cause: failure,
      })
    }
  )

  it.each(operations)(
    'preserves a rejected $name authentication error and its safe message',
    async (operation) => {
      const safe = new AuthOperationError(
        'Please try again later.',
        'AUTH_INTERNAL_ERROR',
        {
          cause: { message: 'private authentication details' },
        }
      )
      vi.spyOn(api, operation.method).mockRejectedValue(safe)

      await expect(operation.run()).rejects.toBe(safe)
      expect(getServerErrorMessage(safe)).toBe('Please try again later.')
    }
  )

  it.each(operations.filter((operation) => operation.method !== 'get'))(
    'normalizes nullable fields on a successful $name response',
    async (operation) => {
      vi.spyOn(api, operation.method).mockResolvedValue({
        data: {
          success: true,
          data: {
            ...saved,
            device_mid_allowlist: null,
            release_notes_by_locale: null,
            files: null,
          },
        },
      })

      await expect(operation.run()).resolves.toEqual({
        ...saved,
        device_mid_allowlist: [],
        release_notes_by_locale: {},
        files: [],
      })
    }
  )

  it.each(operations.filter((operation) => operation.method !== 'get'))(
    'rejects a $name response without saved release data',
    async (operation) => {
      vi.spyOn(api, operation.method).mockResolvedValue({
        data: { success: true, data: null },
      })
      await expect(operation.run()).rejects.toThrow(
        'Invalid desktop release response'
      )
    }
  )
})
