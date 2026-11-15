import { replyToAgentPageRequest } from "../../api/internalAgentClient";

/** Names the page a page_text answer describes. */
export type PageIdentity = { title: string; route: string };

/** The server keeps 60,000 bytes; this keeps the request near that. */
const MAX_CHARS = 60_000;

// innerText follows the layout, so hidden sections drop out; DOMs without
// layout, such as jsdom, have only the raw text.
const rendered = (element: HTMLElement): string =>
  "innerText" in element && element.innerText !== undefined
    ? element.innerText
    : (element.textContent ?? "");

// innerText of an element that is not rendered is its raw text, so hidden
// tabs, retained panels, and inactive sections are skipped before reading.
function shown(element: Element): boolean {
  // jsdom has no layout, and so no checkVisibility.
  if (!("checkVisibility" in Element.prototype))
    return !element.closest("[hidden]") && getComputedStyle(element).visibility !== "hidden";

  return element.checkVisibility({ visibilityProperty: true });
}

// A display: contents element has no box: it reads as not shown and its
// innerText is its raw text, so its children are read one by one instead.
const boxless = (element: Element) => getComputedStyle(element).display === "contents";

// The rendered text of an element, leaving out the right pane (the chat
// itself) wherever it sits inside.
function textWithoutPane(element: HTMLElement): string {
  if (element.matches("[data-right-pane]")) return "";

  if (!boxless(element) && !element.querySelector("[data-right-pane]")) return rendered(element);

  return [...element.children]
    .flatMap((child) =>
      child instanceof HTMLElement && (boxless(child) || shown(child))
        ? [textWithoutPane(child)]
        : [],
    )
    .filter(Boolean)
    .join("\n");
}

// Same-origin frames, such as custom views, keep their text out of innerText.
function frameText(root: HTMLElement): string[] {
  return [...root.querySelectorAll("iframe")].flatMap((frame) => {
    if (!shown(frame)) return [];

    try {
      const body = frame.contentDocument?.body;
      const text = body ? rendered(body).trim() : "";

      return text ? [text] : [];
    } catch {
      return [];
    }
  });
}

/** The visible text of the section the user is looking at, without the chat. */
export function visiblePageText(root: HTMLElement = document.body): string {
  const section = root.querySelector<HTMLElement>(".app-shell__main") ?? root;
  const text = [textWithoutPane(section), ...frameText(section)].join("\n\n").trim();

  return text.length > MAX_CHARS ? text.slice(0, MAX_CHARS) : text;
}

/**
 * Answers one page_text request. Every window showing the session answers and
 * the first reply wins, so a refused reply is not an error.
 */
export async function answerPageRequest(sessionID: string, requestID: string, page: PageIdentity) {
  try {
    await replyToAgentPageRequest(sessionID, requestID, { ...page, text: visiblePageText() });
  } catch {
    // Another window answered first, or the request expired.
  }
}
