import { Download, Upload } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsCard } from '../components/settings-card'
import { SettingsForm } from '../components/settings-form-layout'
import {
  SettingsPageActionsPortal,
  SettingsPageFormActions,
} from '../components/settings-page-context'
import {
  useBuiltinProviderConfig,
  type BuiltinProviderConfigSnapshot,
} from './builtin-provider-config-api'
import { useBuiltinProviderConfigEditor } from './use-builtin-provider-config-editor'

export function BuiltinProviderConfigSection() {
  const { t } = useTranslation()
  const query = useBuiltinProviderConfig()
  if (query.isError && !query.data) {
    return (
      <ErrorState
        title={t('Failed to load settings')}
        description={getServerErrorMessage(query.error)}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!query.data) return <LoadingState message={t('Loading settings...')} />
  return (
    <BuiltinProviderConfigEditor
      snapshot={query.data}
      reload={async () => {
        const result = await query.refetch({ throwOnError: true })
        if (!result.data) {
          throw new Error(t('Invalid built-in configuration response'))
        }
        return result.data
      }}
    />
  )
}

function BuiltinProviderConfigEditor(props: {
  snapshot: BuiltinProviderConfigSnapshot
  reload: () => Promise<BuiltinProviderConfigSnapshot>
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const editor = useBuiltinProviderConfigEditor(props.snapshot, props.reload)
  const fileInput = useRef<HTMLInputElement>(null)
  const publishLabel = editor.isDirty
    ? t('Save and publish')
    : t('Publish saved draft')
  let confirmLabel = t('Publish saved draft')
  if (editor.confirmation?.kind === 'reload') {
    confirmLabel = t('Reload server state')
  }
  if (
    editor.confirmation?.kind === 'publish' &&
    editor.confirmation.saveFirst
  ) {
    confirmLabel = t('Save and publish')
  }
  const errorPanel = editor.error ? (
    <Alert variant='destructive'>
      <AlertDescription className='space-y-2 break-words'>
        <p>{getServerErrorMessage(editor.error)}</p>
        {editor.isConflict && (
          <p>
            {t(
              'The server version changed. Your edits are preserved. Reload the server state, review your draft, and try again.'
            )}
          </p>
        )}
      </AlertDescription>
    </Alert>
  ) : null

  return (
    <>
      <FormDirtyIndicator isDirty={editor.isDirty} />
      <FormNavigationGuard when={editor.isDirty || editor.isBusy} />
      <SettingsPageFormActions
        onSave={() => void editor.saveDraft()}
        onReset={editor.requestReload}
        resetLabel='Reload server state'
        saveLabel='Save draft'
        isSaving={editor.operation === 'save'}
        isSaveDisabled={editor.isBusy || !editor.isDirty}
        isResetDisabled={editor.isBusy}
      />
      <SettingsPageActionsPortal>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={editor.isBusy}
          onClick={() => void editor.requestPublish()}
        >
          {publishLabel}
        </Button>
      </SettingsPageActionsPortal>
      <SettingsCard
        title={t('Built-in provider configuration')}
        description={t(
          'Manage the complete upstream Release JSON used by clients. Saving a draft does not publish it.'
        )}
      >
        <Form {...editor.form}>
          <SettingsForm onSubmit={editor.saveDraft} aria-busy={editor.isBusy}>
            <dl className='grid min-w-0 gap-4 text-sm sm:grid-cols-2'>
              <div className='min-w-0 space-y-1'>
                <dt className='text-muted-foreground'>{t('Server version')}</dt>
                <dd>{formatNumber(props.snapshot.version, locale)}</dd>
              </div>
              <div className='min-w-0 space-y-1'>
                <dt className='text-muted-foreground'>
                  {t('Published revision')}
                </dt>
                <dd>
                  {props.snapshot.published_revision === null
                    ? t('Not published')
                    : formatNumber(props.snapshot.published_revision, locale)}
                </dd>
              </div>
              <div className='min-w-0 space-y-1 sm:col-span-2'>
                <dt className='text-muted-foreground'>{t('Published URL')}</dt>
                <dd className='flex min-w-0 items-start gap-2'>
                  {props.snapshot.published_url ? (
                    <>
                      <a
                        className='text-primary min-w-0 break-all underline underline-offset-4'
                        href={props.snapshot.published_url}
                        target='_blank'
                        rel='noopener noreferrer'
                      >
                        {props.snapshot.published_url}
                      </a>
                      <CopyButton value={props.snapshot.published_url} />
                    </>
                  ) : (
                    t('Not published')
                  )}
                </dd>
              </div>
            </dl>
            {props.snapshot.migration_issues.length > 0 && (
              <Alert>
                <AlertTitle>{t('Migration issues')}</AlertTitle>
                <AlertDescription>
                  <ul className='list-inside list-disc space-y-1 break-words'>
                    {[...new Set(props.snapshot.migration_issues)].map(
                      (issue) => (
                        <li key={issue}>{issue}</li>
                      )
                    )}
                  </ul>
                </AlertDescription>
              </Alert>
            )}
            {errorPanel}
            <div className='flex min-w-0 flex-wrap items-center gap-2'>
              <Input
                ref={fileInput}
                className='sr-only'
                tabIndex={-1}
                type='file'
                accept='.json,application/json'
                aria-label={t('Import Release JSON')}
                disabled={editor.isBusy}
                onChange={(event) => {
                  const file = event.currentTarget.files?.[0]
                  event.currentTarget.value = ''
                  if (file) void editor.importFile(file)
                }}
              />
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={editor.isBusy}
                onClick={() => fileInput.current?.click()}
              >
                <Upload data-icon='inline-start' aria-hidden='true' />
                {t('Import Release JSON')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={editor.isBusy}
                onClick={editor.exportDraft}
              >
                <Download data-icon='inline-start' aria-hidden='true' />
                {t('Export JSON')}
              </Button>
            </div>
            <FormField
              control={editor.form.control}
              name='draft_json'
              render={({ field }) => (
                <FormItem data-settings-form-span='full'>
                  <FormLabel>{t('Release JSON')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Import the complete upstream release, including schemaVersion, revision, and both rule groups. Rule validation runs on the server.'
                    )}
                  </FormDescription>
                  <FormControl>
                    <JsonCodeEditor
                      {...field}
                      textareaRef={field.ref}
                      ariaLabel={t('Release JSON')}
                      heightClassName='h-[55vh] min-h-72'
                      wrapToolbar
                      disabled={editor.isBusy}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsForm>
        </Form>
      </SettingsCard>
      <ConfirmDialog
        open={editor.confirmation !== null}
        onOpenChange={(open) => {
          if (!open) editor.closeConfirmation()
        }}
        title={
          editor.confirmation?.kind === 'reload'
            ? t('Reload server state?')
            : t('Publish built-in configuration?')
        }
        desc={
          editor.confirmation?.kind === 'reload'
            ? t(
                'Refresh the server version and published status while keeping your edits. Review your draft before saving it over the latest saved draft.'
              )
            : t(
                'Publish the saved draft for clients to download. Any unsaved edits will be saved first; publishing stops if saving fails. Publishing does not confirm that clients have applied the configuration.'
              )
        }
        confirmText={confirmLabel}
        handleConfirm={() => void editor.confirm()}
        isLoading={editor.isBusy}
      >
        {errorPanel}
      </ConfirmDialog>
    </>
  )
}
