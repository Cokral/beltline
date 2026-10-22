import { expect, type Page } from "@playwright/test";

/** Chrome's computed colours of the palette's slate and gold in each appearance, as getComputedStyle reports them. */
const glyphColours = {
  light: { slate: "rgb(117, 153, 201)", gold: "rgb(165, 126, 34)" },
  dark: { slate: "rgb(157, 187, 228)", gold: "rgb(227, 185, 94)" }
} as const;

/**
 * The run's metro line, as a person sees and uses it. The selected station's glyph is blue unless it is the run's
 * current station, which keeps its gold; the button has no wash or border; keyboard focus shows an outline on a
 * station button; and Enter selects the station that has focus. The selection is put back before the screenshot.
 */
export async function checkMetroLine(page: Page, id: string, check: { stage: string; current: boolean }, appearance: "light" | "dark" = "light"): Promise<void> {
  const button = (stage: string) => page.locator(`button.run-metro-hit[data-stage="${stage}"]`);
  const selected = button(check.stage);
  await expect(selected, `${id} selected station ${check.stage} should be pressed`).toHaveAttribute("aria-pressed", "true");
  const glyph = await page.locator(`[data-stop="${check.stage}"] .run-metro-glyph`).evaluate(element => getComputedStyle(element).fill);
  expect(glyph, `${id} glyph of ${check.stage}`).toBe(check.current ? glyphColours[appearance].gold : glyphColours[appearance].slate);
  const wash = await selected.evaluate(element => {
    const style = getComputedStyle(element);
    return { background: style.backgroundColor, shadow: style.boxShadow, border: style.borderTopWidth };
  });
  expect(wash, `${id} station button should draw no wash or border`).toEqual({ background: "rgba(0, 0, 0, 0)", shadow: "none", border: "0px" });
  const other = page.locator("button.run-metro-hit:not([data-stage='" + check.stage + "'])").first();
  const otherStage = await other.getAttribute("data-stage");
  await other.focus();
  await page.keyboard.press("Enter");
  await expect(button(otherStage!), `${id} Enter should select ${otherStage}`).toHaveAttribute("aria-pressed", "true");
  await selected.focus();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Shift+Tab");
  const outline = await selected.evaluate(element => getComputedStyle(element).outlineStyle);
  expect(outline, `${id} keyboard focus should show an outline`).toBe("solid");
  await page.keyboard.press("Enter");
  await expect(selected, `${id} Enter should select ${check.stage} again`).toHaveAttribute("aria-pressed", "true");
  // The focus ring is for keyboard users; the screenshot shows the selection alone.
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
}
