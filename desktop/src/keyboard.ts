export function isMacOS() {
  // The shell receives the platform name; the finder still receives only the
  // macOS flag. Either one missing means the macOS default.
  const platform = window.__RHIZOME_PLATFORM__;
  return platform ? platform === "macos" : window.__RHIZOME_MACOS__ !== false;
}

export function primaryModifier(event: Pick<KeyboardEvent, "metaKey" | "ctrlKey">) {
  return isMacOS() ? event.metaKey : event.ctrlKey;
}

export function shortcut({ key, shift = false }: { key: string; shift?: boolean }) {
  const macOS = isMacOS();
  return {
    label: macOS ? `${shift ? "⇧" : ""}⌘${key}` : `Ctrl+${shift ? "Shift+" : ""}${key}`,
    aria: `${macOS ? "Meta" : "Control"}+${shift ? "Shift+" : ""}${key}`,
  };
}
