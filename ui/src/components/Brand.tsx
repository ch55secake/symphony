import type { Theme } from "../theme/tokens"

const mark = `███████╗██╗   ██╗███╗   ███╗██████╗ ██╗  ██╗ ██████╗ ███╗   ██╗██╗   ██╗
██╔════╝╚██╗ ██╔╝████╗ ████║██╔══██╗██║  ██║██╔═══██╗████╗  ██║╚██╗ ██╔╝
███████╗ ╚████╔╝ ██╔████╔██║██████╔╝███████║██║   ██║██╔██╗ ██║ ╚████╔╝
╚════██║  ╚██╔╝  ██║╚██╔╝██║██╔═══╝ ██╔══██║██║   ██║██║╚██╗██║  ╚██╔╝
███████║   ██║   ██║ ╚═╝ ██║██║     ██║  ██║╚██████╔╝██║ ╚████║   ██║
╚══════╝   ╚═╝   ╚═╝     ╚═╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═══╝╚═══╝   ╚═╝`

export function Brand({ theme, expanded }: { theme: Theme; expanded: boolean }) {
  if (!expanded) return <text fg={theme.accent}>SYMPHONY</text>
  return <text fg={theme.accent}>{mark}</text>
}
