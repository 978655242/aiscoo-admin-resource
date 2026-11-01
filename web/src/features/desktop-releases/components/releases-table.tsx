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
import type { ColumnDef, ColumnFiltersState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { formatTimestampToDate } from '@/lib/format'

import { getReleaseState, releaseStateLabelKeys } from '../lib/release-state'
import type { DesktopRelease, ReleaseAction, ReleaseState } from '../types'
import { ReleaseActions } from './release-actions'
import { ReleaseScope, ReleaseStatus } from './release-status'

export function ReleasesTable(props: {
  releases: DesktopRelease[]
  currentStableId?: number
  isLoading: boolean
  isFetching: boolean
  disabled: boolean
  onCreate: () => void
  onDetails: (release: DesktopRelease) => void
  onAction: (release: DesktopRelease, action: ReleaseAction) => void
}) {
  const { t } = useTranslation()
  const [globalFilter, setGlobalFilter] = useState('')
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const columns = useMemo<ColumnDef<DesktopRelease, unknown>[]>(
    () => [
      {
        accessorKey: 'version',
        header: t('Version'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <div className='min-w-0'>
            <Button
              variant='link'
              className='h-auto p-0 font-mono font-medium'
              onClick={() => props.onDetails(row.original)}
            >
              {row.original.version}
            </Button>
            {row.original.id === props.currentStableId && (
              <p className='text-primary text-xs'>
                {t('Current stable release')}
              </p>
            )}
            {row.original.status === 1 &&
              row.original.id !== props.currentStableId && (
                <p className='text-muted-foreground text-xs'>
                  {t('Previous stable release')}
                </p>
              )}
          </div>
        ),
      },
      {
        id: 'state',
        accessorFn: getReleaseState,
        header: t('Distribution status'),
        meta: { mobileBadge: true },
        filterFn: (row, _id, values: string[]) =>
          values.includes(getReleaseState(row.original)),
        enableGlobalFilter: false,
        cell: ({ row }) => <ReleaseStatus release={row.original} />,
      },
      {
        id: 'scope',
        header: t('Distribution scope'),
        enableGlobalFilter: false,
        cell: ({ row }) => <ReleaseScope release={row.original} />,
      },
      {
        accessorKey: 'release_date',
        header: t('Release date'),
        enableGlobalFilter: false,
        cell: ({ row }) => (
          <time
            dateTime={new Date(row.original.release_date * 1000).toISOString()}
          >
            {formatTimestampToDate(row.original.release_date)}
          </time>
        ),
      },
      {
        id: 'actions',
        enableGlobalFilter: false,
        cell: ({ row }) => (
          <ReleaseActions
            release={row.original}
            onDetails={props.onDetails}
            onAction={props.onAction}
            disabled={props.disabled}
          />
        ),
      },
    ],
    [props, t]
  )
  const { table } = useDataTable({
    data: props.releases,
    columns,
    columnFilters,
    onColumnFiltersChange: setColumnFilters,
    globalFilter,
    globalFilterFn: 'includesString',
    onGlobalFilterChange: setGlobalFilter,
    initialPagination: { pageIndex: 0, pageSize: 20 },
    enableRowSelection: false,
    enableSorting: false,
    getRowId: (row) => String(row.id),
    withFilteredRowModel: true,
    withPaginationRowModel: true,
    withSortedRowModel: false,
  })
  const filtered = globalFilter.trim() !== '' || columnFilters.length > 0
  const resetFilters = () => {
    setGlobalFilter('')
    setColumnFilters([])
    table.setPageIndex(0)
  }
  const states: ReleaseState[] = ['unpublished', 'rollout', 'paused', 'stable']
  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={props.isLoading}
      isFetching={props.isFetching}
      skeletonKeyPrefix='desktop-release'
      emptyTitle={
        filtered ? t('No matching versions') : t('No desktop versions yet')
      }
      emptyDescription={
        filtered
          ? t('Try another version or clear the filters.')
          : t('Register an update package to get started.')
      }
      emptyAction={
        <Button
          variant='outline'
          onClick={filtered ? resetFilters : props.onCreate}
        >
          {filtered ? t('Clear filters') : t('New version')}
        </Button>
      }
      toolbarProps={{
        searchPlaceholder: t('Search version number...'),
        filters: [
          {
            columnId: 'state',
            title: t('Distribution status'),
            options: states.map((state) => ({
              value: state,
              label: t(releaseStateLabelKeys[state]),
            })),
            singleSelect: true,
          },
        ],
      }}
      mobile={
        !props.isLoading && table.getRowModel().rows.length === 0 ? (
          <EmptyState
            title={
              filtered
                ? t('No matching versions')
                : t('No desktop versions yet')
            }
            description={
              filtered
                ? t('Try another version or clear the filters.')
                : t('Register an update package to get started.')
            }
            action={
              <Button
                variant='outline'
                onClick={filtered ? resetFilters : props.onCreate}
              >
                {filtered ? t('Clear filters') : t('New version')}
              </Button>
            }
          />
        ) : undefined
      }
      mobileProps={{ enableRowSelection: false }}
    />
  )
}
