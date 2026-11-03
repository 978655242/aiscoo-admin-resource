import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'

import { SettingsPageFrame } from '../../components/settings-page'
import { SettingsPageFormActions } from '../../components/settings-page-context'
import { BuiltinProviderConfigSection } from '../builtin-provider-config-section'
import { getModelsSectionNavItems } from '../section-registry'

const endpoint = '/api/builtin-provider-config'
const release = JSON.stringify({
  schemaVersion: 1,
  revision: 43,
  config: {
    providerConfigRules: { templateRules: [], providerRules: [] },
    modelConfigRules: {
      modelRules: [
        {
          modelMatch: '^example-model$',
          config: {
            enabled: false,
            properties: { contextWindow: 32768 },
            optionSpecs: { maxOutputTokens: null },
          },
        },
      ],
      modelApiRules: [],
      providerSiteRules: [],
      templateModelRules: [],
      builtinProviderModelRules: [],
    },
  },
})
const editedRelease = release.replace('32768', '65536')
const snapshot = {
  version: 7,
  draft_json: release,
  published_revision: 43,
  published_url: 'https://config.example.test/releases/43.json',
  migration_issues: ['Legacy model example-broken requires review'],
}
let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: snapshot },
  })
  vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: true,
      data: { ...snapshot, version: 8, draft_json: editedRelease },
    },
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...snapshot,
        version: 9,
        draft_json: editedRelease,
        published_revision: 44,
        published_url: 'https://config.example.test/releases/44.json',
      },
    },
  })
})

afterEach(async () => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  await i18next.changeLanguage('en')
})

async function show() {
  const root = createRootRoute({ component: Outlet })
  const page = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => (
      <SettingsPageFrame
        title='Built-in provider configuration'
        stackActionsOnMobile
      >
        <BuiltinProviderConfigSection />
      </SettingsPageFrame>
    ),
  })
  const other = createRoute({
    getParentRoute: () => root,
    path: '/403',
    component: () => <p>Another settings page</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([page, other]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return router
}

async function editor() {
  return screen.findByLabelText('Release JSON', { selector: 'textarea' })
}

function edit(value = editedRelease) {
  fireEvent.input(screen.getByRole('textbox', { name: 'Release JSON' }), {
    target: { value },
  })
}

function conflict() {
  const error = new AxiosError('Request failed with status code 409')
  error.response = {
    status: 409,
    data: { success: false, message: 'Configuration changed on the server' },
  } as typeof error.response
  return error
}

describe('built-in provider configuration', () => {
  it('gives the built-in settings title a full mobile row and allows its actions to wrap', async () => {
    await show()
    await editor()
    const title = screen.getByRole('heading', {
      name: 'Built-in provider configuration',
    })
    expect(title.parentElement).toHaveClass('max-sm:basis-full')
    for (const name of [
      'Reload server state',
      'Save draft',
      'Publish saved draft',
    ]) {
      expect(screen.getByRole('button', { name }).parentElement).toHaveClass(
        'flex-wrap'
      )
    }
  })

  it('keeps the existing settings header layout when mobile stacking is not requested', () => {
    render(
      <SettingsPageFrame title='Other settings'>
        <SettingsPageFormActions onSave={() => {}} />
      </SettingsPageFrame>
    )
    expect(
      screen.getByRole('heading', { name: 'Other settings' }).parentElement
    ).not.toHaveClass('max-sm:basis-full')
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeVisible()
  })

  it('loads the real server snapshot and registers the model settings navigation entry', async () => {
    await show()
    expect(await editor()).toHaveValue(release)
    expect(
      screen.getByText('Server version').nextElementSibling
    ).toHaveTextContent('7')
    expect(
      screen.getByText('Published revision').nextElementSibling
    ).toHaveTextContent('43')
    expect(
      screen.getByRole('link', { name: snapshot.published_url })
    ).toHaveAttribute('href', snapshot.published_url)
    expect(screen.getByText(snapshot.migration_issues[0])).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled()
    expect(getModelsSectionNavItems(i18next.t)).toContainEqual(
      expect.objectContaining({
        url: '/system-settings/models/builtin-provider-config',
      })
    )
    expect(api.get).toHaveBeenCalledWith(endpoint, expect.anything())
    expect(
      vi.mocked(api.get).mock.calls.every(([url]) => url === endpoint)
    ).toBe(true)
  })

  it('imports a local full release, formats it, and exports the exact current draft without saving', async () => {
    await show()
    await editor()
    const user = userEvent.setup()
    const file = new File([editedRelease], 'release.json', {
      type: 'application/json',
    })
    Object.defineProperty(file, 'text', { value: async () => editedRelease })
    await user.upload(screen.getByLabelText('Import Release JSON'), file)
    await waitFor(() =>
      expect(screen.getByRole('textbox', { name: 'Release JSON' })).toHaveValue(
        editedRelease
      )
    )
    await user.click(screen.getByRole('button', { name: 'Format JSON' }))
    const formatted = JSON.stringify(JSON.parse(editedRelease), null, 2)
    expect(await editor()).toHaveValue(formatted)
    const createUrl = vi
      .spyOn(URL, 'createObjectURL')
      .mockReturnValue('blob:test-release')
    const revokeUrl = vi
      .spyOn(URL, 'revokeObjectURL')
      .mockImplementation(() => {})
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => {})
    await user.click(screen.getByRole('button', { name: 'Export JSON' }))
    expect(createUrl).toHaveBeenCalledWith(expect.any(Blob))
    expect(await (createUrl.mock.calls[0][0] as Blob).text()).toBe(formatted)
    expect(click).toHaveBeenCalledOnce()
    expect(revokeUrl).toHaveBeenCalledWith('blob:test-release')
    expect(api.put).not.toHaveBeenCalled()
    expect(api.post).not.toHaveBeenCalled()
  })

  it.each([
    {
      name: 'invalid JSON',
      text: async () => '{"broken":',
      message: 'Invalid JSON format',
    },
    {
      name: 'unreadable file',
      text: async () => {
        throw new Error('Read failed')
      },
      message: 'Could not read the Release JSON file',
    },
  ])(
    'preserves the existing draft when importing an $name',
    async ({ text, message }) => {
      await show()
      await editor()
      edit()
      const file = new File(['invalid'], 'release.json', {
        type: 'application/json',
      })
      Object.defineProperty(file, 'text', { value: text })
      await userEvent.upload(screen.getByLabelText('Import Release JSON'), file)
      expect(await screen.findByText(message)).toBeVisible()
      expect(await editor()).toHaveValue(editedRelease)
      expect(api.put).not.toHaveBeenCalled()
      expect(api.post).not.toHaveBeenCalled()
    }
  )

  it.each([
    '{"broken":',
    '{}',
    '{"schemaVersion":1,"revision":9007199254740992,"config":{}}',
  ])(
    'keeps invalid release input and blocks save and publish for %s',
    async (invalid) => {
      await show()
      await editor()
      edit(invalid)
      await userEvent.click(screen.getByRole('button', { name: 'Save draft' }))
      expect(await editor()).toHaveValue(invalid)
      expect(
        screen.getByRole('textbox', { name: 'Release JSON' })
      ).toHaveAttribute('aria-invalid', 'true')
      expect(api.put).not.toHaveBeenCalled()
      await userEvent.click(
        screen.getByRole('button', { name: 'Save and publish' })
      )
      expect(api.post).not.toHaveBeenCalled()
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    }
  )

  it('locks editing and actions until save completes and uses the returned snapshot', async () => {
    let resolve!: (value: Awaited<ReturnType<typeof api.put>>) => void
    vi.mocked(api.put).mockReturnValue(
      new Promise((done) => {
        resolve = done
      })
    )
    await show()
    await editor()
    edit()
    await userEvent.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith(endpoint, {
        expected_version: 7,
        draft_json: editedRelease,
      })
    )
    expect(await editor()).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Save and publish' })
    ).toBeDisabled()
    await act(async () =>
      resolve({
        data: {
          success: true,
          data: { ...snapshot, version: 8, draft_json: editedRelease },
        },
      })
    )
    expect(await editor()).not.toBeDisabled()
    expect(
      screen.getByText('Server version').nextElementSibling
    ).toHaveTextContent('8')
    expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled()
  })

  it('confirms publishing and waits for the dirty draft save before publishing its returned version', async () => {
    let resolve!: (value: Awaited<ReturnType<typeof api.put>>) => void
    vi.mocked(api.put).mockReturnValue(
      new Promise((done) => {
        resolve = done
      })
    )
    await show()
    await editor()
    edit()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Save and publish' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(api.put).not.toHaveBeenCalled()
    expect(api.post).not.toHaveBeenCalled()
    await user.click(
      within(dialog).getByRole('button', { name: 'Save and publish' })
    )
    await waitFor(() => expect(api.put).toHaveBeenCalledOnce())
    expect(api.post).not.toHaveBeenCalled()
    expect(
      within(dialog).getByRole('button', { name: 'Save and publish' })
    ).toBeDisabled()
    await act(async () =>
      resolve({
        data: {
          success: true,
          data: { ...snapshot, version: 8, draft_json: editedRelease },
        },
      })
    )
    await waitFor(() =>
      expect(api.post).toHaveBeenCalledWith(`${endpoint}/publish`, {
        expected_version: 8,
      })
    )
    expect(
      screen.getByText('Published revision').nextElementSibling
    ).toHaveTextContent('44')
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('does not publish an old draft when saving the edited draft is rejected', async () => {
    vi.mocked(api.put).mockResolvedValue({
      data: { success: false, message: 'Invalid parameter mapping' },
    })
    await show()
    await editor()
    edit()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Save and publish' }))
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Save and publish',
      })
    )
    expect(
      await screen.findAllByText('Invalid parameter mapping')
    ).not.toHaveLength(0)
    expect(await editor()).toHaveValue(editedRelease)
    expect(api.post).not.toHaveBeenCalled()
    expect(
      screen.getByText('Published revision').nextElementSibling
    ).toHaveTextContent('43')
  })

  it('keeps edits on a version conflict and requires confirmed refresh before retrying against the latest version', async () => {
    vi.mocked(api.put).mockRejectedValueOnce(conflict())
    await show()
    await editor()
    edit()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    expect(
      await screen.findByText('Configuration changed on the server')
    ).toBeVisible()
    expect(await editor()).toHaveValue(editedRelease)
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: { ...snapshot, version: 10 } },
    })
    await user.click(
      screen.getByRole('button', { name: 'Reload server state' })
    )
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Reload server state',
      })
    )
    await waitFor(() =>
      expect(
        screen.getByText('Server version').nextElementSibling
      ).toHaveTextContent('10')
    )
    expect(await editor()).toHaveValue(editedRelease)
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() =>
      expect(api.put).toHaveBeenLastCalledWith(endpoint, {
        expected_version: 10,
        draft_json: editedRelease,
      })
    )
  })

  it('preserves the saved draft and published snapshot when publishing fails', async () => {
    vi.mocked(api.post).mockRejectedValueOnce(
      new Error('Publishing is unavailable')
    )
    await show()
    await editor()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('button', { name: 'Publish saved draft' })
    )
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Publish saved draft',
      })
    )
    expect(
      await screen.findAllByText('Publishing is unavailable')
    ).not.toHaveLength(0)
    expect(await editor()).toHaveValue(release)
    expect(
      screen.getByText('Published revision').nextElementSibling
    ).toHaveTextContent('43')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('shows initial load failures with retry instead of an empty demonstration draft', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(
      new Error('Configuration is unavailable')
    )
    await show()
    expect(
      await screen.findByText('Configuration is unavailable')
    ).toBeVisible()
    expect(
      screen.queryByRole('textbox', { name: 'Release JSON' })
    ).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await editor()).toHaveValue(release)
  })

  it('blocks tab close and route navigation for unsaved edits and supports keyboard cancellation', async () => {
    const router = await show()
    await editor()
    edit()
    const close = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(close)
    expect(close.defaultPrevented).toBe(true)
    act(() => {
      void router.navigate({ to: '/403' })
    })
    const dialog = await screen.findByRole('alertdialog')
    within(dialog).getByRole('button', { name: 'Stay' }).focus()
    await userEvent.keyboard('{Enter}')
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(router.state.location.pathname).toBe('/')
    expect(await editor()).toHaveValue(editedRelease)
    await userEvent.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled()
    )
    const savedClose = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(savedClose)
    expect(savedClose.defaultPrevented).toBe(false)
    await act(async () => router.navigate({ to: '/403' }))
    expect(screen.getByText('Another settings page')).toBeVisible()
  })

  it('updates labels when switching between English and Chinese without replacing the draft', async () => {
    i18next.addResourceBundle('zh', 'translation', zh.translation)
    await show()
    await editor()
    edit()
    await act(async () => i18next.changeLanguage('zh'))
    expect(
      screen.getByRole('textbox', { name: zh.translation['Release JSON'] })
    ).toHaveValue(editedRelease)
    expect(
      screen.getByRole('button', { name: zh.translation['Save draft'] })
    ).toBeVisible()
    await act(async () => i18next.changeLanguage('en'))
    expect(await editor()).toHaveValue(editedRelease)
  })
})
