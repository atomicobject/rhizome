// Counts component renders through React's DevTools hook, so render-boundary
// tests observe the real tree without replacing modules. Import this module
// before anything that loads react-dom: React reads the hook when it loads.

import type { ReactNode } from "react";

type Fiber = {
  type: object | string | null;
  memoizedProps: object | string | null;
  child: Fiber | null;
  sibling: Fiber | null;
};

const renders = new Map<object, object[]>();

// A render creates a new props object; a bailout keeps the previous one
// (memo reuses it when props are shallowly equal), so each new object is one render.
const seenProps = new WeakSet<object>();

function visit(fiber: Fiber | null) {
  for (let current = fiber; current; current = current.sibling) {
    const props = current.memoizedProps;

    if (current.type instanceof Function && props instanceof Object) {
      if (!seenProps.has(props)) {
        seenProps.add(props);
        renders.set(current.type, [...(renders.get(current.type) ?? []), props]);
      }
    }

    visit(current.child);
  }
}

Object.defineProperty(globalThis, "__REACT_DEVTOOLS_GLOBAL_HOOK__", {
  configurable: true,
  value: {
    supportsFiber: true,
    isDisabled: false,
    renderers: new Map(),
    inject: () => 1,
    checkDCE: () => undefined,
    onScheduleFiberRoot: () => undefined,
    onCommitFiberUnmount: () => undefined,
    onPostCommitFiberRoot: () => undefined,
    onCommitFiberRoot: (_renderer: number, root: { current: Fiber }) => visit(root.current),
  },
});

/** Renders of `component` since the last reset, optionally only those whose props match. */
export function renderCount<Props>(
  component: (props: Props) => ReactNode,
  matches: (props: Props) => boolean = () => true,
): number {
  // SAFETY: entries under a component type are the props React rendered it with.
  const list = (renders.get(component) ?? []) as Props[];

  return list.filter(matches).length;
}

export function resetRenderCounts() {
  renders.clear();
}
