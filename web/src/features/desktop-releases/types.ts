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
export type ReleaseNotesLocale = 'zh-CN' | 'en-US'

export type ReleaseNotes = Partial<Record<ReleaseNotesLocale, string>>

export interface DesktopReleaseFile {
  id?: number
  release_id?: number
  position?: number
  url: string
  sha512: string
  size: number
}

export interface DesktopRelease {
  id: number
  version: string
  platform: string
  status: 0 | 1 | 3
  rollout_percentage: number
  rollout_disabled: boolean
  device_mid_allowlist: string[]
  minimum_system_version: string
  release_date: number
  release_notes_by_locale: ReleaseNotes
  files: DesktopReleaseFile[]
  created_at: number
  updated_at: number
}

export type DesktopReleaseCreate = Omit<
  DesktopRelease,
  'id' | 'created_at' | 'updated_at'
>
export type DesktopReleasePatch = Partial<
  Pick<
    DesktopRelease,
    | 'status'
    | 'rollout_percentage'
    | 'rollout_disabled'
    | 'device_mid_allowlist'
    | 'release_notes_by_locale'
  >
>
export type ReleaseState = 'unpublished' | 'rollout' | 'paused' | 'stable'
export type ReleaseAction =
  | 'rollout'
  | 'notes'
  | 'publish'
  | 'pause'
  | 'resume'
  | 'withdraw'
