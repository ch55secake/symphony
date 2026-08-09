import { describe, expect, test } from "bun:test"
import { commandSuggestions } from "./registry"

describe("commandSuggestions", () => {
  test("matches incomplete commands", () => {
    expect(commandSuggestions("/m").map((command) => command.name)).toEqual(["/model"])
    expect(commandSuggestions("/p").map((command) => command.name)).toEqual(["/plan"])
  })

  test("hides completed commands and arguments", () => {
    expect(commandSuggestions("/model")).toEqual([])
    expect(commandSuggestions("/model gpt")).toEqual([])
  })

  test("exposes mode commands", () => {
    expect(commandSuggestions("/").map((command) => command.name)).toEqual(["/allow-all", "/build", "/help", "/model", "/plan", "/settings", "/theme"])
  })
})
