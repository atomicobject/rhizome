import "@testing-library/jest-dom/vitest";

// WHY: Sigma 3 reads WebGL2 scalar constants while its renderer module loads.
// jsdom has no WebGL implementation, but these constants are sufficient for
// components that do not create a live renderer in unit tests.
const webGLConstants = {
  BOOL: 0x8b56,
  BYTE: 0x1400,
  UNSIGNED_BYTE: 0x1401,
  SHORT: 0x1402,
  UNSIGNED_SHORT: 0x1403,
  INT: 0x1404,
  UNSIGNED_INT: 0x1405,
  FLOAT: 0x1406,
  POINTS: 0x0000,
  LINES: 0x0001,
  TRIANGLES: 0x0004,
  COLOR_BUFFER_BIT: 0x4000,
  FRAMEBUFFER: 0x8d40,
};

Object.defineProperty(globalThis, "WebGLRenderingContext", {
  configurable: true,
  value: webGLConstants,
});

Object.defineProperty(globalThis, "WebGL2RenderingContext", {
  configurable: true,
  value: webGLConstants,
});

// WHY: @sigma/node-image builds its texture atlas while its module loads: it
// probes MAX_TEXTURE_SIZE from a throwaway "webgl" context and seeds a "2d"
// context with one getImageData call. jsdom's getContext returns null (and
// logs "not implemented"), so answer those two probes with the minimum they
// read. Every other context id stays null: no unit test boots a live
// renderer, and a null context keeps that failure obvious. Node-environment
// suites (`@vitest-environment node`) have no canvas at all and skip this.
const MAX_TEXTURE_SIZE = 0x0d33;

if (typeof HTMLCanvasElement !== "undefined") {
  Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
    configurable: true,
    writable: true,
    value: function getContext(this: HTMLCanvasElement, contextId: string) {
      if (contextId === "webgl") {
        return { canvas: this, MAX_TEXTURE_SIZE, getParameter: () => 4096 };
      }

      if (contextId === "2d") {
        return {
          canvas: this,
          getImageData: (_x: number, _y: number, width: number, height: number) => ({
            width,
            height,
            data: new Uint8ClampedArray(width * height * 4),
          }),
        };
      }

      return null;
    },
  });
}

// WHY: jsdom has no layout engine. Browser tests verify resize-driven geometry;
// component tests only need the observer lifecycle to be available.
Object.defineProperty(globalThis, "ResizeObserver", {
  configurable: true,
  writable: true,
  value: class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
});

// WHY: CodeMirror measures selection geometry through Range rects while it
// mounts. jsdom has no layout, so answer with empty rects instead of throwing.
if (typeof Range !== "undefined" && !Range.prototype.getClientRects) {
  Range.prototype.getClientRects = () => Object.assign([], { item: () => null });
}

if (typeof Range !== "undefined" && !Range.prototype.getBoundingClientRect) {
  Range.prototype.getBoundingClientRect = () => new DOMRect();
}
