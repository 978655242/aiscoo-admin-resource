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
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { Hero } from '../components/sections/hero'

test('the homepage lists the three supported coding applications with official links and icons', async () => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
  const root = createRootRoute({ component: Hero })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(<RouterProvider router={router} />)

  const applications = [
    ['Claude Code', 'https://claude.com/product/claude-code'],
    ['Codex', 'https://openai.com/codex/'],
    ['Open Code', 'https://opencode.ai/'],
  ] as const

  for (const [name, url] of applications) {
    const link = await screen.findByRole('link', { name })
    expect(link).toHaveAttribute('href', url)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(link.querySelector('svg, img')).toBeInTheDocument()
  }
  expect(screen.queryByRole('link', { name: 'Cherry Studio' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'CC Switch' })).toBeNull()
  expect(screen.queryByText('More Apps')).toBeNull()
})
