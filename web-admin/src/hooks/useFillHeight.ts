import { useEffect, useRef, useState } from 'react'

/** 测量容器剩余高度，用于让表格在剩余空间内滚动。 */
export default function useFillHeight(offset: number) {
  const ref = useRef<HTMLDivElement>(null)
  const [height, setHeight] = useState(240)

  useEffect(() => {
    const element = ref.current
    if (!element || typeof ResizeObserver === 'undefined') return

    const observer = new ResizeObserver((entries) => {
      const available = entries[0]?.contentRect.height
      if (available) {
        setHeight(Math.max(120, Math.floor(available - offset)))
      }
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [offset])

  return [ref, height] as const
}
