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
import { Upload, X } from 'lucide-react'
import {
  useEffect,
  useId,
  useRef,
  useState,
  type ChangeEvent,
  type ComponentProps,
} from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getReleaseCreateDefaults,
  readReleaseManifestFile,
  releaseCreateFormSchema,
  toDesktopReleaseCreate,
  type ReleaseCreateFormValues,
} from '../lib/release-form'
import type { DesktopReleaseCreate } from '../types'
import { ReleaseCreateFields } from './release-create-fields'
import { ReleaseCreateFiles } from './release-create-files'

type ReleaseCreateSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: DesktopReleaseCreate) => Promise<void>
}

export function ReleaseCreateSheet(props: ReleaseCreateSheetProps) {
  const { t } = useTranslation()
  const form = useForm<ReleaseCreateFormValues>({
    resolver: zodResolver(releaseCreateFormSchema),
    defaultValues: getReleaseCreateDefaults(),
  })
  const [closeConfirm, setCloseConfirm] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importMessage, setImportMessage] = useState('')
  const [importFailed, setImportFailed] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const pending = useRef(false)
  const importRevision = useRef(0)
  const formId = useId()
  const inputId = useId()
  const busy = importing || form.formState.isSubmitting
  // 在渲染时订阅 isDirty，否则首次编辑后关闭读取的状态可能仍是旧值。
  const isDirty = form.formState.isDirty

  useEffect(() => {
    if (props.open) return
    importRevision.current += 1
    pending.current = false
    form.reset(getReleaseCreateDefaults())
    setCloseConfirm(false)
    setImporting(false)
    setImportMessage('')
    setImportFailed(false)
  }, [props.open, form])

  useEffect(
    () => () => {
      importRevision.current += 1
    },
    []
  )

  const requestClose = () => {
    if (busy || pending.current) return
    if (isDirty) {
      setCloseConfirm(true)
      return
    }
    props.onOpenChange(false)
  }

  const handleOpenChange: NonNullable<
    ComponentProps<typeof Sheet>['onOpenChange']
  > = (open, details) => {
    if (!open) {
      details.cancel()
      requestClose()
      return
    }
    props.onOpenChange(true)
  }

  const submit = async (values: ReleaseCreateFormValues) => {
    if (pending.current || importing) return
    pending.current = true
    form.clearErrors('root.server')
    try {
      await props.onSubmit(toDesktopReleaseCreate(values))
    } catch (error) {
      form.setError('root.server', {
        message: getServerErrorMessage(
          error,
          t('Could not create the release. Your inputs have been kept.')
        ),
      })
    } finally {
      pending.current = false
    }
  }

  const importManifest = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file || busy || pending.current) return
    const revision = ++importRevision.current
    setImporting(true)
    setImportMessage('')
    setImportFailed(false)
    try {
      const values = await readReleaseManifestFile(file)
      if (revision !== importRevision.current) return
      // 保留初始基线，导入后仍是未保存修改，关闭时需要确认。
      form.reset(values, { keepDefaultValues: true })
      await form.trigger()
      setImportMessage(
        'Manifest imported. Review the fields and complete any relative download URLs before creating the release.'
      )
    } catch (error) {
      if (revision !== importRevision.current) return
      setImportFailed(true)
      setImportMessage(
        error instanceof Error
          ? error.message
          : 'Could not read the manifest file'
      )
    } finally {
      if (revision === importRevision.current) setImporting(false)
    }
  }

  return (
    <>
      <Sheet open={props.open} onOpenChange={handleOpenChange}>
        <SheetContent
          showCloseButton={false}
          className={sideDrawerContentClassName('max-w-none sm:max-w-[720px]')}
        >
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <div className='flex items-start justify-between gap-3'>
              <SheetTitle>{t('Create desktop release')}</SheetTitle>
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                aria-label={t('Close')}
                disabled={busy}
                onClick={requestClose}
              >
                <X aria-hidden='true' />
              </Button>
            </div>
            <SheetDescription>
              {t(
                'Register an unpublished release. Distribution is enabled separately after review.'
              )}
            </SheetDescription>
          </SheetHeader>
          {props.open && (
            <Form {...form}>
              <form
                id={formId}
                onSubmit={form.handleSubmit(submit)}
                noValidate
                aria-busy={busy}
                className={sideDrawerFormClassName()}
              >
                <section
                  className='grid gap-3 rounded-lg border border-dashed p-4'
                  aria-label={t('Import release manifest')}
                >
                  <div>
                    <p className='text-sm font-medium'>
                      {t('Import release manifest')}
                    </p>
                    <p className='text-muted-foreground mt-1 text-xs leading-5'>
                      {t(
                        'Choose a local YAML file up to 256 KiB. Only metadata is read; importing does not save or publish a release.'
                      )}
                    </p>
                  </div>
                  <label htmlFor={inputId} className='sr-only'>
                    {t('Release manifest file')}
                  </label>
                  <input
                    id={inputId}
                    ref={fileInput}
                    type='file'
                    accept='.yml,.yaml,application/yaml,text/yaml'
                    className='sr-only'
                    disabled={busy}
                    onChange={(event) => {
                      void importManifest(event)
                    }}
                  />
                  <Button
                    type='button'
                    variant='outline'
                    className='justify-self-start'
                    disabled={busy}
                    onClick={() => fileInput.current?.click()}
                  >
                    {importing ? (
                      <LoadingState inline size='sm' />
                    ) : (
                      <Upload aria-hidden='true' />
                    )}
                    {t('Import YAML')}
                  </Button>
                  {importMessage && (
                    <Alert
                      role={importFailed ? 'alert' : 'status'}
                      variant={importFailed ? 'destructive' : 'default'}
                    >
                      <AlertDescription>{t(importMessage)}</AlertDescription>
                    </Alert>
                  )}
                </section>
                {form.formState.errors.root?.server?.message && (
                  <Alert variant='destructive'>
                    <AlertDescription>
                      {t(form.formState.errors.root.server.message)}
                    </AlertDescription>
                  </Alert>
                )}
                <ReleaseCreateFields disabled={busy}>
                  <ReleaseCreateFiles disabled={busy} />
                </ReleaseCreateFields>
              </form>
            </Form>
          )}
          <SheetFooter className={sideDrawerFooterClassName()}>
            <Button
              type='button'
              variant='outline'
              disabled={busy}
              onClick={requestClose}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' form={formId} disabled={busy}>
              {form.formState.isSubmitting && <LoadingState inline size='sm' />}
              {form.formState.isSubmitting
                ? t('Creating...')
                : t('Create release')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={closeConfirm}
        onOpenChange={setCloseConfirm}
        title={t('Discard unsaved changes?')}
        desc={t('Your changes have not been saved.')}
        confirmText={t('Discard changes')}
        disabled={busy}
        handleConfirm={() => {
          if (busy || pending.current) return
          setCloseConfirm(false)
          props.onOpenChange(false)
        }}
      />
    </>
  )
}
