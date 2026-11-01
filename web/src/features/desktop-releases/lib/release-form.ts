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
import { parseDocument } from 'yaml'
import { z } from 'zod'

import type { DesktopReleaseCreate, ReleaseNotes } from '../types'
import { isValidDesktopReleaseVersion } from './release-state'

const MAX_MANIFEST_BYTES = 256 * 1024
const MAX_NOTES_BYTES = 64 * 1024
const FILE_COUNT_ERROR = 'A release must contain between 1 and 8 files'
const MANIFEST_ERROR = 'Invalid release manifest metadata'
const MANIFEST_SIZE_ERROR = 'Manifest file must not exceed 256 KiB'
const ALIAS_ERROR = 'Manifest YAML aliases exceed the safety limit'
const NOTES_SIZE_ERROR = 'Release notes JSON must not exceed 64 KiB'

export function getReleaseNotesByteSize(notes: ReleaseNotes): number {
  // Go 的 JSON 编码会转义 HTML 字符及行分隔符；按服务端存储的字节数校验。
  const encoded = JSON.stringify(notes).replaceAll(
    /[<>&\u2028\u2029]/g,
    (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`
  )
  return new TextEncoder().encode(encoded).length
}

export const releaseNotesSchema = z
  .strictObject(
    {
      'zh-CN': z.string({ error: 'Release notes must be text' }).optional(),
      'en-US': z.string({ error: 'Release notes must be text' }).optional(),
    },
    { error: 'Release notes only support Simplified Chinese and English' }
  )
  .refine(
    (notes) => getReleaseNotesByteSize(notes) <= MAX_NOTES_BYTES,
    NOTES_SIZE_ERROR
  )

function isReleaseFileUrl(value: string): boolean {
  if (!/^https:\/\/[^/?#]+/.test(value) || /[\s\\#]/.test(value)) return false
  const authority = value.slice('https://'.length).split(/[/?]/, 1)[0]
  if (authority.includes('@')) return false
  try {
    const url = new URL(value)
    return (
      url.protocol === 'https:' &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.hash
    )
  } catch {
    return false
  }
}

function isSha512Digest(value: string): boolean {
  const encoded = value.replaceAll(/[\r\n]/g, '')
  if (!/^[A-Za-z0-9+/]{86}==$/.test(encoded)) return false
  try {
    return atob(encoded).length === 64
  } catch {
    return false
  }
}

const releaseFileFormSchema = z.object({
  url: z
    .string()
    .trim()
    .refine(
      isReleaseFileUrl,
      'Enter an HTTPS URL without credentials or a fragment'
    ),
  sha512: z
    .string()
    .refine(isSha512Digest, 'SHA-512 must be a Base64-encoded 64-byte digest'),
  size: z
    .string()
    .refine(
      (value) =>
        /^\d+$/.test(value) &&
        Number.isSafeInteger(Number(value)) &&
        Number(value) > 0,
      'File size must be a positive safe integer in bytes'
    ),
})

export const releaseCreateFormSchema = z.object({
  version: z
    .string()
    .trim()
    .refine(
      isValidDesktopReleaseVersion,
      'Enter a version as X.Y.Z or X.Y.Z-prerelease without build metadata'
    ),
  files: z
    .array(releaseFileFormSchema)
    .min(1, FILE_COUNT_ERROR)
    .max(8, FILE_COUNT_ERROR),
  minimum_system_version: z
    .string()
    .trim()
    .refine(
      (value) => value === '' || /^\d+(?:\.\d+){0,2}$/.test(value),
      'Minimum system version must contain one to three numeric parts'
    ),
  release_date: z
    .number({ error: 'Release date must be a valid date' })
    .refine(
      (value) =>
        Number.isSafeInteger(value) && value >= 0 && value <= 8_640_000_000_000,
      'Release date must be a valid date'
    ),
  release_notes_by_locale: releaseNotesSchema,
})

export type ReleaseCreateFormValues = z.infer<typeof releaseCreateFormSchema>

export function getReleaseCreateDefaults(): ReleaseCreateFormValues {
  return {
    version: '',
    files: [{ url: '', sha512: '', size: '' }],
    minimum_system_version: '22.0.0',
    release_date: 0,
    release_notes_by_locale: {},
  }
}

export function toDesktopReleaseCreate(
  values: ReleaseCreateFormValues
): DesktopReleaseCreate {
  const parsed = releaseCreateFormSchema.parse(values)
  return {
    ...parsed,
    platform: 'darwin-aarch64',
    status: 0,
    rollout_percentage: 0,
    rollout_disabled: false,
    device_mid_allowlist: [],
    files: parsed.files.map((file) => ({ ...file, size: Number(file.size) })),
  }
}

const manifestInteger = z.union([z.string(), z.bigint(), z.number().finite()])
const manifestFileSchema = z.object({
  url: z.string().optional(),
  sha512: z.string().optional(),
  size: manifestInteger.optional(),
})
const manifestSchema = z.object({
  version: z.string(),
  files: z
    .array(manifestFileSchema)
    .min(1, FILE_COUNT_ERROR)
    .max(8, FILE_COUNT_ERROR)
    .optional(),
  path: z.string().optional(),
  sha512: z.string().optional(),
  size: manifestInteger.optional(),
  releaseDate: z.string().optional(),
  minimumSystemVersion: manifestInteger.optional(),
  releaseNotesByLocale: releaseNotesSchema.optional(),
})

function parseManifestReleaseDate(value: string | undefined): number {
  if (value === undefined || value === '') return 0
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.exec(
      value
    )
  if (!match) {
    throw new Error('Manifest releaseDate must be a valid RFC3339 date')
  }
  const [, year, month, day, hour, minute, second] = match
  const daysInMonth = new Date(
    Date.UTC(Number(year), Number(month), 0)
  ).getUTCDate()
  const timestamp = Date.parse(value)
  if (
    Number(month) < 1 ||
    Number(month) > 12 ||
    Number(day) < 1 ||
    Number(day) > daysInMonth ||
    Number(hour) > 23 ||
    Number(minute) > 59 ||
    Number(second) > 59 ||
    !Number.isFinite(timestamp) ||
    timestamp < 0
  ) {
    throw new Error('Manifest releaseDate must be a valid RFC3339 date')
  }
  return Math.floor(timestamp / 1000)
}

export function parseReleaseManifest(text: string): ReleaseCreateFormValues {
  if (new TextEncoder().encode(text).length > MAX_MANIFEST_BYTES) {
    throw new Error(MANIFEST_SIZE_ERROR)
  }
  const document = parseDocument(text, {
    schema: 'core',
    uniqueKeys: true,
    stringKeys: true,
    intAsBigInt: true,
  })
  if (document.errors.length || document.warnings.length) {
    throw new Error(MANIFEST_ERROR)
  }
  let metadata: unknown
  try {
    // 限制别名展开，避免小 YAML 在转换对象时造成无界内存增长。
    metadata = document.toJS({ maxAliasCount: 50 })
  } catch (error) {
    if (error instanceof ReferenceError && /alias count/i.test(error.message)) {
      throw new Error(ALIAS_ERROR)
    }
    throw new Error(MANIFEST_ERROR)
  }
  const result = manifestSchema.safeParse(metadata)
  if (!result.success) {
    const issue = result.error.issues[0]
    if (
      issue?.path[0] === 'files' &&
      (issue.code === 'too_small' || issue.code === 'too_big')
    ) {
      throw new Error(FILE_COUNT_ERROR)
    }
    if (issue?.path[0] === 'releaseNotesByLocale') {
      throw new Error(issue.message)
    }
    throw new Error(MANIFEST_ERROR)
  }
  const manifest = result.data
  const files =
    manifest.files ??
    (manifest.path
      ? [{ url: manifest.path, sha512: manifest.sha512, size: manifest.size }]
      : [])
  if (files.length === 0) throw new Error(FILE_COUNT_ERROR)
  return {
    ...getReleaseCreateDefaults(),
    version: manifest.version,
    files: files.map((file) => ({
      url: file.url ?? '',
      sha512: file.sha512 ?? '',
      size: file.size === undefined ? '' : String(file.size),
    })),
    minimum_system_version:
      manifest.minimumSystemVersion === undefined
        ? '22.0.0'
        : String(manifest.minimumSystemVersion),
    release_date: parseManifestReleaseDate(manifest.releaseDate),
    release_notes_by_locale: manifest.releaseNotesByLocale ?? {},
  }
}

export async function readReleaseManifestFile(
  file: Pick<File, 'size' | 'text'>
): Promise<ReleaseCreateFormValues> {
  if (file.size > MAX_MANIFEST_BYTES) throw new Error(MANIFEST_SIZE_ERROR)
  let text: string
  try {
    text = await file.text()
  } catch {
    throw new Error('Could not read the manifest file')
  }
  return parseReleaseManifest(text)
}
