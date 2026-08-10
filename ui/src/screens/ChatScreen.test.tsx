import { afterEach, describe, expect, test } from "bun:test"
import type { TestRendererSetup } from "@opentui/core/testing"
import { MockTreeSitterClient } from "@opentui/core/testing"
import { testRender } from "@opentui/react/test-utils"
import { act } from "react"
import { commands } from "../commands/registry"
import { SelectionList } from "../components/SelectionList"
import { WorkingIndicator } from "../components/WorkingIndicator"
import { getTheme } from "../theme/tokens"
import { ChatScreen } from "./ChatScreen"

let setup: TestRendererSetup | undefined
let treeSitterClient: MockTreeSitterClient | undefined
afterEach(async () => {
  await act(async () => {
    setup?.renderer.destroy()
    await treeSitterClient?.destroy()
  })
  treeSitterClient = undefined
})

describe("ChatScreen", () => {
  test("keeps every command visible above a populated transcript", async () => {
    treeSitterClient = new MockTreeSitterClient({ autoResolveTimeout: 0 })
    await act(async () => {
      setup = await testRender(<ChatScreen
      provider="openai"
      model="test-model"
      workspace="/workspace"
      status="READY"
      mode="build"
      treeSitterClient={treeSitterClient}
      transcript={[
        { role: "user", label: "You", content: "Please inspect the project" },
        { role: "assistant", label: "test-model", content: "I inspected several files and can continue with the requested work." },
      ]}
      value="/"
      onChange={() => {}}
      onSubmit={() => {}}
      suggestions={[...commands]}
      selected={0}
      theme={getTheme()}
      width={100}
      height={30}
      />, { width: 100, height: 30 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    for (const command of commands) expect(frame.match(new RegExp(command.name.replace("/", "\\/"), "g"))?.length).toBe(1)
    expect(frame).toContain(">")
    expect(frame).toContain("BUILD >")
    expect(frame).toContain("Please inspect the project")
    const paintedBackgrounds = setup!.captureSpans().lines.flatMap((line) => line.spans).filter((span) => span.text.trim()).filter((span) => span.bg.a !== 0)
    expect(paintedBackgrounds).toEqual([])
  })

  test("renders safe command activity metadata", async () => {
    await act(async () => {
      setup = await testRender(<ChatScreen
      provider="openai"
      model="test-model"
      workspace="/workspace"
      status="READY"
      mode="build"
      transcript={[{ role: "activity", label: "test-model", tool: { id: "call-1", name: "run_command", phase: "running", command: "go test ./...", working_directory: "workspace", output_hidden: true } }]}
      value=""
      onChange={() => {}}
      onSubmit={() => {}}
      suggestions={[]}
      selected={0}
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

  test("renders assistant Markdown while preserving user text", async () => {
    treeSitterClient = new MockTreeSitterClient({ autoResolveTimeout: 0 })
    await act(async () => {
      setup = await testRender(<ChatScreen
      provider="openai"
      model="test-model"
      workspace="/workspace"
      status="READY"
      mode="build"
      treeSitterClient={treeSitterClient}
      transcript={[
        { role: "user", label: "You", content: "Use **literal** Markdown here" },
        { role: "assistant", label: "test-model", content: "# Summary\n\n**Implemented** the change.\n\n- Added `MarkdownMessage`\n- Verified the result\n\n```typescript\nconst total = 1 + 2\n```" },
      ]}
      value=""
      onChange={() => {}}
      onSubmit={() => {}}
      suggestions={[]}
      selected={0}
      theme={getTheme()}
      width={100}
      height={30}
      />, { width: 100, height: 30 })
    })
    await setup!.flush()
    await new Promise((resolve) => setTimeout(resolve, 10))
    await setup!.renderOnce()
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("Summary")
    expect(frame).toContain("Implemented")
    expect(frame).toContain("MarkdownMessage")
    expect(frame).toContain("const total = 1 + 2")
    expect(frame).toContain("Use **literal** Markdown here")
  })

  test("keeps approval controls separate from the requested command", async () => {
    const command = "go test ./internal/store/kurrentdb -run TestAppendToSessionStream"
    await act(async () => {
      setup = await testRender(<ChatScreen
      provider="openai"
      model="test-model"
      workspace="/workspace"
      status="AWAITING APPROVAL"
      mode="build"
      transcript={[{ role: "activity", label: "test-model", tool: { id: "call-1", name: "run_command", phase: "awaiting_approval", command, working_directory: "workspace" } }]}
      approval={{ action: "run_command", summary: "run go (4 arguments)", hash: "sha256:test" }}
      value=""
      onChange={() => {}}
      onSubmit={() => {}}
      suggestions={[]}
      selected={0}
      theme={getTheme()}
      width={64}
      height={15}
      />, { width: 64, height: 15 })
    })
    await setup!.flush()
    const lines = setup!.captureCharFrame().split("\n")
    const commandRow = lines.findIndex((line) => line.includes("Wants to run"))
    const decisionRow = lines.findIndex((line) => line.includes("[Y] APPROVE"))
    expect(commandRow).toBeGreaterThanOrEqual(0)
    expect(decisionRow).toBeGreaterThan(commandRow)
    expect(lines[commandRow]).not.toContain("[Y] APPROVE")
  })

  test("renders the working pulse without a spinner glyph", async () => {
    await act(async () => {
      setup = await testRender(<WorkingIndicator status="WORKING" theme={getTheme()} animate={false} />, { width: 30, height: 3 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("●  WORKING")
    expect(frame).not.toContain("|  WORKING")
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
