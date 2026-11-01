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
import { api, type ApiRequestConfig } from '@/lib/api'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import type {
  DesktopRelease,
  DesktopReleaseCreate,
  DesktopReleasePatch,
} from './types'

type DesktopReleaseResponse = Omit<
  DesktopRelease,
  'device_mid_allowlist' | 'release_notes_by_locale' | 'files'
> & {
  device_mid_allowlist: DesktopRelease['device_mid_allowlist'] | null
  release_notes_by_locale: DesktopRelease['release_notes_by_locale'] | null
  files: DesktopRelease['files'] | null
}

type ReleaseResponse<T> = {
  success: boolean
  data: T
  message?: string
  message_key?: string
  message_params?: Record<string, unknown>
  code?: string
}

const mutationConfig: ApiRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
  singleUseAuthorization: true,
}

function normalizeRelease(release: DesktopReleaseResponse): DesktopRelease {
  return {
    ...release,
    device_mid_allowlist: release.device_mid_allowlist ?? [],
    release_notes_by_locale: release.release_notes_by_locale ?? {},
    files: release.files ?? [],
  }
}

export async function listDesktopReleases(
  signal?: AbortSignal
): Promise<DesktopRelease[]> {
  // Query 取消须中止旧 GET；绕过共享去重，防止保存后的刷新复用旧快照覆盖新状态。
  const response = await api.get<
    ReleaseResponse<DesktopReleaseResponse[] | null>
  >('/api/v1/admin/releases', { signal, disableDuplicate: true })
  return (requireServerSuccess(response.data).data ?? []).map(normalizeRelease)
}

export async function createDesktopRelease(
  input: DesktopReleaseCreate
): Promise<DesktopRelease> {
  const response = await api.post<
    ReleaseResponse<DesktopReleaseResponse | null>
  >('/api/v1/admin/releases', input, mutationConfig)
  const result = requireServerSuccess(response.data)
  if (!result.data) {
    throw createServerError(result, 'Invalid desktop release response')
  }
  return normalizeRelease(result.data)
}

export async function updateDesktopRelease(
  id: number,
  patch: DesktopReleasePatch
): Promise<DesktopRelease> {
  const response = await api.patch<
    ReleaseResponse<DesktopReleaseResponse | null>
  >(`/api/v1/admin/releases/${id}`, patch, mutationConfig)
  const result = requireServerSuccess(response.data)
  if (!result.data) {
    throw createServerError(result, 'Invalid desktop release response')
  }
  return normalizeRelease(result.data)
}
