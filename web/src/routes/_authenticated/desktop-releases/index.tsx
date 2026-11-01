import { createFileRoute, redirect } from '@tanstack/react-router'

import { DesktopReleases } from '@/features/desktop-releases'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/desktop-releases/')({
  beforeLoad: () => {
    if (useAuthStore.getState().auth.user?.role !== ROLE.SUPER_ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: DesktopReleases,
})
