import { describe, expect, test } from "bun:test"
import { commandSuggestions } from "./registry"

describe("commandSuggestions", () => {
  test("matches incomplete commands", () => {
    expect(commandSuggestions("/m").map((command) => command.name)).toEqual(["/model"])
  })

  test("hides completed commands and arguments", () => {
    expect(commandSuggestions("/model")).toEqual([])
    expect(commandSuggestions("/model gpt")).toEqual([])
  })
})
