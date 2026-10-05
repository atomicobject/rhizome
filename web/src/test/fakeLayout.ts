import { afterEach, beforeEach } from "vitest";

/**
 * jsdom never lays out or measures anything: it has no ResizeObserver, and
 * every element reports `offsetWidth`/`offsetHeight` of 0. Libraries that
 * measure their own DOM (React Flow, for one) therefore refuse to render
 * anything that depends on a size. These fakes report the size an element
 * asked for through its inline style, and deliver one resize notification per
 * observed element so the measuring code runs.
 */
const FALLBACK_SIZE = 500;

function styleSize(element: HTMLElement, dimension: "width" | "height"): number {
  const declared = Number.parseFloat(element.style[dimension]);

  return Number.isFinite(declared) ? declared : FALLBACK_SIZE;
}

function resizeEntry(target: Element): ResizeObserverEntry {
  const rect = target.getBoundingClientRect();
  const size = { blockSize: rect.height, inlineSize: rect.width };

  return {
    target,
    contentRect: rect,
    borderBoxSize: [size],
    contentBoxSize: [size],
    devicePixelContentBoxSize: [size],
  };
}

class FakeResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}

  observe(target: Element): void {
    this.callback([resizeEntry(target)], this);
  }

  unobserve(): void {}
  disconnect(): void {}
}

function installOffsetSize(dimension: "width" | "height"): () => void {
  const property = dimension === "width" ? "offsetWidth" : "offsetHeight";
  const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, property);
  Object.defineProperty(HTMLElement.prototype, property, {
    configurable: true,
    get(this: HTMLElement) {
      return styleSize(this, dimension);
    },
  });

  return () => {
    if (original) {
      Object.defineProperty(HTMLElement.prototype, property, original);
    } else {
      Reflect.deleteProperty(HTMLElement.prototype, property);
    }
  };
}

/**
 * jsdom has no DOMMatrixReadOnly. React Flow builds one only to read the
 * viewport zoom out of its CSS transform; `m22 = 1` states "unzoomed", which is
 * the only meaningful reading in a document that never paints.
 */
class FakeDOMMatrixReadOnly {
  readonly m22 = 1;
}

function installFakeDOMMatrix(): () => void {
  const original = Object.getOwnPropertyDescriptor(window, "DOMMatrixReadOnly");
  Object.defineProperty(window, "DOMMatrixReadOnly", {
    configurable: true,
    writable: true,
    value: FakeDOMMatrixReadOnly,
  });

  return () => {
    if (original) {
      Object.defineProperty(window, "DOMMatrixReadOnly", original);
    } else {
      Reflect.deleteProperty(window, "DOMMatrixReadOnly");
    }
  };
}

/**
 * jsdom implements no SVG geometry, so `getBBox` is missing entirely and any
 * component that measures its own text (React Flow edge labels, for one)
 * throws. An empty box is the honest answer for an unpainted document.
 */
function installFakeGetBBox(): () => void {
  const original = Object.getOwnPropertyDescriptor(SVGElement.prototype, "getBBox");
  Object.defineProperty(SVGElement.prototype, "getBBox", {
    configurable: true,
    writable: true,
    value: () => new DOMRect(0, 0, 0, 0),
  });

  return () => {
    if (original) {
      Object.defineProperty(SVGElement.prototype, "getBBox", original);
    } else {
      Reflect.deleteProperty(SVGElement.prototype, "getBBox");
    }
  };
}

export function installFakeLayout(): () => void {
  const originalObserver = globalThis.ResizeObserver;
  globalThis.ResizeObserver = FakeResizeObserver;
  const restoreMatrix = installFakeDOMMatrix();
  const restoreGetBBox = installFakeGetBBox();
  const restoreWidth = installOffsetSize("width");
  const restoreHeight = installOffsetSize("height");

  return () => {
    restoreHeight();
    restoreWidth();
    restoreGetBBox();
    restoreMatrix();
    globalThis.ResizeObserver = originalObserver;
  };
}

/** Installs the measurement fakes for the surrounding suite. */
export function withFakeLayout(): void {
  let restore = () => {};

  beforeEach(() => {
    restore = installFakeLayout();
  });
  afterEach(() => {
    restore();
  });
}
