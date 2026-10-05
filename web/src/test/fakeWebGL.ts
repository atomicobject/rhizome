// WHY: Sigma 3 renders through WebGL, which jsdom does not implement. The
// global setup answers the two constant probes @sigma/node-image makes at
// module load; a test that boots a live renderer needs more: WebGL context
// classes it can `instanceof`, a context object that answers every GL call,
// and a container with a non-zero size. Installing that here keeps the cost
// on the few renderer tests instead of every suite.

const GL_CONSTANTS = {
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
  DEPTH_BUFFER_BIT: 0x0100,
  FRAMEBUFFER: 0x8d40,
  ARRAY_BUFFER: 0x8892,
  ELEMENT_ARRAY_BUFFER: 0x8893,
  STATIC_DRAW: 0x88e4,
  DYNAMIC_DRAW: 0x88e8,
  VERTEX_SHADER: 0x8b31,
  FRAGMENT_SHADER: 0x8b30,
  COMPILE_STATUS: 0x8b81,
  LINK_STATUS: 0x8b82,
  TEXTURE_2D: 0x0de1,
  TEXTURE0: 0x84c0,
  RGBA: 0x1908,
  LINEAR: 0x2601,
  NEAREST: 0x2600,
  CLAMP_TO_EDGE: 0x812f,
  TEXTURE_MIN_FILTER: 0x2801,
  TEXTURE_MAG_FILTER: 0x2800,
  TEXTURE_WRAP_S: 0x2802,
  TEXTURE_WRAP_T: 0x2803,
  MAX_TEXTURE_SIZE: 0x0d33,
  BLEND: 0x0be2,
  ONE: 1,
  ONE_MINUS_SRC_ALPHA: 0x0303,
  SRC_ALPHA: 0x0302,
  DEPTH_TEST: 0x0b71,
};

type ContextSize = { width: number; height: number };

function createGLContext(canvas: HTMLCanvasElement) {
  const base = {
    canvas,
    drawingBufferWidth: canvas.width,
    drawingBufferHeight: canvas.height,
    ...GL_CONSTANTS,
    getExtension: () => null,
    getParameter: () => 4096,
    getAttribLocation: () => 0,
    getUniformLocation: () => ({}),
    getShaderParameter: () => true,
    getProgramParameter: () => true,
    getShaderInfoLog: () => "",
    getProgramInfoLog: () => "",
  };

  // WHY: Sigma calls dozens of GL entry points during a draw. Anything the
  // base object does not define is a no-op returning a fresh handle object,
  // which is all Sigma stores and passes back to us.
  const known = new Map<string | symbol, unknown>(Object.entries(base));

  return new Proxy(base, {
    get(_target, property) {
      return known.has(property) ? known.get(property) : () => ({});
    },
  });
}

/**
 * Install WebGL context classes, a functioning `getContext`, and non-zero
 * element sizing so a live Sigma renderer can mount in jsdom.
 * Returns a restore function; call it in `afterEach`.
 */
export function installFakeWebGL(size: ContextSize = { width: 800, height: 600 }): () => void {
  class FakeWebGLRenderingContext {}

  Object.assign(FakeWebGLRenderingContext, GL_CONSTANTS);

  class FakeWebGL2RenderingContext extends FakeWebGLRenderingContext {}

  const previousGL = Object.getOwnPropertyDescriptor(globalThis, "WebGLRenderingContext");
  const previousGL2 = Object.getOwnPropertyDescriptor(globalThis, "WebGL2RenderingContext");

  const previousGetContext = Object.getOwnPropertyDescriptor(
    HTMLCanvasElement.prototype,
    "getContext",
  );

  const previousWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth");
  const previousHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight");

  Object.defineProperty(globalThis, "WebGLRenderingContext", {
    configurable: true,
    value: FakeWebGLRenderingContext,
  });
  Object.defineProperty(globalThis, "WebGL2RenderingContext", {
    configurable: true,
    value: FakeWebGL2RenderingContext,
  });
  Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
    configurable: true,
    writable: true,
    value: function getContext(this: HTMLCanvasElement, contextId: string) {
      if (contextId === "2d") {
        return {
          canvas: this,
          scale: () => undefined,
          clearRect: () => undefined,
          fillRect: () => undefined,
          fillText: () => undefined,
          measureText: () => ({ width: 0 }),
          beginPath: () => undefined,
          closePath: () => undefined,
          moveTo: () => undefined,
          lineTo: () => undefined,
          arc: () => undefined,
          fill: () => undefined,
          stroke: () => undefined,
          save: () => undefined,
          restore: () => undefined,
          drawImage: () => undefined,
          getImageData: (_x: number, _y: number, width: number, height: number) => ({
            width,
            height,
            data: new Uint8ClampedArray(width * height * 4),
          }),
        };
      }

      if (contextId !== "webgl" && contextId !== "webgl2") return null;
      const context = createGLContext(this);
      Object.setPrototypeOf(context, FakeWebGL2RenderingContext.prototype);

      return context;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get: () => size.width,
  });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get: () => size.height,
  });

  return () => {
    restore(globalThis, "WebGLRenderingContext", previousGL);
    restore(globalThis, "WebGL2RenderingContext", previousGL2);
    restore(HTMLCanvasElement.prototype, "getContext", previousGetContext);
    restore(HTMLElement.prototype, "offsetWidth", previousWidth);
    restore(HTMLElement.prototype, "offsetHeight", previousHeight);
  };
}

type PatchTarget = typeof globalThis | HTMLElement;

function restore(
  target: PatchTarget,
  property: string,
  descriptor: PropertyDescriptor | undefined,
): void {
  if (descriptor) {
    Object.defineProperty(target, property, descriptor);

    return;
  }

  Reflect.deleteProperty(target, property);
}
