import { afterEach, describe, expect, test } from "bun:test"
import type { TestRendererSetup } from "@opentui/core/testing"
import { testRender } from "@opentui/react/test-utils"
import { act } from "react"
import { commands } from "../commands/registry"
import { SelectionList } from "../components/SelectionList"
import { getTheme } from "../theme/tokens"
import { ChatScreen } from "./ChatScreen"

let setup: TestRendererSetup | undefined
afterEach(async () => {
  await act(async () => setup?.renderer.destroy())
})

describe("ChatScreen", () => {
  test("keeps every command visible above a populated transcript", async () => {
    await act(async () => {
      setup = await testRender(<ChatScreen
      provider="openai"
      model="test-model"
      workspace="/workspace"
      status="READY"
      transcript={[
        { role: "user", label: "You", content: "Please inspect the project" },
        { role: "assistant", label: "test-model", content: "I inspected several files and can continue with the requested work." },
      ]}
      value="/"
      onChange={() => {}}
      onSubmit={() => {}}
      suggestions={[...commands]}
      selected={0}
      spinner={0}
      theme={getTheme()}
      width={100}
      height={30}
      />, { width: 100, height: 30 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    for (const command of commands) expect(frame.match(new RegExp(command.name.replace("/", "\\/"), "g"))?.length).toBe(1)
    expect(frame).toContain("ASK")
    expect(frame).toContain("Please inspect the project")
  })

  test("renders safe command activity metadata", async () => {
    await act(async () => {
      setup = await testRender(<ChatScreen
      provider="openai"
      model="test-model"
      workspace="/workspace"
      status="WORKING"
      transcript={[{ role: "activity", label: "test-model", tool: { id: "call-1", name: "run_command", phase: "running", command: "go test ./...", working_directory: "workspace", output_hidden: true } }]}
      value=""
      onChange={() => {}}
      onSubmit={() => {}}
      suggestions={[]}
      selected={0}
      spinner={0}
      theme={getTheme()}
      width={100}
      height={24}
      />, { width: 100, height: 24 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("Running")
    expect(frame).toContain("go test ./...")
    expect(frame).toContain("output hidden")
  })

  test("scrolls a long selection list to the active option", async () => {
    const options = Array.from({ length: 20 }, (_, index) => `model-${index}`)
    await act(async () => {
      setup = await testRender(<SelectionList options={options} selected={16} height={5} theme={getTheme()} />, { width: 50, height: 8 })
    })
    await setup!.flush()
    await setup!.renderOnce()
    await setup!.flush()
    expect(setup!.captureCharFrame()).toContain("model-16")
  })
})
