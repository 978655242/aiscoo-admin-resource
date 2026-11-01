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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { ReleaseCreateSheet } from '../components/release-create-sheet'
import type { DesktopReleaseCreate } from '../types'

const digest = btoa('\0'.repeat(64))
const manifest = `version: 1.2.3
files:
  - url: https://downloads.example.com/app.zip
    sha512: ${digest}
    size: 2048
`

function Harness(props: {
  onSubmit: (input: DesktopReleaseCreate) => Promise<void>
  closeOnSuccess?: boolean
}) {
  const [open, setOpen] = useState(true)
  return (
    <>
      <button type='button' onClick={() => setOpen(true)}>
        Open create sheet
      </button>
      <ReleaseCreateSheet
        open={open}
        onOpenChange={setOpen}
        onSubmit={async (input) => {
          await props.onSubmit(input)
          if (props.closeOnSuccess) setOpen(false)
        }}
      />
    </>
  )
}

async function importManifest(
  user: ReturnType<typeof userEvent.setup>,
  text = manifest
): Promise<void> {
  const file = new File([text], 'latest-mac.yml', { type: 'application/yaml' })
  // jsdom 的 File 未实现 text；只替换这个浏览器读取边界，解析仍运行真实路径。
  Object.defineProperty(file, 'text', { value: () => Promise.resolve(text) })
  await user.upload(screen.getByLabelText('Release manifest file'), file)
  await screen.findByRole('status')
}

describe('desktop release create sheet', () => {
  test('confirms a dirty close and resets all input after discarding', async () => {
    const user = userEvent.setup()
    render(<Harness onSubmit={vi.fn().mockResolvedValue(undefined)} />)
    await user.type(screen.getByRole('textbox', { name: 'Version' }), '1.2.3')
    await user.click(screen.getByRole('button', { name: 'Close' }))
    const confirmation = await screen.findByRole('alertdialog')
    await user.click(
      within(confirmation).getByRole('button', { name: 'Cancel' })
    )
    expect(screen.getByRole('textbox', { name: 'Version' })).toHaveValue(
      '1.2.3'
    )
    await user.click(screen.getByRole('button', { name: 'Close' }))
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Discard changes',
      })
    )
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Open create sheet' }))
    expect(await screen.findByRole('textbox', { name: 'Version' })).toHaveValue(
      ''
    )
  })

  test('keeps both notes languages when switching and previews safe Markdown', async () => {
    const user = userEvent.setup()
    render(<Harness onSubmit={vi.fn().mockResolvedValue(undefined)} />)
    const chinese = screen.getByRole('textbox', {
      name: 'Release notes (Simplified Chinese)',
    })
    await user.type(chinese, '中文说明')
    await user.click(screen.getByRole('tab', { name: 'English' }))
    const english = screen.getByRole('textbox', {
      name: 'Release notes (English)',
    })
    fireEvent.change(english, {
      target: { value: '**Safe notes** <script>alert(1)</script>' },
    })
    await user.click(screen.getByRole('button', { name: 'Preview' }))
    expect(screen.getByText('Safe notes')).toBeVisible()
    expect(document.querySelector('script')).toBeNull()
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    expect(
      screen.getByRole('textbox', { name: 'Release notes (English)' })
    ).toHaveValue('**Safe notes** <script>alert(1)</script>')
    await user.click(screen.getByRole('tab', { name: 'Simplified Chinese' }))
    expect(
      screen.getByRole('textbox', {
        name: 'Release notes (Simplified Chinese)',
      })
    ).toHaveValue('中文说明')
  })

  test('blocks invalid submit and labels the invalid version field', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<Harness onSubmit={onSubmit} />)
    await user.click(screen.getByRole('button', { name: 'Create release' }))
    expect(
      await screen.findByText(
        'Enter a version as X.Y.Z or X.Y.Z-prerelease without build metadata'
      )
    ).toBeVisible()
    expect(screen.getByRole('textbox', { name: 'Version' })).toHaveAttribute(
      'aria-invalid',
      'true'
    )
    expect(onSubmit).not.toHaveBeenCalled()
  })

  test('imports metadata locally without saving and reports relative URLs for completion', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<Harness onSubmit={onSubmit} />)
    await importManifest(
      user,
      manifest.replace('https://downloads.example.com/', '')
    )
    expect(screen.getByRole('textbox', { name: 'Download URL' })).toHaveValue(
      'app.zip'
    )
    expect(
      screen.getByText('Enter an HTTPS URL without credentials or a fragment')
    ).toBeVisible()
    expect(onSubmit).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Create release' }))
    expect(onSubmit).not.toHaveBeenCalled()
  })

  test('keeps inputs on API rejection and prevents close or duplicate submit while pending', async () => {
    const user = userEvent.setup()
    let rejectRequest: ((error: Error) => void) | undefined
    const onSubmit = vi.fn(
      (_input: DesktopReleaseCreate) =>
        new Promise<void>((_resolve, reject) => {
          rejectRequest = reject
        })
    )
    render(<Harness onSubmit={onSubmit} />)
    await importManifest(user)
    await user.click(screen.getByRole('button', { name: 'Create release' }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    expect(onSubmit.mock.calls[0]?.[0]).toMatchObject({
      platform: 'darwin-aarch64',
      status: 0,
      rollout_percentage: 0,
      rollout_disabled: false,
      device_mid_allowlist: [],
      release_date: 0,
    })
    const creating = screen.getByRole('button', {
      name: 'Creating...',
    })
    expect(creating).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Close' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: 'Version' })).toBeDisabled()
    await user.click(creating)
    await user.keyboard('{Escape}')
    expect(screen.getByRole('dialog')).toBeVisible()
    expect(onSubmit).toHaveBeenCalledTimes(1)
    if (!rejectRequest) {
      throw new Error('Expected an outstanding create request')
    }
    rejectRequest(new Error('This version already exists'))
    expect(await screen.findByText('This version already exists')).toBeVisible()
    expect(screen.getByRole('textbox', { name: 'Version' })).toHaveValue(
      '1.2.3'
    )
    expect(screen.getByRole('textbox', { name: 'Download URL' })).toHaveValue(
      'https://downloads.example.com/app.zip'
    )
    expect(screen.getByRole('button', { name: 'Create release' })).toBeEnabled()
  })

  test('lets the parent close after successful create and restores clean defaults on reopening', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<Harness onSubmit={onSubmit} closeOnSuccess />)
    await importManifest(user)
    await user.click(screen.getByRole('button', { name: 'Create release' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(onSubmit).toHaveBeenCalledTimes(1)
    await user.click(screen.getByRole('button', { name: 'Open create sheet' }))
    expect(await screen.findByRole('textbox', { name: 'Version' })).toHaveValue(
      ''
    )
    expect(
      screen.getByRole('textbox', { name: 'Minimum system version' })
    ).toHaveValue('22.0.0')
  })
})
