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
import { zodResolver } from '@hookform/resolvers/zod'
import { X } from 'lucide-react'
import { useId, useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { releaseNotesSchema } from '../lib/release-form'
import type { DesktopRelease, DesktopReleasePatch } from '../types'
import { ReleaseNotesEditor } from './release-notes-editor'

interface ReleaseEditSheetProps {
  release: DesktopRelease
  action: 'notes' | 'rollout'
  pending: boolean
  error: string | null
  onClose: () => void
  onSubmit: (patch: DesktopReleasePatch) => Promise<void>
}

const notesFormSchema = z.object({
  release_notes_by_locale: releaseNotesSchema,
})
const percentageError = 'Enter a whole percentage from 0 to 100.'
const rolloutFormSchema = z.object({
  rollout_percentage: z
    .number({ error: percentageError })
    .refine(
      (value) => Number.isInteger(value) && value >= 0 && value <= 100,
      percentageError
    ),
  allowlist: z.string().superRefine((value, context) => {
    const mids = parseDeviceMidAllowlist(value)
    if (mids.length > 200) {
      context.addIssue({
        code: 'custom',
        message: 'Allow at most 200 unique device MIDs.',
      })
    }
    if (mids.some((mid) => !/^[a-zA-Z0-9-]{1,64}$/.test(mid))) {
      context.addIssue({
        code: 'custom',
        message: 'Each MID must contain 1–64 letters, digits or hyphens.',
      })
    }
  }),
})

function parseDeviceMidAllowlist(value: string): string[] {
  return [
    ...new Set(
      value
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter(Boolean)
    ),
  ]
}

export function ReleaseEditSheet(props: ReleaseEditSheetProps) {
  if (props.action === 'notes') return <ReleaseNotesEdit {...props} />
  return <ReleaseRolloutEdit {...props} />
}

function ReleaseNotesEdit(props: ReleaseEditSheetProps) {
  const { t } = useTranslation()
  const formId = useId()
  const form = useForm<z.infer<typeof notesFormSchema>>({
    resolver: zodResolver(notesFormSchema),
    defaultValues: {
      release_notes_by_locale: props.release.release_notes_by_locale,
    },
  })
  return (
    <ReleaseEditorFrame
      {...props}
      formId={formId}
      dirty={form.formState.isDirty}
      title={t('Edit release notes')}
      description={t(
        'Changes only the bilingual release notes. Distribution status, rollout scope and artifacts stay the same.'
      )}
      saveLabel={t('Save release notes')}
    >
      <Form {...form}>
        <form
          id={formId}
          noValidate
          className={sideDrawerFormClassName('min-w-0')}
          onSubmit={form.handleSubmit((values) =>
            props.onSubmit({
              release_notes_by_locale: values.release_notes_by_locale,
            })
          )}
        >
          <FormField
            control={form.control}
            name='release_notes_by_locale'
            render={({ field }) => (
              <FormItem className='min-w-0'>
                <FormControl>
                  <ReleaseNotesEditor
                    value={field.value}
                    onChange={field.onChange}
                    disabled={props.pending}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </ReleaseEditorFrame>
  )
}

function ReleaseRolloutEdit(props: ReleaseEditSheetProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const formId = useId()
  const form = useForm<z.infer<typeof rolloutFormSchema>>({
    resolver: zodResolver(rolloutFormSchema),
    defaultValues: {
      rollout_percentage: props.release.rollout_percentage,
      allowlist: props.release.device_mid_allowlist.join('\n'),
    },
  })
  const percentage = form.watch('rollout_percentage')
  const count = parseDeviceMidAllowlist(form.watch('allowlist')).length
  const dirtyFields = form.formState.dirtyFields
  const hasScopeChanges = !!(
    dirtyFields.rollout_percentage || dirtyFields.allowlist
  )
  const draft = props.release.status === 0
  const editable = props.release.status === 0 || props.release.status === 3
  const paused = props.release.status === 3 && props.release.rollout_disabled
  let footerNote = t(
    'Saving changes only the preview percentage and allowlist. Stable-channel distribution is unchanged.'
  )
  if (paused) {
    footerNote = t(
      'Saving keeps this rollout paused, including the allowlist. Use Resume rollout separately to distribute again.'
    )
  } else if (draft) {
    footerNote = t(
      'Starts preview distribution with the scope above. Stable-channel distribution is unchanged.'
    )
  }
  return (
    <ReleaseEditorFrame
      {...props}
      formId={formId}
      dirty={form.formState.isDirty}
      title={draft ? t('Start preview rollout') : t('Adjust rollout')}
      description={t(
        'Only preview-channel devices can receive this release. Eligible preview devices receive it when it is newer than the current stable release and passes the percentage or allowlist rule.'
      )}
      saveLabel={draft ? t('Start preview rollout') : t('Save rollout')}
      footerNote={footerNote}
      saveDisabled={!editable || (!draft && !hasScopeChanges)}
    >
      <Form {...form}>
        <form
          id={formId}
          noValidate
          className={sideDrawerFormClassName('min-w-0')}
          onSubmit={form.handleSubmit(async (values) => {
            if (!editable || (!draft && !hasScopeChanges)) return
            const patch: DesktopReleasePatch = {}
            // 按脏字段提交，避免仅改比例时覆盖其他管理员刚更新的白名单，反之亦然。
            if (draft || dirtyFields.rollout_percentage) {
              patch.rollout_percentage = values.rollout_percentage
            }
            if (draft || dirtyFields.allowlist) {
              patch.device_mid_allowlist = parseDeviceMidAllowlist(
                values.allowlist
              )
            }
            // 启动草稿显式确认完整范围；已暂停版本仍只改范围，由恢复操作启用分发。
            if (draft) {
              patch.status = 3
              patch.rollout_disabled = false
            }
            await props.onSubmit(patch)
          })}
        >
          <FieldGroup>
            <FormField
              control={form.control}
              name='rollout_percentage'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Rollout percentage')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type='number'
                      inputMode='numeric'
                      min={0}
                      max={100}
                      step={1}
                      value={Number.isFinite(field.value) ? field.value : ''}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                      disabled={props.pending || !editable}
                    />
                  </FormControl>
                  <ToggleGroup
                    value={
                      Number.isFinite(percentage) ? [String(percentage)] : []
                    }
                    onValueChange={(values) => {
                      const next = values[0]
                      if (next !== undefined) {
                        form.setValue('rollout_percentage', Number(next), {
                          shouldDirty: true,
                          shouldValidate: true,
                        })
                      }
                    }}
                    disabled={props.pending || !editable}
                    variant='outline'
                    spacing={2}
                    className='grid w-full grid-cols-5'
                    aria-label={t('Quick rollout percentage')}
                  >
                    {[0, 10, 25, 50, 100].map((value) => (
                      <ToggleGroupItem
                        key={value}
                        value={String(value)}
                        className='w-full'
                      >
                        {formatNumber(value, locale)}%
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                  <FormDescription>
                    {percentage === 0
                      ? t(
                          'Only allowlisted devices can receive this preview at 0%. Without an allowlist, no devices receive it.'
                        )
                      : t(
                          'The percentage selects preview devices deterministically. The allowlist adds devices outside that percentage.'
                        )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='allowlist'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Device MID allowlist')}</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      rows={8}
                      className='min-h-40 font-mono break-all'
                      disabled={props.pending || !editable}
                      spellCheck={false}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'One MID per line, using 1–64 letters, digits or hyphens. Empty lines and duplicates are removed.'
                    )}
                  </FormDescription>
                  <p
                    className='text-muted-foreground text-xs'
                    aria-live='polite'
                  >
                    {t('Allowlisted devices: {{count}} / 200', {
                      count: formatNumber(count, locale),
                    })}
                  </p>
                  <FormMessage />
                </FormItem>
              )}
            />
          </FieldGroup>
          {!editable && (
            <p role='alert' className='text-destructive'>
              {t('This action is no longer available for the release status.')}
            </p>
          )}
        </form>
      </Form>
    </ReleaseEditorFrame>
  )
}

function ReleaseEditorFrame(
  props: ReleaseEditSheetProps & {
    title: string
    description: string
    saveLabel: string
    formId: string
    dirty: boolean
    children: ReactNode
    footerNote?: string
    saveDisabled?: boolean
  }
) {
  const { t } = useTranslation()
  const [confirmClose, setConfirmClose] = useState(false)
  function close(): void {
    if (props.pending) return
    if (props.dirty) setConfirmClose(true)
    else props.onClose()
  }
  return (
    <>
      <Sheet
        open
        onOpenChange={(open) => {
          if (!open) close()
        }}
      >
        <SheetContent
          showCloseButton={false}
          className={sideDrawerContentClassName('sm:max-w-[780px]')}
        >
          <SheetHeader
            className={sideDrawerHeaderClassName('shrink-0 gap-2 pr-14')}
          >
            <SheetTitle>{props.title}</SheetTitle>
            <p className='text-muted-foreground text-xs break-all'>
              {t('Version {{version}}', { version: props.release.version })} ·{' '}
              {props.release.platform.startsWith('darwin')
                ? t('macOS Apple Silicon')
                : props.release.platform}
            </p>
            <SheetDescription className='break-words'>
              {props.description}
            </SheetDescription>
            <Button
              variant='ghost'
              size='icon-sm'
              disabled={props.pending}
              className='absolute top-3 right-3'
              aria-label={t('Close')}
              onClick={close}
            >
              <X aria-hidden='true' />
            </Button>
          </SheetHeader>
          {props.children}
          <SheetFooter
            className={sideDrawerFooterClassName('shrink-0 sm:flex-wrap')}
          >
            {props.footerNote && (
              <p className='text-muted-foreground col-span-2 text-xs leading-5 sm:mr-auto sm:max-w-sm'>
                {props.footerNote}
              </p>
            )}
            {props.error && (
              <Alert
                variant='destructive'
                className='col-span-2 min-w-0 sm:order-first sm:basis-full'
              >
                <AlertDescription className='break-words'>
                  {props.error}
                </AlertDescription>
              </Alert>
            )}
            <Button
              type='button'
              variant='outline'
              disabled={props.pending}
              onClick={close}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              form={props.formId}
              disabled={props.pending || props.saveDisabled}
            >
              {props.pending ? t('Saving...') : props.saveLabel}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        title={t('Discard unsaved changes?')}
        desc={t('Your changes have not been saved.')}
        cancelBtnText={t('Keep editing')}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          if (props.pending) return
          setConfirmClose(false)
          props.onClose()
        }}
      />
    </>
  )
}
