import { Eye, MoreHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import type { DesktopRelease, ReleaseAction } from '../types'

export function ReleaseActions(props: {
  release: DesktopRelease
  disabled?: boolean
  onDetails?: (release: DesktopRelease) => void
  onAction: (release: DesktopRelease, action: ReleaseAction) => void
}) {
  const { t } = useTranslation()
  const release = props.release
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant='ghost'
            size='icon-sm'
            disabled={props.disabled}
            aria-label={t('Actions for version {{version}}', {
              version: release.version,
            })}
          />
        }
      >
        <MoreHorizontal aria-hidden='true' />
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {props.onDetails && (
          <DropdownMenuItem onClick={() => props.onDetails?.(release)}>
            <Eye aria-hidden='true' />
            {t('Details')}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onClick={() => props.onAction(release, 'notes')}>
          {t('Edit release notes')}
        </DropdownMenuItem>
        {release.status !== 1 && (
          <DropdownMenuItem onClick={() => props.onAction(release, 'rollout')}>
            {release.status === 0
              ? t('Start preview rollout')
              : t('Adjust rollout')}
          </DropdownMenuItem>
        )}
        {release.status === 3 && (
          <DropdownMenuItem
            onClick={() =>
              props.onAction(
                release,
                release.rollout_disabled ? 'resume' : 'pause'
              )
            }
          >
            {release.rollout_disabled
              ? t('Resume rollout')
              : t('Pause rollout')}
          </DropdownMenuItem>
        )}
        {release.status !== 1 && (
          <DropdownMenuItem onClick={() => props.onAction(release, 'publish')}>
            {t('Publish stable release')}
          </DropdownMenuItem>
        )}
        {release.status !== 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant='destructive'
              onClick={() => props.onAction(release, 'withdraw')}
            >
              {t('Stop distribution')}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
