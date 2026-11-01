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
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { EmptyState } from '@/components/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/ui/markdown'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'

import type { DesktopRelease, ReleaseAction } from '../types'
import { ReleaseActions } from './release-actions'
import { ReleaseScope, ReleaseStatus } from './release-status'

export interface ReleaseDetailSheetProps {
  release: DesktopRelease | null
  currentStableId?: number
  disabled?: boolean
  onOpenChange: (open: boolean) => void
  onAction: (release: DesktopRelease, action: ReleaseAction) => void
}

export function ReleaseDetailSheet(props: ReleaseDetailSheetProps) {
  return (
    <Sheet open={props.release !== null} onOpenChange={props.onOpenChange}>
      {props.release && (
        <ReleaseDetailContent
          key={props.release.id}
          release={props.release}
          currentStableId={props.currentStableId}
          disabled={props.disabled}
          onAction={props.onAction}
        />
      )}
    </Sheet>
  )
}

function ReleaseDetailContent(
  props: Omit<ReleaseDetailSheetProps, 'release' | 'onOpenChange'> & {
    release: DesktopRelease
  }
) {
  const { t } = useTranslation()
  const release = props.release
  let primaryAction: ReleaseAction = 'notes'
  let primaryLabel = t('Edit release notes')
  if (release.status === 0) {
    primaryAction = 'rollout'
    primaryLabel = t('Start preview rollout')
  } else if (release.status === 3 && release.rollout_disabled) {
    primaryAction = 'resume'
    primaryLabel = t('Resume rollout')
  } else if (release.status === 3) {
    primaryAction = 'publish'
    primaryLabel = t('Publish stable release')
  }
  const panelClassName = sideDrawerFormClassName('min-w-0 data-hidden:hidden')
  return (
    <SheetContent
      showCloseButton={false}
      className={sideDrawerContentClassName('sm:max-w-[780px]')}
    >
      <SheetHeader
        className={sideDrawerHeaderClassName('shrink-0 gap-2 pr-14')}
      >
        <SheetTitle className='min-w-0 break-words'>
          {t('Version {{version}}', { version: release.version })}
        </SheetTitle>
        <SheetDescription className='break-all'>
          {release.platform.startsWith('darwin')
            ? t('macOS Apple Silicon')
            : release.platform}
        </SheetDescription>
        <div className='flex flex-wrap items-center gap-2'>
          <ReleaseStatus release={release} />
          {release.status === 1 && (
            <Badge variant='outline'>
              {release.id === props.currentStableId
                ? t('Current stable release')
                : t('Historical stable release')}
            </Badge>
          )}
        </div>
        <SheetClose
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              className='absolute top-3 right-3'
              aria-label={t('Close')}
            />
          }
        >
          <X aria-hidden='true' />
        </SheetClose>
      </SheetHeader>
      <Tabs defaultValue='overview' className='min-h-0 min-w-0 flex-1 gap-0'>
        <div className='shrink-0 overflow-x-auto border-b px-4 sm:px-6'>
          <TabsList
            variant='line'
            className='min-w-max gap-4 group-data-horizontal/tabs:h-11'
            aria-label={t('Release details')}
          >
            <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
            <TabsTrigger value='notes'>{t('Release notes')}</TabsTrigger>
            <TabsTrigger value='artifacts'>{t('Artifacts')}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value='overview' className={panelClassName}>
          <ReleaseOverview release={release} />
        </TabsContent>
        <TabsContent value='notes' className={panelClassName}>
          {(['zh-CN', 'en-US'] as const).map((language) => (
            <section
              key={language}
              lang={language}
              className='min-w-0 space-y-3'
              aria-label={
                language === 'zh-CN' ? t('Simplified Chinese') : t('English')
              }
            >
              <h3 className='font-medium'>
                {language === 'zh-CN' ? t('Simplified Chinese') : t('English')}
              </h3>
              {release.release_notes_by_locale[language]?.trim() ? (
                <Markdown className='min-w-0 [overflow-wrap:anywhere] break-words'>
                  {release.release_notes_by_locale[language] ?? ''}
                </Markdown>
              ) : (
                <p className='text-muted-foreground'>
                  {t('No release notes for this language.')}
                </p>
              )}
            </section>
          ))}
        </TabsContent>
        <TabsContent value='artifacts' className={panelClassName}>
          <ReleaseArtifacts release={release} />
        </TabsContent>
      </Tabs>
      <SheetFooter
        className={sideDrawerFooterClassName(
          'flex shrink-0 flex-row items-center justify-end'
        )}
      >
        <ReleaseActions
          release={release}
          disabled={props.disabled}
          onAction={props.onAction}
        />
        <Button
          disabled={props.disabled}
          onClick={() => props.onAction(release, primaryAction)}
        >
          {primaryLabel}
        </Button>
      </SheetFooter>
    </SheetContent>
  )
}

function ReleaseOverview(props: { release: DesktopRelease }) {
  const { t } = useTranslation()
  const release = props.release
  const darwin = release.platform.startsWith('darwin')
  const details = [
    { label: t('Status'), value: <ReleaseStatus release={release} /> },
    {
      label: t('Distribution scope'),
      value: <ReleaseScope release={release} />,
    },
    {
      label: t('Release date'),
      value: formatTimestampToDate(release.release_date),
    },
    {
      label: darwin
        ? t('Minimum Darwin kernel version')
        : t('Minimum system version'),
      value: release.minimum_system_version || t('Not specified'),
    },
    {
      label: t('Created At'),
      value: formatTimestampToDate(release.created_at),
    },
    {
      label: t('Updated At'),
      value: formatTimestampToDate(release.updated_at),
    },
  ]
  return (
    <>
      <dl className='grid min-w-0 gap-5 sm:grid-cols-2'>
        {details.map((detail) => (
          <div key={detail.label} className='min-w-0 space-y-1.5'>
            <dt className='text-muted-foreground text-xs'>{detail.label}</dt>
            <dd className='break-words'>{detail.value}</dd>
          </div>
        ))}
      </dl>
      {darwin && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'The minimum system version is a Darwin kernel version, not a macOS marketing version.'
          )}
        </p>
      )}
    </>
  )
}

function ReleaseArtifacts(props: { release: DesktopRelease }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const files = [...props.release.files].sort(
    (left, right) => (left.position ?? 0) - (right.position ?? 0)
  )
  if (files.length === 0) return <EmptyState title={t('No artifacts')} />
  return (
    <ol className='min-w-0 space-y-6'>
      {files.map((file, index) => (
        <li
          key={file.id ?? `${index}-${file.url}`}
          className='min-w-0 space-y-3 rounded-lg border p-4'
        >
          <div className='flex flex-wrap items-center gap-2'>
            <h3 className='font-medium'>
              {t('Artifact {{number}}', {
                number: formatNumber(index + 1, locale),
              })}
            </h3>
            {index === 0 && (
              <Badge variant='secondary'>{t('Primary file')}</Badge>
            )}
          </div>
          <dl className='min-w-0 space-y-3'>
            {[
              {
                label: t('URL'),
                value: file.url,
                copyLabel: t('Copy artifact URL'),
              },
              {
                label: t('SHA-512'),
                value: file.sha512,
                copyLabel: t('Copy SHA-512'),
              },
            ].map((field) => (
              <div key={field.label} className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>{field.label}</dt>
                <dd className='flex min-w-0 items-start gap-2'>
                  <code className='min-w-0 flex-1 pt-1 font-mono text-xs leading-5 break-all'>
                    {field.value}
                  </code>
                  <CopyButton
                    value={field.value}
                    aria-label={field.copyLabel}
                    className='size-7'
                    iconClassName='size-3.5'
                  />
                </dd>
              </div>
            ))}
            <div>
              <dt className='text-muted-foreground text-xs'>{t('Size')}</dt>
              <dd className='mt-1 break-words'>
                {t('{{size}} bytes', { size: formatNumber(file.size, locale) })}
              </dd>
            </div>
          </dl>
        </li>
      ))}
    </ol>
  )
}
