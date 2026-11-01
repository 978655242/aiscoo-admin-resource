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
import { useEffect, useRef } from 'react'

const SPACING = 11.5
const COLUMNS = 126
const WIDTH = COLUMNS * SPACING
const HEIGHT = 420
const DOT_SIZE = 3.1
const MAX_ROWS = 48

const ROW_COLORS = Array.from({ length: MAX_ROWS }, (_, row) => {
  const mix = Math.min(1, row / 30)
  return `rgb(${Math.round(199 - mix * 157)}, ${Math.round(250 - mix * 65)}, ${Math.round(251 - mix * 41)})`
})

const hash = (value: number) => {
  const v = Math.sin(value * 127.1 + 311.7) * 43758.5453123
  return v - Math.floor(v)
}

const columns = Array.from({ length: COLUMNS }, (_, index) => {
  const center = Math.abs(index / (COLUMNS - 1) - 0.5) * 2
  return {
    index,
    baseline: HEIGHT - 22 - center * 32 + Math.sin(index * 0.17) * 9,
    resting: Math.round(
      7 +
        18 * Math.exp(-(((index - 61) / 32) ** 2)) +
        Math.sin(index * 0.21) * 4 +
        hash(index) * 5
    ),
  }
})

// Deterministic "speech" rhythm: phrases of 1.6-2.8s separated by short pauses.
const phases: {
  start: number
  phrase: number
  duration: number
  seed: number
}[] = []
let cycleDuration = 0
for (let index = 0; index < 16; index++) {
  const phrase = 1.6 + hash(index + 50) * 1.2
  const duration = phrase + 0.2 + hash(index + 80) * 0.3
  phases.push({ start: cycleDuration, phrase, duration, seed: index })
  cycleDuration += duration
}

function voiceAt(time: number) {
  if (time < 1.1) return 0
  const cycle = (time - 1.1) % cycleDuration
  const phase = phases.find((part) => cycle < part.start + part.duration)
  if (!phase) return 0
  const local = cycle - phase.start
  if (local > phase.phrase) return 0
  const envelope = Math.min(1, local / 0.1, (phase.phrase - local) / 0.14)
  const syllable = local * 6
  const syllableIndex = Math.floor(syllable)
  const burst = Math.sin(Math.PI * (syllable - syllableIndex)) ** 1.7
  return Math.max(
    0,
    envelope * burst * (0.4 + 0.6 * hash(syllableIndex + phase.seed * 37))
  )
}

/**
 * Dotted audio-equalizer horizon from the official site hero. Columns of
 * square dots rise and fall in time with a looping synthetic voice pattern.
 */
export function HeroEqualizer(props: { className?: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    const context = canvas?.getContext('2d')
    if (!canvas || !context) return

    const reduceMotion = window.matchMedia(
      '(prefers-reduced-motion: reduce)'
    ).matches
    const start = performance.now()
    let frame = 0

    const paint = (time: number) => {
      const dpr = window.devicePixelRatio || 1
      const scale = canvas.clientWidth / WIDTH
      context.setTransform(1, 0, 0, 1, 0, 0)
      context.clearRect(0, 0, canvas.width, canvas.height)
      context.setTransform(
        scale * dpr,
        0,
        0,
        scale * dpr,
        0,
        canvas.height - HEIGHT * scale * dpr
      )
      context.globalAlpha = 0.75
      for (const column of columns) {
        const delay = Math.abs(column.index - 66) * 0.007
        const band = 0.54 + 0.46 * Math.sin(time * 1.43 + column.index * 0.21)
        const level = voiceAt(Math.max(0, time - delay))
        const visible =
          time < 1.1
            ? column.resting
            : Math.max(
                2,
                Math.min(
                  column.resting + 4,
                  column.resting - 4 + Math.round(level * (6 + band * 5))
                )
              )
        for (let row = 0; row < visible; row++) {
          context.fillStyle = ROW_COLORS[Math.min(row, MAX_ROWS - 1)]
          context.fillRect(
            column.index * SPACING,
            column.baseline - row * SPACING,
            DOT_SIZE,
            DOT_SIZE
          )
        }
      }
    }

    const resize = () => {
      const dpr = window.devicePixelRatio || 1
      canvas.width = Math.round(canvas.clientWidth * dpr)
      canvas.height = Math.round(canvas.clientHeight * dpr)
      paint(reduceMotion ? 0 : (performance.now() - start) / 1000)
    }

    const tick = () => {
      if (document.hidden) {
        frame = 0
        return
      }
      paint((performance.now() - start) / 1000)
      frame = requestAnimationFrame(tick)
    }

    const resumeWhenVisible = () => {
      if (!document.hidden && !frame && !reduceMotion) {
        frame = requestAnimationFrame(tick)
      }
    }

    const observer = new ResizeObserver(resize)
    observer.observe(canvas)
    resize()
    document.addEventListener('visibilitychange', resumeWhenVisible)
    if (!reduceMotion) frame = requestAnimationFrame(tick)

    return () => {
      observer.disconnect()
      document.removeEventListener('visibilitychange', resumeWhenVisible)
      cancelAnimationFrame(frame)
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      aria-hidden
      className={props.className}
      style={{
        maskImage: 'linear-gradient(transparent 8%, #000 42%)',
        WebkitMaskImage: 'linear-gradient(transparent 8%, #000 42%)',
      }}
    />
  )
}
