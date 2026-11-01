import { describe, expect, it } from 'vitest'

import {
  compareReleaseVersions,
  getCurrentStableRelease,
  getReleaseState,
  getStableAfterAction,
  isValidDesktopReleaseVersion,
  releaseStateLabelKeys,
  sortDesktopReleases,
} from '../lib/release-state'
import type { DesktopRelease } from '../types'

function release(overrides: Partial<DesktopRelease> = {}): DesktopRelease {
  return {
    id: 1,
    version: '3.9.0',
    platform: 'darwin-aarch64',
    status: 0,
    rollout_percentage: 0,
    rollout_disabled: false,
    device_mid_allowlist: [],
    minimum_system_version: '',
    release_date: 1793491200,
    release_notes_by_locale: {},
    files: [
      {
        url: 'https://downloads.example.com/app.dmg',
        sha512: 'digest',
        size: 1,
      },
    ],
    created_at: 1793491200,
    updated_at: 1793491200,
    ...overrides,
  }
}

describe('release state', () => {
  it.each([
    { status: 0, rollout_disabled: false, state: 'unpublished' },
    { status: 0, rollout_disabled: true, state: 'unpublished' },
    { status: 3, rollout_disabled: false, state: 'rollout' },
    { status: 3, rollout_disabled: true, state: 'paused' },
    { status: 1, rollout_disabled: false, state: 'stable' },
    { status: 1, rollout_disabled: true, state: 'stable' },
  ] as const)(
    'maps status $status with rollout_disabled=$rollout_disabled to $state',
    (scenario) => {
      expect(getReleaseState(release(scenario))).toBe(scenario.state)
    }
  )

  it('provides English translation keys for each release state', () => {
    expect(releaseStateLabelKeys).toEqual({
      unpublished: 'Not distributing',
      rollout: 'Preview rollout',
      paused: 'Rollout paused',
      stable: 'Stable release',
    })
  })
})

describe('semantic release versions', () => {
  it.each([
    ['3.9.0', '3.10.0'],
    ['3.10.9', '3.10.10'],
    ['3.9.0', '4.0.0-alpha'],
    ['1.0.0-alpha', '1.0.0-alpha.1'],
    ['1.0.0-alpha.1', '1.0.0-alpha.beta'],
    ['1.0.0-alpha.beta', '1.0.0-beta'],
    ['1.0.0-beta', '1.0.0-beta.2'],
    ['1.0.0-beta.2', '1.0.0-beta.11'],
    ['1.0.0-beta.11', '1.0.0-rc.1'],
    ['1.0.0-rc.1', '1.0.0'],
    ['1.0.0-2', '1.0.0-a'],
    ['1.0.0-Z', '1.0.0-a'],
    ['1.0.0-a.9', '1.0.0-a.a'],
    ['9007199254740992.0.0', '9007199254740993.0.0'],
    ['1.9007199254740992.0', '1.9007199254740993.0'],
    ['1.0.9223372036854775806', '1.0.9223372036854775807'],
    ['1.0.0-9007199254740992', '1.0.0-9007199254740993'],
  ])('orders %s before %s without numeric precision loss', (a, b) => {
    expect(compareReleaseVersions(a, b)).toBeLessThan(0)
    expect(compareReleaseVersions(b, a)).toBeGreaterThan(0)
  })

  it.each([
    ['1.0.0--1', '1.0.0-0'],
    ['1.0.0--2', '1.0.0--1'],
    ['1.0.0--9223372036854775808', '1.0.0--9223372036854775807'],
    ['1.0.0-9223372036854775807', '1.0.0--9223372036854775809'],
    ['1.0.0--tag', '1.0.0-9223372036854775808'],
    ['1.0.0-100000000000000000000', '1.0.0-99999999999999999999'],
  ])(
    'uses backend signed int64 classification to order %s before %s',
    (a, b) => {
      expect(compareReleaseVersions(a, b)).toBeLessThan(0)
      expect(compareReleaseVersions(b, a)).toBeGreaterThan(0)
    }
  )

  it.each([
    ['3.10.0', '3.10.0'],
    ['3.10.0-rc.1', '3.10.0-rc.1'],
    ['3.10.0-rc.01', '3.10.0-rc.1'],
  ])('gives equal precedence to %s and %s', (a, b) => {
    expect(compareReleaseVersions(a, b)).toBe(0)
  })

  it.each([
    '0.0.0',
    '3.10.0-rc.1',
    '3.10.0-custom-tag.01',
    '9223372036854775807.0.0',
    '0.9223372036854775807.0',
    '0.0.9223372036854775807',
  ])(
    'accepts the backend version shape and integer range for %s',
    (version) => {
      expect(isValidDesktopReleaseVersion(version)).toBe(true)
    }
  )

  it.each([
    '',
    '3.10',
    'v3.10.0',
    '03.10.0',
    '3.010.0',
    '3.10.00',
    '3.10.0-',
    '3.10.0-rc..1',
    '3.10.0+build.1',
    '3.10.0\n',
    '3.10.0-rc.1\n',
    '9223372036854775808.0.0',
    '0.9223372036854775808.0',
    '0.0.9223372036854775808',
  ])('rejects versions the backend rejects: %s', (version) => {
    expect(isValidDesktopReleaseVersion(version)).toBe(false)
  })

  it('uses deterministic text ordering for malformed historical versions', () => {
    expect(compareReleaseVersions('legacy-a', 'legacy-b')).toBeLessThan(0)
    expect(compareReleaseVersions('legacy-b', 'legacy-a')).toBeGreaterThan(0)
    expect(compareReleaseVersions('legacy-a', 'legacy-a')).toBe(0)
  })
})

describe('release sorting and stable supply', () => {
  it('sorts readonly releases by descending version then id without mutation', () => {
    const releases = Object.freeze([
      Object.freeze(release({ id: 30, version: '3.9.0' })),
      Object.freeze(release({ id: 10, version: '3.10.0' })),
      Object.freeze(release({ id: 99, version: '3.10.0-rc.1' })),
      Object.freeze(release({ id: 20, version: '3.10.0' })),
    ])

    const sorted = sortDesktopReleases(releases)

    expect(sorted.map((item) => item.id)).toEqual([20, 10, 99, 30])
    expect(releases.map((item) => item.id)).toEqual([30, 10, 99, 20])
    expect(sorted).not.toBe(releases)
    expect(sorted[0]).toBe(releases[3])
  })

  it('selects the highest supported stable version with artifacts despite rollout_disabled', () => {
    const current = release({
      id: 2,
      version: '3.10.0',
      status: 1,
      rollout_disabled: true,
    })
    const releases = Object.freeze([
      release({ id: 1, version: '3.9.0', status: 1 }),
      current,
      release({ id: 3, version: '3.11.0', status: 1, files: [] }),
      release({ id: 4, version: '3.12.0', status: 3 }),
      release({ id: 5, version: '3.13.0', status: 3, rollout_disabled: true }),
      release({ id: 6, version: '3.14.0', status: 0 }),
      release({ id: 7, version: '3.15.0', status: 1, platform: 'win32-x64' }),
      release({ id: 8, version: '3.16.0', status: 1, platform: 'darwin-x64' }),
    ])

    expect(getCurrentStableRelease(releases)).toBe(current)
  })

  it.each(['darwin-arm64', ' DARWIN-AARCH64 '])(
    'selects a stable release for the backend platform alias %s',
    (platform) => {
      const stable = release({ platform, status: 1 })
      expect(getCurrentStableRelease([stable])).toBe(stable)
    }
  )

  it('breaks equal-version stable ties by descending id', () => {
    const older = release({ id: 1, status: 1 })
    const newer = release({ id: 2, status: 1 })
    expect(getCurrentStableRelease([older, newer])).toBe(newer)
  })

  it('has no stable supply when the list is empty or no stable artifacts exist', () => {
    expect(getCurrentStableRelease([])).toBeUndefined()
    expect(
      getCurrentStableRelease([
        release({ status: 0 }),
        release({ id: 2, status: 3 }),
        release({ id: 3, status: 1, files: [] }),
      ])
    ).toBeUndefined()
  })

  it('withdraws the final stable release without inventing a fallback', () => {
    const stable = Object.freeze(release({ status: 1 }))
    const preview = Object.freeze(
      release({ id: 2, version: '3.10.0', status: 3 })
    )
    const releases = Object.freeze([stable, preview])

    expect(getStableAfterAction(releases, stable, 'withdraw')).toBeUndefined()
    expect(stable.status).toBe(1)
    expect(releases).toEqual([stable, preview])
  })

  it('withdraws the current stable and falls back to the highest remaining stable', () => {
    const older = release({ id: 1, status: 1, version: '3.9.0' })
    const current = release({ id: 2, status: 1, version: '3.10.0' })
    expect(getStableAfterAction([older, current], current, 'withdraw')).toBe(
      older
    )
  })

  it('publishes a lower version while retaining the higher stable supply', () => {
    const current = release({ id: 1, status: 1, version: '3.10.0' })
    const target = Object.freeze(
      release({ id: 2, status: 3, version: '3.9.0' })
    )
    const releases = Object.freeze([target, current])

    expect(getStableAfterAction(releases, target, 'publish')).toBe(current)
    expect(target.status).toBe(3)
  })

  it('publishes a higher version as the stable supply without mutating the target', () => {
    const current = release({ id: 1, status: 1, version: '3.9.0' })
    const target = Object.freeze(
      release({ id: 2, status: 3, version: '3.10.0' })
    )
    const releases = Object.freeze([current, target])

    expect(getStableAfterAction(releases, target, 'publish')).toEqual({
      ...target,
      status: 1,
    })
    expect(target.status).toBe(3)
    expect(releases[1]).toBe(target)
  })

  it('does not replace stable supply when a published target has no artifacts', () => {
    const current = release({ id: 1, status: 1 })
    const target = release({ id: 2, version: '3.10.0', files: [] })
    expect(getStableAfterAction([current, target], target, 'publish')).toBe(
      current
    )
  })
})
