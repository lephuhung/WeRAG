/* The label of the ⌘K / Ctrl+K palette shortcut for the visitor's system:
 * Apple keyboards show ⌘, every other system Ctrl (the handler accepts both). */

/** Whether a user-agent / platform string belongs to an Apple system. */
export function isApplePlatform(platform: string): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(platform);
}

/** "⌘K" on Apple systems, "Ctrl K" elsewhere. */
export function paletteShortcutLabel(platform: string): string {
  return isApplePlatform(platform) ? "⌘K" : "Ctrl K";
}
