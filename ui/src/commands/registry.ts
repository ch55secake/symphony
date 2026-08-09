export const commands = [
	{ name: "/allow-all", description: "Approve actions for this session" },
	{ name: "/build", description: "Switch to implementation mode" },
	{ name: "/help", description: "Show available commands" },
	{ name: "/model", description: "Select the active model" },
	{ name: "/plan", description: "Switch to planning mode" },
  { name: "/settings", description: "Inspect the current session" },
  { name: "/theme", description: "Choose the next-session theme" },
] as const

export type Command = (typeof commands)[number]

export function commandSuggestions(value: string): Command[] {
  if (!value.startsWith("/") || value.includes(" ")) return []
  return commands.filter((command) => command.name.startsWith(value) && command.name !== value)
}
