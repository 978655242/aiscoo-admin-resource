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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type {
  PlanRecord,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { api } from '@/lib/api'

import { Wallet } from '..'

const plans: PlanRecord[] = ['Trial', 'Lite', 'Pro', 'Max'].map(
  (title, index) => ({
    plan: {
      id: index + 1,
      title,
      subtitle: 'Model access for your workspace',
      price_amount: index * 10,
      currency: 'USD',
      duration_unit: 'month',
      duration_value: 1,
      quota_reset_period: 'weekly',
      enabled: true,
      sort_order: index,
      allow_balance_pay: true,
      allow_wallet_overflow: true,
      max_purchase_per_user: index === 0 ? 1 : 0,
      total_amount: 500000,
    },
  })
)

const topupInfo = {
  enable_online_topup: true,
  enable_stripe_topup: false,
  enable_redemption: true,
  pay_methods: [{ type: 'alipay', name: 'Alipay' }],
  min_topup: 1,
  stripe_min_topup: 1,
  amount_options: [10, 20, 50, 100],
  discount: {},
}

let quota = 50000000
let availablePlans: PlanRecord[] = plans
let redemptionEnabled = true
let subscriptions: UserSubscriptionRecord[] = []
let clients: QueryClient[] = []

beforeEach(() => {
  quota = 50000000
  availablePlans = plans
  redemptionEnabled = true
  subscriptions = []
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    let data: unknown
    switch (url) {
      case '/api/status':
        data = { price: 1 }
        break
      case '/api/user/self':
        data = { quota, used_quota: 500000, request_count: 1000 }
        break
      case '/api/user/topup/info':
        data = { ...topupInfo, enable_redemption: redemptionEnabled }
        break
      case '/api/user/aff':
        data = 'invite'
        break
      case '/api/subscription/plans':
        data = availablePlans
        break
      case '/api/subscription/self':
        data = {
          billing_preference: 'wallet_first',
          subscriptions: [],
          all_subscriptions: subscriptions,
        }
        break
      default:
        if (url.startsWith('/api/user/topup/self?')) {
          data = { total: 0, items: [] }
        } else {
          throw new Error(`Unexpected GET ${url}`)
        }
    }
    return { data: { success: true, data } }
  })
  vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/user/amount') {
      return { data: { success: true, data: '10' } }
    }
    throw new Error(`Unexpected POST ${url}`)
  })
})

afterEach(() => {
  for (const client of clients) client.clear()
  clients = []
  localStorage.clear()
})

function renderWallet() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <Wallet />
    </QueryClientProvider>
  )
}

describe('wallet balance actions', () => {
  it('keeps recharge and redemption forms hidden until their balance action is opened', async () => {
    const user = userEvent.setup()
    renderWallet()
    await screen.findByRole('heading', { name: 'Lite' })
    const balance = screen.getByRole('region', { name: 'Current Balance' })
    expect(
      within(balance).getByRole('button', { name: 'Recharge' })
    ).toBeEnabled()
    expect(
      screen.queryByRole('spinbutton', { name: 'Custom Amount' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('textbox', { name: 'Redemption Code' })
    ).not.toBeInTheDocument()

    const recharge = within(balance).getByRole('button', { name: 'Recharge' })
    recharge.focus()
    await user.keyboard('{Enter}')
    const dialog = await screen.findByRole('dialog', { name: 'Recharge' })
    expect(
      within(dialog).getByRole('spinbutton', { name: 'Custom Amount' })
    ).toBeVisible()
    expect(within(dialog).queryByRole('textbox')).not.toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(recharge).toHaveFocus()
  })

  it('returns to recharge when payment confirmation is cancelled', async () => {
    const user = userEvent.setup()
    renderWallet()
    await screen.findByRole('heading', { name: 'Lite' })
    await user.click(screen.getByRole('button', { name: 'Recharge' }))
    const recharge = await screen.findByRole('dialog', { name: 'Recharge' })
    await user.click(within(recharge).getByRole('button', { name: 'Alipay' }))
    const confirmation = await screen.findByRole('alertdialog')
    await user.click(
      within(confirmation).getByRole('button', { name: 'Cancel' })
    )
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(
      within(recharge).getByRole('spinbutton', { name: 'Custom Amount' })
    ).toBeVisible()
  })

  it('does not open payment confirmation when a quote arrives after recharge is closed', async () => {
    const user = userEvent.setup()
    renderWallet()
    await screen.findByRole('heading', { name: 'Lite' })
    await user.click(screen.getByRole('button', { name: 'Recharge' }))
    const recharge = await screen.findByRole('dialog', { name: 'Recharge' })
    let finishQuote!: (response: {
      data: { success: boolean; data: string }
    }) => void
    vi.mocked(api.post).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishQuote = resolve
        })
    )
    await user.click(within(recharge).getByRole('button', { name: 'Alipay' }))
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    await act(async () => {
      finishQuote({ data: { success: true, data: '10' } })
    })
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('opens order history from the balance area', async () => {
    const user = userEvent.setup()
    renderWallet()
    await screen.findByRole('heading', { name: 'Lite' })
    await user.click(screen.getByRole('button', { name: 'Order History' }))
    expect(
      await screen.findByRole('dialog', { name: 'Billing History' })
    ).toBeVisible()
  })

  it('preserves the entered code after a failed redemption and closes after success', async () => {
    const user = userEvent.setup()
    const post = vi.mocked(api.post)
    renderWallet()
    await screen.findByRole('heading', { name: 'Lite' })
    const redeem = screen.getByRole('button', { name: 'Redeem' })
    await user.click(redeem)
    const dialog = await screen.findByRole('dialog', { name: 'Redeem' })
    const input = within(dialog).getByRole('textbox', {
      name: 'Redemption Code',
    })
    const submit = within(dialog).getByRole('button', { name: 'Redeem' })
    expect(submit).toBeDisabled()
    await user.type(input, 'wallet-code')
    post.mockResolvedValueOnce({
      data: { success: false, message: 'Invalid code' },
    })
    await user.click(submit)
    expect(
      await within(dialog).findByRole('textbox', { name: 'Redemption Code' })
    ).toHaveValue('wallet-code')
    await waitFor(() => expect(submit).toBeEnabled())

    quota += 500000
    post.mockResolvedValueOnce({ data: { success: true, data: 500000 } })
    await user.click(submit)
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(post).toHaveBeenLastCalledWith('/api/user/topup', {
      key: 'wallet-code',
    })
    expect(redeem).toHaveFocus()
  })

  it('disables redemption when codes are disabled in the server configuration', async () => {
    redemptionEnabled = false
    renderWallet()
    await screen.findByRole('heading', { name: 'Lite' })
    expect(screen.getByRole('button', { name: 'Redeem' })).toBeDisabled()
  })
})

describe('wallet subscription catalog', () => {
  it('lays out four plans in responsive columns and aligns their action rows', async () => {
    renderWallet()
    const plan = await screen.findByRole('article', { name: 'Lite' })
    const grid = plan.parentElement
    expect(grid).toHaveClass(
      'grid-cols-1',
      '@min-[40rem]/plans:grid-cols-2',
      '@min-[64rem]/plans:grid-cols-4'
    )
    expect(screen.getAllByRole('article')).toHaveLength(4)
    expect(plan).toHaveClass('grid-rows-subgrid', 'row-span-5')
  })

  it.each([1, 2, 3])(
    'uses only the available %i columns when fewer than four plans exist',
    async (count) => {
      availablePlans = plans.slice(0, count)
      renderWallet()
      const plan = await screen.findByRole('article', { name: 'Trial' })
      const grid = plan.parentElement
      expect(screen.getAllByRole('article')).toHaveLength(count)
      expect(grid).not.toHaveClass('@min-[64rem]/plans:grid-cols-4')
      if (count === 1) {
        expect(grid).not.toHaveClass('@min-[40rem]/plans:grid-cols-2')
      }
      if (count === 3) {
        expect(grid).toHaveClass('@min-[64rem]/plans:grid-cols-3')
      }
    }
  )

  it('keeps a plan unavailable when its purchase limit has been reached', async () => {
    subscriptions = [
      {
        subscription: {
          id: 1,
          user_id: 1,
          plan_id: 1,
          status: 'expired',
          start_time: 0,
          end_time: 1,
          amount_total: 500000,
          amount_used: 500000,
        },
      },
    ]
    renderWallet()
    const plan = await screen.findByRole('article', { name: 'Trial' })
    expect(
      within(plan).getByRole('button', { name: 'Limit Reached' })
    ).toBeDisabled()
  })

  it('opens the existing purchase dialog for the chosen plan', async () => {
    const user = userEvent.setup()
    renderWallet()
    const plan = await screen.findByRole('article', { name: 'Pro' })
    await user.click(
      within(plan).getByRole('button', { name: 'Subscribe Now' })
    )
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Pro', { exact: true })).toBeVisible()
  })

  it('shows the page without a catalog when there are no plans or subscriptions', async () => {
    availablePlans = []
    renderWallet()
    await waitFor(() =>
      expect(screen.queryByText('Subscription Plans')).not.toBeInTheDocument()
    )
    expect(
      screen.getByRole('region', { name: 'Current Balance' })
    ).toBeVisible()
  })

  it('shows configured highlights and wraps long plan descriptions without truncating them', async () => {
    const description =
      'A detailed plan description with model access and workspace features'.repeat(
        3
      )
    availablePlans = [
      {
        plan: {
          ...plans[1].plan,
          title: description,
          xcode_display: {
            group_key: 'lite',
            group_title: 'Lite',
            offer_kind: 'standard',
            highlights: ['Weekly model quota'],
            features: ['Workspace access'],
          },
        },
      },
    ]
    renderWallet()
    const plan = await screen.findByRole('article', { name: description })
    expect(within(plan).getByText('Weekly model quota')).toBeVisible()
    expect(within(plan).getByText('Workspace access')).toBeVisible()
    expect(
      within(plan).getByRole('heading', { name: description })
    ).not.toHaveClass('truncate')
  })
})
