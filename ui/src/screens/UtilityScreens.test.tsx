import { afterEach, describe, expect, test } from "bun:test"
import type { TestRendererSetup } from "@opentui/core/testing"
import { testRender } from "@opentui/react/test-utils"
import { act } from "react"
import { getTheme } from "../theme/tokens"
import { SelectionScreen } from "./SelectionScreen"
import { ConfirmationScreen, SettingsScreen } from "./UtilityScreens"

let setup: TestRendererSetup | undefined
afterEach(async () => {
  await act(async () => setup?.renderer.destroy())
})

function expectTransparent() {
  const painted = setup!.captureSpans().lines.flatMap((line) => line.spans).filter((span) => span.text.trim() && span.bg.a !== 0)
  expect(painted).toEqual([])
}

describe("utility screens", () => {
  test("distinguishes the active model from the focused model", async () => {
    const options = Array.from({ length: 12 }, (_, index) => `model-${index}`)
    await act(async () => {
      setup = await testRender(<SelectionScreen name="model" options={options} selected={7} current="model-5" width={80} height={20} theme={getTheme()} />, { width: 80, height: 20 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("SYMPHONY  / MODEL")
    expect(frame).toContain("> model-7")
    expect(frame).toContain("model-5")
    expect(frame).toContain("CURRENT")
    expect(frame).toContain("8 / 12")
    expectTransparent()
  })

  test("renders theme descriptions and palette previews", async () => {
    await act(async () => {
      setup = await testRender(<SelectionScreen name="theme" options={["default", "contrast", "mono"]} selected={1} current="default" width={80} height={20} theme={getTheme("contrast")} />, { width: 80, height: 20 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("PREVIEW A PALETTE")
    expect(frame).toContain("> CONTRAST")
    expect(frame).toContain("Cyan + fuchsia")
    expect(frame).toContain("DEFAULT")
    expect(frame).toContain("CURRENT")
    expectTransparent()
  })

  test("renders an interactive settings dashboard", async () => {
    await act(async () => {
      setup = await testRender(<SettingsScreen provider="openai" model="gpt-test" themeName="default" workspace="/workspace/symphony" allowAll selected={2} width={80} height={20} theme={getTheme()} />, { width: 80, height: 20 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("SYMPHONY  / SETTINGS")
    expect(frame).toContain("> APPROVALS")
    expect(frame).toContain("ALLOW ALL")
    expect(frame).toContain("PROVIDER")
    expect(frame).toContain("/workspace/symphony")
    expect(frame).toContain("ENTER CHANGE")
    expectTransparent()
  })

  test("keeps compact settings controls visible with long values", async () => {
    await act(async () => {
      setup = await testRender(<SettingsScreen provider="opencode" model="a-very-long-model-name-that-must-not-wrap" themeName="contrast" workspace="/workspace/with/a/very/long/project/path" allowAll={false} selected={0} width={48} height={14} theme={getTheme("contrast")} />, { width: 48, height: 14 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("> MODEL")
    expect(frame).toContain("ASK EVERY TIME")
    expect(frame).toContain("ENTER CHANGE")
    expect(frame.split("\n")).toHaveLength(15)
    expectTransparent()
  })

  test("explains the session-only allow-all confirmation", async () => {
    await act(async () => {
      setup = await testRender(<ConfirmationScreen status="Allow all workspace writes and commands for this session?" width={80} height={18} theme={getTheme()} />, { width: 80, height: 18 })
    })
    await setup!.flush()
    const frame = setup!.captureCharFrame()
    expect(frame).toContain("ENABLE ALLOW ALL?")
    expect(frame).toContain("will run without")
    expect(frame).toContain("individual prompts")
    expect(frame).toContain("current session only")
    expect(frame).toContain("KEEP ASKING")
    expectTransparent()
  })
})
