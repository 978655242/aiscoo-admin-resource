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
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  compareReleaseVersions,
  getCurrentStableRelease,
  getStableAfterAction,
} from '../lib/release-state'
import type {
  DesktopRelease,
  DesktopReleasePatch,
  ReleaseAction,
} from '../types'
import { ReleaseEditSheet } from './release-edit-sheet'

export interface ReleaseOperationDialogProps {
  release: DesktopRelease | null
  action: ReleaseAction | null
  releases: DesktopRelease[]
  onClose: () => void
  onSubmit: (id: number, patch: DesktopReleasePatch) => Promise<void>
}

type ReleaseOperationSessionProps = Omit<
  ReleaseOperationDialogProps,
  'release' | 'action'
> & {
  release: DesktopRelease
  action: ReleaseAction
}

export function ReleaseOperationDialog(props: ReleaseOperationDialogProps) {
  if (!props.release || !props.action) return null
  return (
    <ReleaseOperationSession
      key={`${props.release.id}-${props.action}`}
      release={props.release}
      action={props.action}
      releases={props.releases}
      onClose={props.onClose}
      onSubmit={props.onSubmit}
    />
  )
}

function ReleaseOperationSession(props: ReleaseOperationSessionProps) {
  const { t } = useTranslation()
  const submitting = useRef(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(patch: DesktopReleasePatch): Promise<void> {
    // 同一渲染周期内的双击可能早于 disabled 生效，使用同步门禁保证只提交一次。
    if (submitting.current) return
    submitting.current = true
    setPending(true)
    setError(null)
    try {
      await props.onSubmit(props.release.id, patch)
    } catch (cause) {
      setError(getServerErrorMessage(cause, t('Failed to update the release.')))
    } finally {
      submitting.current = false
      setPending(false)
    }
  }

  if (props.action === 'notes' || props.action === 'rollout') {
    return (
      <ReleaseEditSheet
        release={props.release}
        action={props.action}
        pending={pending}
        error={error}
        onClose={props.onClose}
        onSubmit={submit}
      />
    )
  }
  return (
    <ReleaseConfirmation
      release={props.release}
      action={props.action}
      releases={props.releases}
      pending={pending}
      error={error}
      onClose={() => {
        if (!submitting.current) props.onClose()
      }}
      onSubmit={submit}
    />
  )
}

type ReleaseConfirmationProps = {
  release: DesktopRelease
  action: Exclude<ReleaseAction, 'notes' | 'rollout'>
  releases: DesktopRelease[]
  pending: boolean
  error: string | null
  onClose: () => void
  onSubmit: (patch: DesktopReleasePatch) => Promise<void>
}

function ReleaseConfirmation(props: ReleaseConfirmationProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const release = props.release
  const action = props.action
  const current = getCurrentStableRelease(props.releases)
  let title = t('Publish stable release')
  let confirmLabel = t('Publish {{version}} to stable', {
    version: release.version,
  })
  let description = t(
    'Marks this version as stable. Stable-channel devices receive the highest published stable version; preview percentage and allowlist do not limit stable distribution.'
  )
  let patch: DesktopReleasePatch = { status: 1, rollout_disabled: false }
  let canSubmit = release.status !== 1
  if (action === 'pause') {
    title = t('Pause rollout')
    confirmLabel = t('Pause {{version}} rollout', { version: release.version })
    description = t(
      'Pauses future preview distribution, including allowlisted devices. The percentage and allowlist are kept for resuming.'
    )
    patch = { rollout_disabled: true }
    canSubmit = release.status === 3 && !release.rollout_disabled
  } else if (action === 'resume') {
    title = t('Resume rollout')
    confirmLabel = t('Resume {{version}} rollout', { version: release.version })
    description = t(
      'Resumes preview distribution. Keeps the existing percentage and allowlist; stable-channel distribution is unchanged.'
    )
    patch = { rollout_disabled: false }
    canSubmit = release.status === 3 && release.rollout_disabled
  } else if (action === 'withdraw') {
    title = t('Stop distribution')
    confirmLabel = t('Stop distributing {{version}}', {
      version: release.version,
    })
    description = t(
      'Stops future distribution of this release on all channels. Existing installations keep their installed version; this does not uninstall or roll them back.'
    )
    patch = { status: 0 }
    canSubmit = release.status !== 0
  }
  const affectsStable = action === 'publish' || action === 'withdraw'
  const after = affectsStable
    ? getStableAfterAction(props.releases, release, action)
    : undefined
  const olderPublish =
    action === 'publish' &&
    current &&
    compareReleaseVersions(release.version, current.version) < 0
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
      title={title}
      desc={
        <div className='min-w-0 space-y-4 break-words'>
          <dl className='grid grid-cols-2 gap-3'>
            <div className='min-w-0'>
              <dt className='text-xs'>{t('Version')}</dt>
              <dd className='text-foreground mt-1 font-medium break-all'>
                {release.version}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-xs'>{t('Platform')}</dt>
              <dd className='text-foreground mt-1 break-all'>
                {release.platform.startsWith('darwin')
                  ? t('macOS Apple Silicon')
                  : release.platform}
                <span className='text-muted-foreground mt-1 block text-xs'>
                  {release.platform}
                </span>
              </dd>
            </div>
          </dl>
          <p>{description}</p>
          {action === 'publish' && (
            <p>
              {t(
                'Preview devices may continue to receive a higher visible preview version. Publishing does not downgrade an already installed newer version.'
              )}
            </p>
          )}
          {(action === 'pause' || action === 'resume') && (
            <p>
              {t(
                'Preview percentage: {{percentage}}%; allowlisted devices: {{count}}.',
                {
                  percentage: formatNumber(release.rollout_percentage, locale),
                  count: formatNumber(
                    release.device_mid_allowlist.length,
                    locale
                  ),
                }
              )}
            </p>
          )}
          {affectsStable && (
            <dl className='grid grid-cols-2 gap-3 rounded-lg border p-3'>
              <div className='min-w-0'>
                <dt className='text-xs'>{t('Current stable release')}</dt>
                <dd className='text-foreground mt-1 break-all'>
                  {current?.version ?? t('No stable release')}
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-xs'>{t('After this action')}</dt>
                <dd className='text-foreground mt-1 break-all'>
                  {after?.version ?? t('No stable release')}
                </dd>
              </div>
            </dl>
          )}
          {olderPublish && (
            <p className='text-warning'>
              {t(
                'This version is older than the current stable release. Publishing it will not change the current stable release or downgrade installed versions.'
              )}
            </p>
          )}
          {action === 'withdraw' && release.status === 1 && !after && (
            <p className='text-warning'>
              {t(
                'No stable release will be available for future stable-channel update requests.'
              )}
            </p>
          )}
          {action === 'withdraw' &&
            release.status === 1 &&
            after &&
            current?.id === release.id && (
              <p>
                {t(
                  'Future stable-channel requests will use {{version}}. Clients do not downgrade an already installed newer version.',
                  { version: after.version }
                )}
              </p>
            )}
          {!canSubmit && (
            <p className='text-warning'>
              {t('This action is no longer available for the release status.')}
            </p>
          )}
        </div>
      }
      confirmText={props.pending ? t('Saving...') : confirmLabel}
      isLoading={props.pending}
      disabled={!canSubmit}
      destructive={action === 'withdraw'}
      handleConfirm={() => {
        if (canSubmit) void props.onSubmit(patch)
      }}
      className='w-[calc(100%-2rem)] min-w-0 data-[size=default]:sm:max-w-xl'
    >
      {props.error && (
        <Alert variant='destructive'>
          <AlertDescription className='break-words'>
            {props.error}
          </AlertDescription>
        </Alert>
      )}
    </ConfirmDialog>
  )
}
