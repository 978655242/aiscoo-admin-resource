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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, RefreshCw } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  createDesktopRelease,
  listDesktopReleases,
  updateDesktopRelease,
} from './api'
import { ReleaseCreateSheet } from './components/release-create-sheet'
import { ReleaseDetailSheet } from './components/release-detail-sheet'
import { ReleaseOperationDialog } from './components/release-operation-dialog'
import { ReleasesTable } from './components/releases-table'
import {
  getCurrentStableRelease,
  sortDesktopReleases,
} from './lib/release-state'
import type {
  DesktopRelease,
  DesktopReleaseCreate,
  DesktopReleasePatch,
  ReleaseAction,
} from './types'

const releasesQueryKey = ['desktop-releases'] as const

export function DesktopReleases() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [detailId, setDetailId] = useState<number | null>(null)
  const [operation, setOperation] = useState<{
    id: number
    action: ReleaseAction
  } | null>(null)
  const query = useQuery({
    queryKey: releasesQueryKey,
    queryFn: ({ signal }) => listDesktopReleases(signal),
    meta: { errorToast: false },
  })
  const releases = useMemo(
    () => sortDesktopReleases(query.data ?? []),
    [query.data]
  )
  const currentStable = getCurrentStableRelease(releases)
  const active = releases.filter(
    (release) => release.status === 3 && !release.rollout_disabled
  ).length
  const paused = releases.filter(
    (release) => release.status === 3 && release.rollout_disabled
  ).length
  const detail = releases.find((release) => release.id === detailId) ?? null
  const target =
    releases.find((release) => release.id === operation?.id) ?? null
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const createMutation = useMutation({
    mutationFn: createDesktopRelease,
    meta: { errorToast: false },
  })
  const updateMutation = useMutation({
    mutationFn: (input: { id: number; patch: DesktopReleasePatch }) =>
      updateDesktopRelease(input.id, input.patch),
    meta: { errorToast: false },
  })
  const busy = createMutation.isPending || updateMutation.isPending

  const handleCreate = async (input: DesktopReleaseCreate) => {
    const created = await createMutation.mutateAsync(input)
    await queryClient.cancelQueries({ queryKey: releasesQueryKey })
    queryClient.setQueryData<DesktopRelease[]>(releasesQueryKey, (previous) => [
      ...(previous ?? []),
      created,
    ])
    setCreateOpen(false)
    setDetailId(created.id)
    toast.success(t('Version registered without distribution'))
    void queryClient.invalidateQueries({ queryKey: releasesQueryKey })
  }
  const handleUpdate = async (id: number, patch: DesktopReleasePatch) => {
    const updated = await updateMutation.mutateAsync({ id, patch })
    await queryClient.cancelQueries({ queryKey: releasesQueryKey })
    queryClient.setQueryData<DesktopRelease[]>(
      releasesQueryKey,
      (previous) =>
        previous?.map((release) => (release.id === id ? updated : release)) ?? [
          updated,
        ]
    )
    setOperation(null)
    toast.success(t('Version updated'))
    void queryClient.invalidateQueries({ queryKey: releasesQueryKey })
  }
  const openDetails = useCallback(
    (release: DesktopRelease) => setDetailId(release.id),
    []
  )
  const openAction = useCallback(
    (release: DesktopRelease, action: ReleaseAction) => {
      setDetailId(null)
      setOperation({ id: release.id, action })
    },
    []
  )

  return (
    <>
      <SectionPageLayout fixedContent stackActionsOnMobile>
        <SectionPageLayout.Title>
          {t('Version Management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={() => void query.refetch()}
            disabled={query.isFetching || busy}
          >
            <RefreshCw
              className={query.isFetching ? 'animate-spin' : undefined}
              aria-hidden='true'
            />
            {t('Refresh')}
          </Button>
          <Button onClick={() => setCreateOpen(true)} disabled={busy}>
            <Plus aria-hidden='true' />
            {t('New version')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-3'>
            <p className='text-muted-foreground text-sm'>
              {t('Manage Aiscoo desktop versions and update distribution.')}
            </p>
            <section
              aria-label={t('Distribution overview')}
              className='bg-muted/20 grid shrink-0 gap-3 rounded-xl border p-4 sm:grid-cols-2'
            >
              <div>
                <p className='text-muted-foreground text-xs'>
                  {t('Current stable release')}
                </p>
                {query.isPending ? (
                  <Skeleton className='mt-2 h-6 w-36' />
                ) : (
                  <p className='mt-1 font-mono text-lg font-medium'>
                    {query.data
                      ? (currentStable?.version ??
                        t('No stable version available'))
                      : '—'}
                  </p>
                )}
                <p className='text-muted-foreground mt-1 text-xs'>
                  macOS Apple Silicon
                </p>
              </div>
              <div>
                <p className='text-muted-foreground text-xs'>
                  {t('Preview rollout')}
                </p>
                {query.isPending ? (
                  <Skeleton className='mt-2 h-6 w-36' />
                ) : (
                  <p className='mt-1 text-sm'>
                    {query.data
                      ? t('{{active}} active · {{paused}} paused', {
                          active: formatNumber(active, locale),
                          paused: formatNumber(paused, locale),
                        })
                      : '—'}
                  </p>
                )}
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('Rollout percentages apply only to the preview channel.')}
                </p>
              </div>
            </section>
            {query.isError && query.data && (
              <div
                role='alert'
                className='border-destructive/40 text-destructive rounded-lg border p-3 text-sm'
              >
                {t('Refresh failed. Showing the last loaded versions.')}{' '}
                {getServerErrorMessage(query.error)}
              </div>
            )}
            {query.isError && !query.data ? (
              <ErrorState
                description={getServerErrorMessage(query.error)}
                onRetry={() => void query.refetch()}
              />
            ) : (
              <div className='min-h-0 flex-1'>
                <ReleasesTable
                  releases={releases}
                  currentStableId={currentStable?.id}
                  isLoading={query.isPending}
                  isFetching={query.isFetching}
                  disabled={busy}
                  onCreate={() => setCreateOpen(true)}
                  onDetails={openDetails}
                  onAction={openAction}
                />
              </div>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <ReleaseCreateSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
      />
      <ReleaseDetailSheet
        release={detail}
        currentStableId={currentStable?.id}
        disabled={busy}
        onOpenChange={(open) => {
          if (!open) setDetailId(null)
        }}
        onAction={openAction}
      />
      <ReleaseOperationDialog
        key={operation ? `${operation.id}:${operation.action}` : 'closed'}
        release={target}
        action={operation?.action ?? null}
        releases={releases}
        onClose={() => setOperation(null)}
        onSubmit={handleUpdate}
      />
    </>
  )
}
