import type { DesktopRelease, ReleaseState } from '../types'

export const releaseStateLabelKeys: Record<ReleaseState, string> = {
  unpublished: 'Not distributing',
  rollout: 'Preview rollout',
  paused: 'Rollout paused',
  stable: 'Stable release',
}

export function getReleaseState(release: DesktopRelease): ReleaseState {
  if (release.status === 1) return 'stable'
  if (release.status === 3) {
    return release.rollout_disabled ? 'paused' : 'rollout'
  }
  return 'unpublished'
}

const releaseVersionPattern =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/
const maxVersionComponent = 9223372036854775807n

type ReleaseVersion = {
  core: bigint[]
  prerelease: string[]
}

function parseReleaseVersion(value: string): ReleaseVersion | undefined {
  const match = releaseVersionPattern.exec(value)
  if (!match) return undefined
  const core = match.slice(1, 4).map((part) => BigInt(part))
  if (core.some((part) => part > maxVersionComponent)) return undefined
  return { core, prerelease: match[4]?.split('.') ?? [] }
}

export function isValidDesktopReleaseVersion(value: string): boolean {
  return parseReleaseVersion(value) !== undefined
}

function parsePrereleaseInteger(value: string): bigint | undefined {
  if (!/^-?\d+$/.test(value)) return undefined
  const numeric = BigInt(value)
  // 后端 compareDesktopReleaseVersions 用 strconv.Atoi 判定：负数且在 int64
  // 范围内也算数字，越界则按非数字排序；此处对齐后端而非完整 SemVer 规范。
  if (numeric < -maxVersionComponent - 1n || numeric > maxVersionComponent) {
    return undefined
  }
  return numeric
}

/** Ascending precedence matching the backend's X.Y.Z[-prerelease] contract. */
export function compareReleaseVersions(a: string, b: string): number {
  const left = parseReleaseVersion(a)
  const right = parseReleaseVersion(b)
  if (!left || !right) {
    if (a === b) return 0
    return a < b ? -1 : 1
  }

  // 后端接受 int64 的版本分量，BigInt 避免超过 2^53 后把相邻版本排成相等。
  for (let index = 0; index < 3; index++) {
    if (left.core[index] !== right.core[index]) {
      return left.core[index] < right.core[index] ? -1 : 1
    }
  }
  if (left.prerelease.length === 0 && right.prerelease.length === 0) return 0
  if (left.prerelease.length === 0) return 1
  if (right.prerelease.length === 0) return -1

  const sharedLength = Math.min(left.prerelease.length, right.prerelease.length)
  for (let index = 0; index < sharedLength; index++) {
    const x = left.prerelease[index]
    const y = right.prerelease[index]
    if (x === y) continue
    const xNumber = parsePrereleaseInteger(x)
    const yNumber = parsePrereleaseInteger(y)
    if (xNumber !== undefined && yNumber !== undefined) {
      if (xNumber !== yNumber) return xNumber < yNumber ? -1 : 1
    } else if (xNumber !== undefined) {
      return -1
    } else if (yNumber !== undefined) {
      return 1
    } else {
      return x < y ? -1 : 1
    }
  }
  return left.prerelease.length - right.prerelease.length
}

export function sortDesktopReleases(
  releases: readonly DesktopRelease[]
): DesktopRelease[] {
  return [...releases].sort(
    (a, b) => compareReleaseVersions(b.version, a.version) || b.id - a.id
  )
}

export function getCurrentStableRelease(
  releases: readonly DesktopRelease[]
): DesktopRelease | undefined {
  return sortDesktopReleases(releases).find((release) => {
    const platform = release.platform.trim().toLowerCase()
    return (
      (platform === 'darwin-aarch64' || platform === 'darwin-arm64') &&
      release.status === 1 &&
      release.files.length > 0
    )
  })
}

export function getStableAfterAction(
  releases: readonly DesktopRelease[],
  target: DesktopRelease,
  action: 'publish' | 'withdraw'
): DesktopRelease | undefined {
  const updated: DesktopRelease = {
    ...target,
    status: action === 'publish' ? 1 : 0,
  }
  return getCurrentStableRelease([
    ...releases.filter((release) => release.id !== target.id),
    updated,
  ])
}
