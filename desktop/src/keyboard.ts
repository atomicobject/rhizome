export function isMacOS() {
  return window.__RHIZOME_MACOS__ !== false;
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
