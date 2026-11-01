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
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { ReleaseDetailSheet } from '../components/release-detail-sheet'
import { ReleaseOperationDialog } from '../components/release-operation-dialog'
import type { DesktopRelease, ReleaseAction } from '../types'

function release(overrides: Partial<DesktopRelease> = {}): DesktopRelease {
  return {
    id: 12,
    version: '2.0.0',
    platform: 'darwin-aarch64',
    status: 0,
    rollout_percentage: 10,
    rollout_disabled: false,
    device_mid_allowlist: [],
    minimum_system_version: '22.0.0',
    release_date: 1793491200,
    release_notes_by_locale: { 'zh-CN': '修复更新', 'en-US': 'Update fixes' },
    files: [
      {
        url: 'https://cdn.example.test/app.zip',
        sha512: 'a'.repeat(88),
        size: 1024,
      },
    ],
    created_at: 1793491200,
    updated_at: 1793491200,
    ...overrides,
  }
}

function operation(
  target: DesktopRelease,
  action: ReleaseAction,
  releases: DesktopRelease[] = [target]
) {
  const onSubmit = vi
    .fn<(id: number, patch: object) => Promise<void>>()
    .mockResolvedValue(undefined)
  const onClose = vi.fn()
  const view = render(
    <ReleaseOperationDialog
      release={target}
      action={action}
      releases={releases}
      onClose={onClose}
      onSubmit={onSubmit}
    />
  )
  return { ...view, onSubmit, onClose }
}

describe('release rollout editing', () => {
  test('starting a draft supports allowlist-only testing and submits normalized unique MIDs', async () => {
    const user = userEvent.setup()
    const target = release()
    const { onSubmit, onClose } = operation(target, 'rollout')
    expect(
      screen.getByRole('heading', { name: 'Start preview rollout' })
    ).toBeVisible()
    expect(
      screen.getByRole('spinbutton', { name: 'Rollout percentage' })
    ).toHaveValue(10)
    await user.click(screen.getByRole('button', { name: '0%' }))
    expect(screen.getByRole('button', { name: '0%' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByText(/Only allowlisted devices/)).toBeVisible()
    fireEvent.change(
      screen.getByRole('textbox', { name: 'Device MID allowlist' }),
      {
        target: { value: ' device-1 \n\ndevice-1\n Device-2 ' },
      }
    )
    expect(screen.getByText('Allowlisted devices: 2 / 200')).toBeVisible()
    expect(screen.getByText(/Only preview-channel devices/)).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Start preview rollout' })
    )
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(12, {
        status: 3,
        rollout_disabled: false,
        rollout_percentage: 0,
        device_mid_allowlist: ['device-1', 'Device-2'],
      })
    )
    expect(onClose).not.toHaveBeenCalled()
  })

  test('changing only the percentage of a paused rollout preserves pause and omits the allowlist', async () => {
    const user = userEvent.setup()
    const target = release({
      status: 3,
      rollout_disabled: true,
      device_mid_allowlist: ['device-1'],
    })
    const { onSubmit } = operation(target, 'rollout')
    expect(screen.getByText(/Saving keeps this rollout paused/)).toBeVisible()
    await user.click(screen.getByRole('button', { name: '25%' }))
    await user.click(screen.getByRole('button', { name: 'Save rollout' }))
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(12, {
        rollout_percentage: 25,
      })
    )
  })

  test.each([false, true])(
    'changing only the allowlist with paused=%s omits the percentage and distribution state',
    async (paused) => {
      const user = userEvent.setup()
      const { onSubmit } = operation(
        release({
          status: 3,
          rollout_disabled: paused,
          rollout_percentage: 25,
          device_mid_allowlist: ['device-1'],
        }),
        'rollout'
      )
      fireEvent.change(
        screen.getByRole('textbox', { name: 'Device MID allowlist' }),
        {
          target: { value: ' device-2 \ndevice-2\n device-3 ' },
        }
      )
      await user.click(screen.getByRole('button', { name: 'Save rollout' }))
      await waitFor(() =>
        expect(onSubmit).toHaveBeenCalledWith(12, {
          device_mid_allowlist: ['device-2', 'device-3'],
        })
      )
    }
  )

  test('changing only the active rollout percentage omits the allowlist', async () => {
    const user = userEvent.setup()
    const { onSubmit } = operation(
      release({ status: 3, device_mid_allowlist: ['device-1'] }),
      'rollout'
    )
    await user.click(screen.getByRole('button', { name: '50%' }))
    await user.click(screen.getByRole('button', { name: 'Save rollout' }))
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(12, { rollout_percentage: 50 })
    )
  })

  test('an unchanged rollout disables save, and reverting a percentage edit disables it again', async () => {
    const user = userEvent.setup()
    const { onSubmit } = operation(release({ status: 3 }), 'rollout')
    const save = screen.getByRole('button', { name: 'Save rollout' })
    expect(save).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '50%' }))
    expect(save).toBeEnabled()
    await user.click(screen.getByRole('button', { name: '10%' }))
    expect(save).toBeDisabled()
    await user.click(save)
    await user.click(
      screen.getByRole('spinbutton', { name: 'Rollout percentage' })
    )
    await user.keyboard('{Enter}')
    expect(onSubmit).not.toHaveBeenCalled()
  })

  test('starting an unchanged draft sends its existing scope and explicitly enables preview distribution', async () => {
    const user = userEvent.setup()
    const { onSubmit } = operation(
      release({ rollout_disabled: true, device_mid_allowlist: ['device-1'] }),
      'rollout'
    )
    const start = screen.getByRole('button', { name: 'Start preview rollout' })
    expect(start).toBeEnabled()
    await user.click(start)
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(12, {
        status: 3,
        rollout_disabled: false,
        rollout_percentage: 10,
        device_mid_allowlist: ['device-1'],
      })
    )
  })

  test.each(['-1', '101', '25.5'])(
    'percentage %s is rejected as outside the integer 0–100 contract',
    async (value) => {
      const user = userEvent.setup()
      const { onSubmit } = operation(release({ status: 3 }), 'rollout')
      fireEvent.change(
        screen.getByRole('spinbutton', { name: 'Rollout percentage' }),
        { target: { value } }
      )
      await user.click(screen.getByRole('button', { name: 'Save rollout' }))
      expect(
        await screen.findByText('Enter a whole percentage from 0 to 100.')
      ).toBeVisible()
      expect(
        screen.getByRole('spinbutton', { name: 'Rollout percentage' })
      ).toHaveAttribute('aria-invalid', 'true')
      expect(onSubmit).not.toHaveBeenCalled()
    }
  )

  test.each([
    ['bad_mid', 'Each MID must contain 1–64 letters, digits or hyphens.'],
    ['a'.repeat(65), 'Each MID must contain 1–64 letters, digits or hyphens.'],
    [
      Array.from({ length: 201 }, (_, index) => `device-${index}`).join('\n'),
      'Allow at most 200 unique device MIDs.',
    ],
  ])(
    'invalid allowlist is shown inline and cannot be submitted',
    async (value, message) => {
      const user = userEvent.setup()
      const { onSubmit } = operation(release({ status: 3 }), 'rollout')
      fireEvent.change(
        screen.getByRole('textbox', { name: 'Device MID allowlist' }),
        { target: { value } }
      )
      await user.click(screen.getByRole('button', { name: 'Save rollout' }))
      expect(await screen.findByText(message)).toBeVisible()
      expect(
        screen.getByRole('textbox', { name: 'Device MID allowlist' })
      ).toHaveAttribute('aria-invalid', 'true')
      expect(onSubmit).not.toHaveBeenCalled()
    }
  )

  test('dirty close asks before discarding and cancelling keeps the edited scope', async () => {
    const user = userEvent.setup()
    const { onClose } = operation(release({ status: 3 }), 'rollout')
    await user.click(screen.getByRole('button', { name: '50%' }))
    await user.click(screen.getByRole('button', { name: 'Close' }))
    const prompt = await screen.findByRole('alertdialog', {
      name: 'Discard unsaved changes?',
    })
    expect(onClose).not.toHaveBeenCalled()
    await user.click(
      within(prompt).getByRole('button', { name: 'Keep editing' })
    )
    expect(
      screen.getByRole('spinbutton', { name: 'Rollout percentage' })
    ).toHaveValue(50)
    await user.keyboard('{Escape}')
    const nextPrompt = await screen.findByRole('alertdialog', {
      name: 'Discard unsaved changes?',
    })
    await user.click(
      within(nextPrompt).getByRole('button', { name: 'Discard changes' })
    )
    expect(onClose).toHaveBeenCalledOnce()
  })

  test('pending submission blocks repeated saves and close; rejection preserves values and its precise error', async () => {
    const user = userEvent.setup()
    let rejectSave: (reason: Error) => void = () => undefined
    const request = new Promise<void>((_resolve, reject) => {
      rejectSave = reject
    })
    const { onSubmit, onClose } = operation(release({ status: 3 }), 'rollout')
    onSubmit.mockReturnValueOnce(request)
    await user.click(screen.getByRole('button', { name: '50%' }))
    await user.dblClick(screen.getByRole('button', { name: 'Save rollout' }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    expect(
      screen.getByRole('spinbutton', { name: 'Rollout percentage' })
    ).toBeDisabled()
    await user.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()
    await act(async () =>
      rejectSave(
        new Error('Preview configuration was changed by another administrator.')
      )
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Preview configuration was changed by another administrator.'
    )
    expect(
      screen.getByRole('spinbutton', { name: 'Rollout percentage' })
    ).toHaveValue(50)
    await user.click(screen.getByRole('button', { name: 'Save rollout' }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2))
  })
})

describe('release action confirmations', () => {
  test.each([
    {
      action: 'publish' as const,
      target: release(),
      button: 'Publish 2.0.0 to stable',
      patch: { status: 1, rollout_disabled: false },
    },
    {
      action: 'pause' as const,
      target: release({ status: 3 }),
      button: 'Pause 2.0.0 rollout',
      patch: { rollout_disabled: true },
    },
    {
      action: 'resume' as const,
      target: release({ status: 3, rollout_disabled: true }),
      button: 'Resume 2.0.0 rollout',
      patch: { rollout_disabled: false },
    },
    {
      action: 'withdraw' as const,
      target: release({ status: 1 }),
      button: 'Stop distributing 2.0.0',
      patch: { status: 0 },
    },
  ])(
    '$action shows release context and submits its minimal patch',
    async ({ action, target, button, patch }) => {
      const user = userEvent.setup()
      const { onSubmit } = operation(target, action)
      const dialog = screen.getByRole('alertdialog')
      expect(within(dialog).getAllByText('2.0.0')[0]).toBeVisible()
      expect(within(dialog).getByText('darwin-aarch64')).toBeVisible()
      await user.click(within(dialog).getByRole('button', { name: button }))
      await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(12, patch))
    }
  )

  test('publishing an older version explains why the current stable version and installed versions do not downgrade', () => {
    const target = release({ version: '1.0.0' })
    operation(target, 'publish', [
      target,
      release({ id: 20, version: '3.0.0', status: 1 }),
    ])
    expect(
      screen.getByText(
        /will not change the current stable release or downgrade installed versions/
      )
    ).toBeVisible()
    expect(screen.getByText('Current stable release')).toBeVisible()
    expect(screen.getByText('After this action')).toBeVisible()
    expect(screen.getAllByText('3.0.0')).toHaveLength(2)
  })

  test('withdrawing the last stable version explains that future stable requests have no release and existing installations stay', () => {
    operation(release({ status: 1 }), 'withdraw')
    expect(
      screen.getByText(/No stable release will be available/)
    ).toBeVisible()
    expect(
      screen.getByText(/Existing installations keep their installed version/)
    ).toBeVisible()
  })

  test('pause includes allowlisted devices and resume keeps the previous percentage and allowlist', () => {
    const view = operation(
      release({
        status: 3,
        rollout_percentage: 25,
        device_mid_allowlist: ['device-1'],
      }),
      'pause'
    )
    expect(screen.getByText(/including allowlisted devices/)).toBeVisible()
    view.unmount()
    operation(
      release({
        status: 3,
        rollout_disabled: true,
        rollout_percentage: 25,
        device_mid_allowlist: ['device-1'],
      }),
      'resume'
    )
    expect(
      screen.getByText(/Keeps the existing percentage and allowlist/)
    ).toBeVisible()
    expect(screen.getByText(/25%/)).toBeVisible()
  })

  test('confirmation failure keeps the dialog open with the server error and allows retry', async () => {
    const user = userEvent.setup()
    const { onSubmit, onClose } = operation(release(), 'publish')
    onSubmit.mockRejectedValueOnce(new Error('Artifact validation failed.'))
    await user.click(
      screen.getByRole('button', { name: 'Publish 2.0.0 to stable' })
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Artifact validation failed.'
    )
    expect(onClose).not.toHaveBeenCalled()
    await user.click(
      screen.getByRole('button', { name: 'Publish 2.0.0 to stable' })
    )
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2))
  })
})

describe('release notes editing', () => {
  test('notes editing explains its scope and protects changed notes on close', async () => {
    const user = userEvent.setup()
    const { onClose } = operation(release(), 'notes')
    expect(
      screen.getByText(/Changes only the bilingual release notes/)
    ).toBeVisible()
    const editor = screen.getAllByRole('textbox')[0]
    fireEvent.change(editor, { target: { value: '修复下载问题' } })
    await user.click(screen.getByRole('button', { name: 'Close' }))
    const prompt = await screen.findByRole('alertdialog', {
      name: 'Discard unsaved changes?',
    })
    expect(onClose).not.toHaveBeenCalled()
    await user.click(
      within(prompt).getByRole('button', { name: 'Keep editing' })
    )
    expect(editor).toHaveValue('修复下载问题')
  })

  test('notes save patches only notes and retains edits after an API rejection', async () => {
    const user = userEvent.setup()
    const { onSubmit } = operation(release(), 'notes')
    onSubmit.mockRejectedValueOnce(
      new Error('Release notes could not be saved.')
    )
    const editor = screen.getAllByRole('textbox')[0]
    fireEvent.change(editor, { target: { value: '修复下载问题' } })
    await user.click(screen.getByRole('button', { name: 'Save release notes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Release notes could not be saved.'
    )
    expect(editor).toHaveValue('修复下载问题')
    expect(onSubmit).toHaveBeenCalledWith(12, {
      release_notes_by_locale: {
        'zh-CN': '修复下载问题',
        'en-US': 'Update fixes',
      },
    })
  })
})

describe('release detail sheet', () => {
  test('shows stable history and Darwin requirement with a fixed frame and scrollable tabs', async () => {
    const user = userEvent.setup()
    render(
      <ReleaseDetailSheet
        release={release({ status: 1 })}
        currentStableId={99}
        onOpenChange={vi.fn()}
        onAction={vi.fn()}
      />
    )
    const sheet = screen.getByRole('dialog')
    expect(sheet).toHaveClass('w-full', 'overflow-hidden', 'sm:max-w-[780px]')
    expect(screen.getByText('Historical stable release')).toBeVisible()
    expect(screen.getByText('Minimum Darwin kernel version')).toBeVisible()
    expect(screen.getByRole('tabpanel')).toHaveClass(
      'overflow-y-auto',
      'min-w-0'
    )
    await user.click(screen.getByRole('tab', { name: 'Release notes' }))
    expect(screen.getByRole('tab', { name: 'Release notes' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByText('Simplified Chinese')).toBeVisible()
    expect(screen.getByText('English')).toBeVisible()
    expect(screen.getByText('修复更新')).toBeVisible()
    expect(screen.getByText('Update fixes')).toBeVisible()
  })

  test('artifacts preserve configured order, wrap long URLs, mark the primary file and use clipboard controls', async () => {
    const user = userEvent.setup()
    const url = `https://cdn.example.test/${'x'.repeat(240)}/primary.zip`
    render(
      <ReleaseDetailSheet
        release={release({
          files: [
            { url, sha512: 'a'.repeat(88), size: 2048, position: 0 },
            {
              url: 'https://cdn.example.test/second.dmg',
              sha512: 'b'.repeat(88),
              size: 1024,
              position: 1,
            },
          ],
        })}
        onOpenChange={vi.fn()}
        onAction={vi.fn()}
      />
    )
    await user.click(screen.getByRole('tab', { name: 'Artifacts' }))
    const artifacts = screen.getAllByRole('listitem')
    expect(within(artifacts[0]).getByText('Primary file')).toBeVisible()
    expect(within(artifacts[0]).getByText(url)).toHaveClass('break-all')
    expect(
      within(artifacts[1]).getByText('https://cdn.example.test/second.dmg')
    ).toBeVisible()
    await user.click(
      within(artifacts[0]).getByRole('button', { name: 'Copy artifact URL' })
    )
    expect(await navigator.clipboard.readText()).toBe(url)
    expect(
      within(artifacts[0]).getByRole('button', { name: 'Copy SHA-512' })
    ).toBeVisible()
  })

  test('primary action delegates to the parent and disabled detail prevents mutation actions', async () => {
    const user = userEvent.setup()
    const target = release({ status: 3 })
    const onAction = vi.fn()
    const view = render(
      <ReleaseDetailSheet
        release={target}
        onOpenChange={vi.fn()}
        onAction={onAction}
      />
    )
    await user.click(
      screen.getByRole('button', { name: 'Publish stable release' })
    )
    expect(onAction).toHaveBeenCalledWith(target, 'publish')
    view.rerender(
      <ReleaseDetailSheet
        release={target}
        disabled
        onOpenChange={vi.fn()}
        onAction={onAction}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Publish stable release' })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Actions for version 2.0.0' })
    ).toBeDisabled()
  })
})
