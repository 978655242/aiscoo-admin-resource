import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { t } from 'i18next'
import { z } from 'zod'

import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

const endpoint = '/api/builtin-provider-config'
const queryKey = ['builtin-provider-config'] as const
const safeInteger = z.number().int().min(0).max(Number.MAX_SAFE_INTEGER)
const snapshotSchema = z.object({
  version: safeInteger,
  draft_json: z.string(),
  published_revision: safeInteger.nullable(),
  published_url: z.url({ protocol: /^https?$/ }).nullable(),
  migration_issues: z.array(z.string()),
})

export type BuiltinProviderConfigSnapshot = z.infer<typeof snapshotSchema>

// XCODE: 仅检查完整上游 envelope，不解析或重写 sparse config 叶子；规则语义由服务端校验。
const releaseSchema = z.object({
  schemaVersion: z.literal(1),
  revision: safeInteger,
  config: z.object({
    providerConfigRules: z.object({
      templateRules: z.array(z.unknown()),
      providerRules: z.array(z.unknown()),
    }),
    modelConfigRules: z.object({
      modelRules: z.array(z.unknown()),
      modelApiRules: z.array(z.unknown()),
      providerSiteRules: z.array(z.unknown()),
      templateModelRules: z.array(z.unknown()),
      builtinProviderModelRules: z.array(z.unknown()),
    }),
  }),
})

export function getBuiltinReleaseError(value: string): string | null {
  let release: unknown
  try {
    release = JSON.parse(value)
  } catch {
    return 'Invalid JSON format'
  }
  if (!releaseSchema.safeParse(release).success) {
    return 'Import a complete Release JSON with schemaVersion 1, a safe integer revision, and both rule groups.'
  }
  return null
}

export const builtinProviderConfigFormSchema = z.object({
  draft_json: z.string().superRefine((value, context) => {
    const error = getBuiltinReleaseError(value)
    if (error) context.addIssue({ code: 'custom', message: t(error) })
  }),
})

function readSnapshot(response: unknown): BuiltinProviderConfigSnapshot {
  const envelope = requireServerSuccess(response)
  const parsed = z
    .object({ success: z.literal(true), data: snapshotSchema })
    .safeParse(envelope)
  if (!parsed.success) {
    throw new Error(t('Invalid built-in configuration response'))
  }
  return parsed.data.data
}

export function useBuiltinProviderConfig() {
  return useQuery({
    queryKey,
    queryFn: async ({ signal }) =>
      readSnapshot((await api.get(endpoint, { signal })).data),
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
}

export function useSaveBuiltinProviderConfig() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (request: {
      expected_version: number
      draft_json: string
    }) => readSnapshot((await api.put(endpoint, request)).data),
    retry: false,
    onError: (error) => handleServerError(error),
    onSuccess: (snapshot) => client.setQueryData(queryKey, snapshot),
  })
}

export function usePublishBuiltinProviderConfig() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (request: { expected_version: number }) =>
      readSnapshot((await api.post(`${endpoint}/publish`, request)).data),
    retry: false,
    onError: (error) => handleServerError(error),
    onSuccess: (snapshot) => client.setQueryData(queryKey, snapshot),
  })
}
