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
import { useRef, type CSSProperties, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

interface GlowCardProps {
  children: ReactNode
  className?: string
  style?: CSSProperties
}

/**
 * Card with a cursor-following highlight and a diagonal sheen on hover,
 * the interaction used by the official site's scene cards.
 */
export function GlowCard(props: GlowCardProps) {
  const ref = useRef<HTMLDivElement>(null)

  const track = (event: React.PointerEvent<HTMLDivElement>) => {
    const el = ref.current
    if (!el) return
    const rect = el.getBoundingClientRect()
    el.style.setProperty('--mx', `${event.clientX - rect.left}px`)
    el.style.setProperty('--my', `${event.clientY - rect.top}px`)
  }

  return (
    <div
      ref={ref}
      onPointerMove={track}
      style={props.style}
      className={cn(
        'glow-card group relative overflow-hidden',
        props.className
      )}
    >
      {props.children}
    </div>
  )
}
