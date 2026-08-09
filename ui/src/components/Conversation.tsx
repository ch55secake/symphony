import type { ScrollBoxRenderable } from "@opentui/core"
import { useEffect, useRef } from "react"
import type { TranscriptEntry } from "../protocol/types"
import type { Theme } from "../theme/tokens"
import { ToolActivityRow } from "./ToolActivityRow"

export function Conversation({ entries, theme }: { entries: TranscriptEntry[]; theme: Theme }) {
  const scrollbox = useRef<ScrollBoxRenderable>(null)
  useEffect(() => {
    const frame = requestAnimationFrame(() => scrollbox.current?.scrollTo({ y: scrollbox.current.scrollHeight, x: 0 }))
    return () => cancelAnimationFrame(frame)
  }, [entries])

  if (entries.length === 0) return <box alignItems="center" justifyContent="center" style={{ flexGrow: 1, flexShrink: 1, minHeight: 3, width: "100%", overflow: "hidden" }}>
    <text fg={theme.subtle}>Conversation ready. Ask for a change or type /help.</text>
  </box>

  return <scrollbox ref={scrollbox} focusable={false} stickyScroll style={{ flexGrow: 1, flexShrink: 1, minHeight: 3, width: "100%", overflow: "hidden" }}>
    {entries.map((entry, index) => {
      if (entry.role === "activity" && entry.tool) return <ToolActivityRow key={entry.tool.id} tool={entry.tool} theme={theme} />
      const user = entry.role === "user"
      return <box key={index} flexDirection="column" paddingLeft={user ? 0 : 2} paddingRight={1} marginBottom={1}>
        <text fg={user ? theme.accent : theme.model}>{user ? "YOU" : entry.label.toUpperCase()}</text>
        <text fg={user ? theme.muted : theme.text}>{entry.content}</text>
      </box>
    })}
  </scrollbox>
}
