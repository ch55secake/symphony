import { SyntaxStyle, type TreeSitterClient } from "@opentui/core"
import type { Theme } from "../theme/tokens"

const styles = new Map<string, SyntaxStyle>()

function styleKey(theme: Theme) {
  return [theme.text, theme.muted, theme.subtle, theme.accent, theme.model, theme.warning, theme.success].join(":")
}

function markdownStyle(theme: Theme) {
  const key = styleKey(theme)
  const cached = styles.get(key)
  if (cached) return cached
  const syntaxStyle = SyntaxStyle.fromStyles({
    default: { fg: theme.text },
    conceal: { fg: theme.subtle, dim: true },
    comment: { fg: theme.subtle, italic: true },
    string: { fg: theme.success },
    "string.special": { fg: theme.success },
    number: { fg: theme.warning },
    boolean: { fg: theme.warning },
    constant: { fg: theme.warning },
    "constant.builtin": { fg: theme.warning },
    variable: { fg: theme.text },
    "variable.member": { fg: theme.accent },
    "variable.builtin": { fg: theme.model },
    property: { fg: theme.accent },
    type: { fg: theme.model },
    "type.builtin": { fg: theme.model },
    function: { fg: theme.accent, bold: true },
    "function.call": { fg: theme.accent },
    "function.method": { fg: theme.accent },
    "function.method.call": { fg: theme.accent },
    "function.builtin": { fg: theme.accent },
    keyword: { fg: theme.model, bold: true },
    operator: { fg: theme.warning },
    "punctuation.delimiter": { fg: theme.muted },
    "punctuation.bracket": { fg: theme.muted },
    "punctuation.special": { fg: theme.warning },
    attribute: { fg: theme.warning },
    label: { fg: theme.model },
    "module.builtin": { fg: theme.model },
    "markup.heading": { fg: theme.model, bold: true },
    "markup.strong": { fg: theme.text, bold: true },
    "markup.italic": { fg: theme.muted, italic: true },
    "markup.strikethrough": { fg: theme.subtle },
    "markup.raw": { fg: theme.accent },
    "markup.quote": { fg: theme.muted },
    "markup.list": { fg: theme.accent, bold: true },
    "markup.link": { fg: theme.accent },
    "markup.link.label": { fg: theme.accent, underline: true },
    "markup.link.url": { fg: theme.subtle, underline: true },
  })
  styles.set(key, syntaxStyle)
  return syntaxStyle
}

export function MarkdownMessage({ content, theme, treeSitterClient }: { content: string; theme: Theme; treeSitterClient?: TreeSitterClient }) {
  return <markdown
    content={content}
    syntaxStyle={markdownStyle(theme)}
    treeSitterClient={treeSitterClient}
    fg={theme.text}
    conceal
    concealCode
    tableOptions={{ style: "columns", widthMode: "full", wrapMode: "word", borders: false, outerBorder: false, cellPaddingX: 1, cellPaddingY: 0 }}
    style={{ width: "100%", flexShrink: 0 }}
  />
}
