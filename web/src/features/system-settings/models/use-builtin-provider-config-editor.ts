import { zodResolver } from '@hookform/resolvers/zod'
import { isAxiosError } from 'axios'
import { useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'

import {
  builtinProviderConfigFormSchema,
  getBuiltinReleaseError,
  usePublishBuiltinProviderConfig,
  useSaveBuiltinProviderConfig,
  type BuiltinProviderConfigSnapshot,
} from './builtin-provider-config-api'

type Confirmation =
  | { kind: 'publish'; saveFirst: boolean }
  | { kind: 'reload' }
  | null

export function useBuiltinProviderConfigEditor(
  snapshot: BuiltinProviderConfigSnapshot,
  reload: () => Promise<BuiltinProviderConfigSnapshot>
) {
  const { t } = useTranslation()
  const save = useSaveBuiltinProviderConfig()
  const publish = usePublishBuiltinProviderConfig()
  const version = useRef(snapshot.version)
  const locked = useRef(false)
  const [operation, setOperation] = useState<
    'save' | 'publish' | 'import' | 'reload' | null
  >(null)
  const [confirmation, setConfirmation] = useState<Confirmation>(null)
  const [error, setError] = useState<unknown>(null)
  const form = useForm({
    resolver: zodResolver(builtinProviderConfigFormSchema),
    defaultValues: { draft_json: snapshot.draft_json },
  })
  const isDirty = form.formState.isDirty
  const isBusy = operation !== null
  const isConflict = isAxiosError(error) && error.response?.status === 409

  const saveDraft = form.handleSubmit(async (values) => {
    if (locked.current) return
    locked.current = true
    setOperation('save')
    setError(null)
    try {
      const saved = await save.mutateAsync({
        expected_version: version.current,
        draft_json: values.draft_json,
      })
      version.current = saved.version
      form.reset({ draft_json: saved.draft_json })
      toast.success(t('Draft saved'))
    } catch (cause) {
      setError(cause)
    } finally {
      locked.current = false
      setOperation(null)
    }
  })

  const requestPublish = async () => {
    if (locked.current || !(await form.trigger())) return
    setError(null)
    setConfirmation({ kind: 'publish', saveFirst: isDirty })
  }

  const confirm = async () => {
    if (locked.current || !confirmation) return
    locked.current = true
    setOperation(confirmation.kind)
    setError(null)
    try {
      if (confirmation.kind === 'reload') {
        const draft = form.getValues('draft_json')
        const latest = await reload()
        version.current = latest.version
        // XCODE: 冲突后只在用户确认时更新版本基线；刷新服务端状态仍保留用户原文。
        form.reset({ draft_json: latest.draft_json })
        form.setValue('draft_json', draft, { shouldDirty: true })
      } else {
        if (!(await form.trigger())) return
        let expectedVersion = version.current
        // XCODE: 必须等待当前草稿保存成功，用返回的版本发布；保存失败不能发布旧草稿。
        if (form.formState.isDirty) {
          const saved = await save.mutateAsync({
            expected_version: expectedVersion,
            draft_json: form.getValues('draft_json'),
          })
          expectedVersion = saved.version
          version.current = saved.version
          form.reset({ draft_json: saved.draft_json })
        }
        const published = await publish.mutateAsync({
          expected_version: expectedVersion,
        })
        version.current = published.version
        form.reset({ draft_json: published.draft_json })
        toast.success(t('Built-in configuration published'))
      }
      setConfirmation(null)
    } catch (cause) {
      setError(cause)
      handleServerError(cause)
    } finally {
      locked.current = false
      setOperation(null)
    }
  }

  const importFile = async (file: File) => {
    if (locked.current) return
    locked.current = true
    setOperation('import')
    setError(null)
    try {
      let text: string
      try {
        text = await file.text()
      } catch {
        throw new Error(t('Could not read the Release JSON file'))
      }
      const validationError = getBuiltinReleaseError(text)
      if (validationError) throw new Error(t(validationError))
      form.setValue('draft_json', text, {
        shouldDirty: true,
        shouldValidate: true,
      })
    } catch (cause) {
      setError(cause)
      handleServerError(cause)
    } finally {
      locked.current = false
      setOperation(null)
    }
  }

  const exportDraft = () => {
    const blob = new Blob([form.getValues('draft_json')], {
      type: 'application/json;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'builtin-provider-config.json'
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return {
    form,
    isDirty,
    isBusy,
    isConflict,
    operation,
    confirmation,
    error,
    saveDraft,
    requestPublish,
    confirm,
    importFile,
    exportDraft,
    closeConfirmation: () => {
      if (!locked.current) setConfirmation(null)
    },
    requestReload: () => {
      if (!locked.current) {
        setError(null)
        setConfirmation({ kind: 'reload' })
      }
    },
  }
}
