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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { Profile } from '..'
import { LanguagePreferencesCard } from '../components/language-preferences-card'
import { ProfileHeader } from '../components/profile-header'
import { NotificationTab } from '../components/tabs/notification-tab'
import type { UserProfile } from '../types'

const profile: UserProfile = {
  id: 13,
  username: 'alice',
  display_name: 'Alice',
  email: 'alice@example.com',
  role: 1,
  group: 'default',
  quota: 50000000,
  used_quota: 500000,
  request_count: 1200,
  status: 1,
  aff_count: 0,
  aff_quota: 0,
  aff_history_quota: 0,
  created_time: 0,
  setting: JSON.stringify({ notify_type: 'email', language: 'en' }),
}

let client: QueryClient
let checkinEnabled = false

beforeEach(() => {
  checkinEnabled = false
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/self') {
      return { data: { success: true, data: profile } }
    }
    if (url === '/api/status') {
      return {
        data: { success: true, data: { checkin_enabled: checkinEnabled } },
      }
    }
    if (url.startsWith('/api/user/checkin')) {
      return {
        data: {
          success: true,
          data: { stats: { records: [], checked_in_today: false } },
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})

afterEach(() => {
  client.clear()
  localStorage.clear()
})

function renderProfile() {
  client.setQueryData(['status'], { checkin_enabled: checkinEnabled })
  return render(
    <QueryClientProvider client={client}>
      <Profile />
    </QueryClientProvider>
  )
}

describe('compact profile layout', () => {
  it('fills the shared page width without an extra centered width cap', async () => {
    renderProfile()
    const summary = await screen.findByRole('region', { name: 'Profile' })
    const content = summary.parentElement
    expect(content).toHaveClass('w-full')
    expect(content?.className).not.toMatch(/\bmax-w-/)
    expect(content).not.toHaveClass('mx-auto')
  })

  it('keeps the account summary first and language preferences in a responsive side column', async () => {
    renderProfile()
    const summary = await screen.findByRole('region', { name: 'Profile' })
    expect(
      within(summary).getByRole('heading', { name: 'Alice' })
    ).toBeVisible()
    expect(within(summary).getByText('alice@example.com')).toBeVisible()
    const preferences = screen.getByRole('complementary', {
      name: 'Preferences',
    })
    expect(
      within(preferences).getByRole('combobox', { name: 'Interface Language' })
    ).toBeVisible()
    const layout = preferences.parentElement
    expect(layout).toHaveClass(
      'grid-cols-1',
      '@min-[52rem]/profile:grid-cols-[minmax(0,1fr)_18rem]'
    )
    expect(screen.getByRole('button', { name: 'Save Settings' })).toBeVisible()
    for (const label of ['Current Balance', 'Total Usage', 'API Requests']) {
      expect(screen.queryByText(label)).not.toBeInTheDocument()
    }
  })

  it('keeps long account names and email readable instead of truncating them', () => {
    const name = 'A long account display name that should remain fully readable'
    const email = 'a-very-long-account-address@example-with-a-long-name.com'
    render(
      <ProfileHeader
        profile={{ ...profile, display_name: name, email }}
        loading={false}
      />
    )
    expect(screen.getByRole('heading', { name })).not.toHaveClass('truncate')
    expect(screen.getByText(email)).not.toHaveClass('truncate')
  })

  it('lets the user copy their account ID using the keyboard', async () => {
    const user = userEvent.setup()
    const copy = vi.spyOn(navigator.clipboard, 'writeText')
    render(<ProfileHeader profile={profile} loading={false} />)
    const button = screen.getByRole('button', { name: 'User ID' })
    button.focus()
    await user.keyboard('{Enter}')
    await waitFor(() => expect(copy).toHaveBeenCalledWith('13'))
  })

  it('shows only the account summary skeleton during loading', () => {
    render(<ProfileHeader profile={null} loading />)
    const summary = screen.getByRole('region', { name: 'Profile' })
    expect(summary).toHaveAttribute('aria-busy', 'true')
    expect(within(summary).queryByRole('term')).not.toBeInTheDocument()
    for (const label of ['Current Balance', 'Total Usage', 'API Requests']) {
      expect(within(summary).queryByText(label)).not.toBeInTheDocument()
    }
  })

  it('keeps optional daily check-in in the preferences column', async () => {
    checkinEnabled = true
    renderProfile()
    const preferences = await screen.findByRole('complementary', {
      name: 'Preferences',
    })
    expect(await within(preferences).findByText('Daily Check-in')).toBeVisible()
    const checkin = within(preferences).getByRole('button', {
      name: 'Check in now',
    })
    expect(checkin).toHaveClass('w-full', '@min-[28rem]/checkin:w-auto')
    expect(checkin.parentElement).toHaveClass('@min-[28rem]/checkin:flex-row')
    expect(checkin.parentElement).not.toHaveClass('sm:flex-row')
    const toggle = within(preferences).getByRole('button', {
      name: /^Daily Check-in/,
    })
    expect(toggle).toHaveClass(
      'min-w-0',
      'flex-1',
      'shrink',
      'whitespace-normal'
    )
    expect(toggle).not.toHaveClass('shrink-0')
  })
})

describe('language preferences', () => {
  it.each([true, false])(
    'saves or rolls back a language selection when success is %s',
    async (success) => {
      const user = userEvent.setup()
      const i18n = createInstance()
      await i18n.use(initReactI18next).init({
        lng: 'en',
        fallbackLng: 'en',
        resources: { en: { translation: {} }, fr: { translation: {} } },
      })
      const put = vi.spyOn(api, 'put').mockResolvedValue({
        data: { success, message: success ? '' : 'Unavailable' },
      })
      const onUpdate = vi.fn()
      render(
        <I18nextProvider i18n={i18n}>
          <LanguagePreferencesCard
            profile={profile}
            onProfileUpdate={onUpdate}
          />
        </I18nextProvider>
      )
      const language = screen.getByRole('combobox', {
        name: 'Interface Language',
      })
      await user.click(language)
      await user.click(await screen.findByRole('option', { name: 'Français' }))
      await waitFor(() => expect(language).not.toBeDisabled())
      expect(put).toHaveBeenCalledWith('/api/user/self', { language: 'fr' })
      expect(language).toHaveValue(success ? 'Français' : 'English')
      expect(i18n.language).toBe(success ? 'fr' : 'en')
      expect(onUpdate).toHaveBeenCalledTimes(success ? 1 : 0)
    }
  )
})

describe('compact notification controls', () => {
  it('switches notification methods without losing entered values and saves only on request', async () => {
    const user = userEvent.setup()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const onUpdate = vi.fn()
    render(
      <NotificationTab
        profile={profile}
        onUpdate={onUpdate}
        quotaRemindEnabled
        quotaRemindThreshold={1000}
      />
    )
    const methods = screen.getByRole('group', { name: 'Notification Method' })
    expect(
      within(methods).getByRole('button', { name: 'Webhook' })
    ).toHaveClass('min-h-9')
    expect(
      within(methods).getByRole('button', { name: 'Webhook' })
    ).not.toHaveClass('flex-col')
    await user.click(within(methods).getByRole('button', { name: 'Webhook' }))
    const url = screen.getByRole('textbox', { name: 'Webhook URL' })
    await user.type(url, 'https://example.com/notifications')
    await user.click(within(methods).getByRole('button', { name: 'Bark' }))
    expect(
      screen.queryByRole('textbox', { name: 'Webhook URL' })
    ).not.toBeInTheDocument()
    await user.click(within(methods).getByRole('button', { name: 'Webhook' }))
    expect(screen.getByRole('textbox', { name: 'Webhook URL' })).toHaveValue(
      'https://example.com/notifications'
    )
    expect(put).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(onUpdate).toHaveBeenCalledOnce())
    expect(put).toHaveBeenCalledWith(
      '/api/user/setting',
      expect.objectContaining({
        notify_type: 'webhook',
        webhook_url: 'https://example.com/notifications',
      })
    )
  })

  it('expands Gotify setup instructions using the keyboard', async () => {
    const user = userEvent.setup()
    render(
      <NotificationTab
        profile={profile}
        onUpdate={vi.fn()}
        quotaRemindEnabled
        quotaRemindThreshold={1000}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Gotify' }))
    const instructions = screen.getByRole('button', {
      name: 'Setup Instructions',
    })
    expect(instructions).toHaveAttribute('aria-expanded', 'false')
    instructions.focus()
    await user.keyboard('{Enter}')
    expect(instructions).toHaveAttribute('aria-expanded', 'true')
    expect(
      screen.getByText('1. Create an application in your Gotify server')
    ).toBeVisible()
    expect(
      screen.getByRole('link', { name: 'Gotify Documentation' })
    ).toHaveAttribute('href', 'https://gotify.net/')
  })

  it('keeps the reminder threshold toggle keyboard accessible', async () => {
    const user = userEvent.setup()
    render(
      <NotificationTab
        profile={profile}
        onUpdate={vi.fn()}
        quotaRemindEnabled
        quotaRemindThreshold={1000}
      />
    )
    const reminder = screen.getByRole('switch', {
      name: 'Low balance reminder',
    })
    reminder.focus()
    await user.keyboard(' ')
    expect(reminder).not.toBeChecked()
    expect(
      screen.queryByRole('spinbutton', { name: 'Reminder threshold (USD)' })
    ).not.toBeInTheDocument()
  })
})
