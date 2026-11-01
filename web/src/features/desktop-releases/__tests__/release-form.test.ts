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
import { describe, expect, test, vi } from 'vitest'

import {
  getReleaseCreateDefaults,
  getReleaseNotesByteSize,
  parseReleaseManifest,
  readReleaseManifestFile,
  releaseCreateFormSchema,
  releaseNotesSchema,
  toDesktopReleaseCreate,
  type ReleaseCreateFormValues,
} from '../lib/release-form'

const digest = btoa('\0'.repeat(64))

function validForm(): ReleaseCreateFormValues {
  return {
    ...getReleaseCreateDefaults(),
    version: '1.2.3-rc.01',
    files: [
      {
        url: 'https://downloads.example.com/app.zip',
        sha512: digest,
        size: '2048',
      },
    ],
  }
}

const manifest = `version: 1.2.3
files:
  - url: https://downloads.example.com/app.zip
    sha512: ${digest}
    size: 2048
`

describe('desktop release creation validation', () => {
  test('builds an unpublished Apple Silicon payload and preserves file order', () => {
    const values = validForm()
    values.files.push({
      url: 'https://downloads.example.com/app.dmg',
      sha512: digest,
      size: '4096',
    })

    expect(toDesktopReleaseCreate(values)).toEqual({
      version: '1.2.3-rc.01',
      platform: 'darwin-aarch64',
      status: 0,
      rollout_percentage: 0,
      rollout_disabled: false,
      device_mid_allowlist: [],
      minimum_system_version: '22.0.0',
      release_date: 0,
      release_notes_by_locale: {},
      files: [
        {
          url: 'https://downloads.example.com/app.zip',
          sha512: digest,
          size: 2048,
        },
        {
          url: 'https://downloads.example.com/app.dmg',
          sha512: digest,
          size: 4096,
        },
      ],
    })
  })

  test.each([
    '1.2.3+build.1',
    'v1.2.3',
    '01.2.3',
    '1.2',
    '1.2.3-',
    '9223372036854775808.0.0',
  ])('rejects backend-incompatible version %s', (version) => {
    const result = releaseCreateFormSchema.safeParse({
      ...validForm(),
      version,
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual(['version'])
    }
  })

  test.each([
    'app.zip',
    'http://downloads.example.com/app.zip',
    'https://operator:secret@downloads.example.com/app.zip',
    'https://@downloads.example.com/app.zip',
    'https://downloads.example.com/app.zip#digest',
    'https://downloads.example.com/app.zip#',
    'https:/downloads.example.com/app.zip',
  ])('rejects unsafe or incomplete download URL %s', (url) => {
    const values = validForm()
    values.files[0].url = url
    const result = releaseCreateFormSchema.safeParse(values)
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual(['files', 0, 'url'])
    }
  })

  test.each([
    btoa('\0'.repeat(63)),
    btoa('\0'.repeat(65)),
    `${'!'.repeat(86)}==`,
    digest.slice(0, -2),
  ])(
    'rejects a SHA-512 value that does not decode to exactly 64 bytes',
    (sha512) => {
      const values = validForm()
      values.files[0].sha512 = sha512
      expect(releaseCreateFormSchema.safeParse(values).success).toBe(false)
    }
  )

  test.each(['', '0', '-1', '1.5', '9007199254740992', '1e6', 'NaN'])(
    'rejects non-positive or unsafe file size %s',
    (size) => {
      const values = validForm()
      values.files[0].size = size
      expect(releaseCreateFormSchema.safeParse(values).success).toBe(false)
    }
  )

  test('accepts a positive safe integer without rounding it', () => {
    const values = validForm()
    values.files[0].size = '9007199254740991'
    expect(toDesktopReleaseCreate(values).files[0].size).toBe(
      Number.MAX_SAFE_INTEGER
    )
  })

  test.each([0, 9])('rejects a release with %i files', (count) => {
    const values = validForm()
    values.files = Array.from({ length: count }, () => ({ ...values.files[0] }))
    expect(releaseCreateFormSchema.safeParse(values).success).toBe(false)
  })

  test.each(['', '22', '22.0', '22.0.0'])(
    'accepts minimum system version %s',
    (minimum_system_version) => {
      expect(
        releaseCreateFormSchema.safeParse({
          ...validForm(),
          minimum_system_version,
        }).success
      ).toBe(true)
    }
  )

  test.each(['macOS 13', '22.0.0.1', '22..0', '-22', '22.0-beta'])(
    'rejects invalid dotted system version %s',
    (minimum_system_version) => {
      expect(
        releaseCreateFormSchema.safeParse({
          ...validForm(),
          minimum_system_version,
        }).success
      ).toBe(false)
    }
  )

  test.each([-1, 1.5, Number.NaN, Number.MAX_SAFE_INTEGER + 1])(
    'rejects a release timestamp that cannot be represented safely',
    (release_date) => {
      expect(
        releaseCreateFormSchema.safeParse({ ...validForm(), release_date })
          .success
      ).toBe(false)
    }
  )

  test('enforces the 64 KiB serialized release notes limit including UTF-8 and JSON overhead', () => {
    expect(
      releaseNotesSchema.safeParse({ 'en-US': 'a'.repeat(65_524) }).success
    ).toBe(true)
    expect(
      releaseNotesSchema.safeParse({ 'en-US': 'a'.repeat(65_525) }).success
    ).toBe(false)
    expect(
      releaseNotesSchema.safeParse({ 'zh-CN': '界'.repeat(21_841) }).success
    ).toBe(true)
    expect(
      releaseNotesSchema.safeParse({ 'zh-CN': '界'.repeat(21_842) }).success
    ).toBe(false)
  })

  test('counts HTML and line separator escaping exactly as the backend JSON encoder', () => {
    expect(getReleaseNotesByteSize({ 'en-US': '<>&\u2028\u2029' })).toBe(42)
    expect(
      releaseNotesSchema.safeParse({ 'en-US': '<'.repeat(10_921) }).success
    ).toBe(false)
  })

  test('rejects unsupported release notes languages', () => {
    expect(releaseNotesSchema.safeParse({ fr: 'Bonjour' }).success).toBe(false)
  })
})

describe('local desktop release manifest import', () => {
  test('imports ordered files, localized notes, the minimum system version, and an RFC3339 date', () => {
    const values =
      parseReleaseManifest(`${manifest}  - url: https://downloads.example.com/app.dmg
    sha512: ${digest}
    size: 4096
minimumSystemVersion: 23.0.0
releaseDate: '2026-11-01T10:20:30+08:00'
releaseNotesByLocale:
  zh-CN: 修复更新
  en-US: Update fixes
`)

    expect(values.version).toBe('1.2.3')
    expect(values.files.map((file) => [file.url, file.size])).toEqual([
      ['https://downloads.example.com/app.zip', '2048'],
      ['https://downloads.example.com/app.dmg', '4096'],
    ])
    expect(values.minimum_system_version).toBe('23.0.0')
    expect(values.release_date).toBe(Date.parse('2026-11-01T02:20:30Z') / 1000)
    expect(values.release_notes_by_locale).toEqual({
      'zh-CN': '修复更新',
      'en-US': 'Update fixes',
    })
  })

  test('keeps relative URLs for manual completion and reports the URL field as invalid', () => {
    const values = parseReleaseManifest(
      manifest.replace('https://downloads.example.com/', '')
    )
    expect(values.files[0].url).toBe('app.zip')
    const result = releaseCreateFormSchema.safeParse(values)
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual(['files', 0, 'url'])
    }
  })

  test('supports a legacy path and checksum without inventing a file size', () => {
    const values = parseReleaseManifest(`version: 1.2.3
path: legacy.zip
sha512: ${digest}
`)
    expect(values.files).toEqual([
      { url: 'legacy.zip', sha512: digest, size: '' },
    ])
    expect(values.release_date).toBe(0)
    expect(values.minimum_system_version).toBe('22.0.0')
    const result = releaseCreateFormSchema.safeParse({
      ...values,
      files: [
        { ...values.files[0], url: 'https://downloads.example.com/legacy.zip' },
      ],
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues[0]?.path).toEqual(['files', 0, 'size'])
    }
  })

  test('prefers files over legacy metadata and keeps a numeric Darwin major version', () => {
    const values = parseReleaseManifest(`${manifest}path: ignored.zip
sha512: ignored
minimumSystemVersion: 22
`)
    expect(values.files[0].url).toBe('https://downloads.example.com/app.zip')
    expect(values.minimum_system_version).toBe('22')
  })

  test.each([
    'files: [',
    'just plain text',
    'version: 42\nfiles: []',
    'version: 1.2.3\nfiles: []',
    `${manifest}releaseNotesByLocale:\n  fr: Bonjour`,
    `${manifest}releaseDate: not-a-date`,
    `${manifest}releaseDate: '2026-02-31T00:00:00Z'`,
    `${manifest}version: 2.0.0`,
  ])(
    'rejects malformed or incompatible metadata without returning partial form values',
    (text) => {
      expect(() => parseReleaseManifest(text)).toThrow()
    }
  )

  test('rejects more than eight manifest files with a clear translation key', () => {
    const text = `version: 1.2.3\nfiles:\n${'  - url: app.zip\n'.repeat(9)}`
    expect(() => parseReleaseManifest(text)).toThrow(
      'A release must contain between 1 and 8 files'
    )
  })

  test('blocks excessive YAML alias expansion with a clear translation key', () => {
    const text = `${manifest}a: &a [x, x, x, x, x, x, x, x, x, x]
b: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a, *a]
c: [*b, *b, *b, *b, *b, *b, *b, *b, *b, *b]
`
    expect(() => parseReleaseManifest(text)).toThrow(
      'Manifest YAML aliases exceed the safety limit'
    )
  })

  test('enforces the manifest limit in UTF-8 bytes before parsing', () => {
    expect(() => parseReleaseManifest('界'.repeat(87_382))).toThrow(
      'Manifest file must not exceed 256 KiB'
    )
  })

  test('rejects an oversized local file before reading its contents', async () => {
    const read = vi.fn<() => Promise<string>>()
    await expect(
      readReleaseManifestFile({ size: 256 * 1024 + 1, text: read })
    ).rejects.toThrow('Manifest file must not exceed 256 KiB')
    expect(read).not.toHaveBeenCalled()
  })

  test('reads a bounded local file without requesting its download URLs', async () => {
    const read = vi.fn().mockResolvedValue(manifest)
    const request = vi.fn()
    vi.stubGlobal('fetch', request)
    try {
      const values = await readReleaseManifestFile({
        size: new TextEncoder().encode(manifest).length,
        text: read,
      })
      expect(values.files).toHaveLength(1)
      expect(request).not.toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
