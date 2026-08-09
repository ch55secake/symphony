import type { TextRenderable } from "@opentui/core"
import { useTimeline } from "@opentui/react"
import { useEffect, useRef } from "react"
import type { Theme } from "../theme/tokens"

function PulseDot({ theme, animate }: { theme: Theme; animate: boolean }) {
  const dot = useRef<TextRenderable>(null)
  const timeline = useTimeline({ autoplay: false })
  const animation = useRef(timeline)

  useEffect(() => {
    const target = dot.current
    if (!target || !animate) return
    animation.current.add(target, { opacity: 0.3, duration: 700, ease: "inOutSine", loop: true, alternate: true }).play()
    return () => {
      animation.current.pause()
      animation.current.resetItems()
      target.opacity = 1
    }
  }, [animate])

  return <text ref={dot} fg={theme.accent}>●</text>
}

export function WorkingIndicator({ status, theme, animate = true }: { status: "WORKING" | "CANCELING"; theme: Theme; animate?: boolean }) {
  return <box flexDirection="row">
    <PulseDot theme={theme} animate={animate} />
    <text fg={theme.accent}>  {status}</text>
  </box>
}
