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
import { SidebarTrigger } from '@/components/ui/sidebar'

import { PublicHeader } from './public-header'

/**
 * Console top bar. Reuses the public site header so the logo, centered
 * navigation and right-side actions look the same before and after sign-in.
 * The sidebar trigger is only shown on mobile, where the sidebar is a sheet.
 */
export function AppHeader() {
  return (
    <>
      <PublicHeader
        fullWidth
        leftContent={
          <SidebarTrigger variant='ghost' className='size-8 md:hidden' />
        }
      />
      {/* PublicHeader is fixed; reserve its height in the layout flow. */}
      <div aria-hidden className='h-[var(--app-header-height)] shrink-0' />
    </>
  )
}
