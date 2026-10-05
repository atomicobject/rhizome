// Run from a checkout with web dependencies installed. Output stays outside the repo.
const { chromium } = require('../../web/node_modules/playwright');
const fs = require('node:fs');

async function main() {
  const [url, output, button, summary] = process.argv.slice(2);
  if (!url || !output || !button || !summary) {
    throw new Error('Usage: node node_open_browser.cjs URL OUTPUT_PREFIX BUTTON_TEXT EXACT_SUMMARY');
  }
  const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal'] });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    await page.addInitScript((expected) => {
      window.nodeOpenProbe = { contexts: [], start: null, domMs: null, frameMs: null };
      const original = HTMLCanvasElement.prototype.getContext;
      const seen = new WeakSet();
      HTMLCanvasElement.prototype.getContext = function (kind, ...args) {
        const result = original.call(this, kind, ...args);
        if (result && ['webgl', 'webgl2'].includes(kind) && !seen.has(this)) {
          seen.add(this);
          const debug = result.getExtension('WEBGL_debug_renderer_info');
          window.nodeOpenProbe.contexts.push({
            time: performance.now(),
            global: Boolean(this.closest('.ontology-home__graph')),
            local: Boolean(this.closest('.ontology-local-graph')),
            vendor: debug ? result.getParameter(debug.UNMASKED_VENDOR_WEBGL) : null,
            renderer: debug ? result.getParameter(debug.UNMASKED_RENDERER_WEBGL) : null,
          });
        }
        return result;
      };
      addEventListener('pointerdown', () => {
        window.nodeOpenProbe.start = performance.now();
      }, true);
      addEventListener('DOMContentLoaded', () => {
        new MutationObserver(() => {
          const probe = window.nodeOpenProbe;
          if (probe.start === null || probe.domMs !== null) return;
          const found = [...document.querySelectorAll('td, dd, p, span, div')]
            .some(element => element.textContent === expected);
          if (!found) return;
          probe.domMs = performance.now() - probe.start;
          requestAnimationFrame(() => { probe.frameMs = performance.now() - probe.start; });
        }).observe(document.body, { childList: true, subtree: true, characterData: true });
      });
    }, summary);
    await page.goto(url);
    await page.locator('.ontology-home__graph canvas.sigma-nodes').waitFor();
    await page.waitForTimeout(1500);
    const before = await page.evaluate(() => window.nodeOpenProbe.contexts);
    await page.getByRole('button', { name: button, exact: true }).first().click();
    await page.waitForFunction(() => window.nodeOpenProbe.frameMs !== null);
    const result = await page.evaluate(() => window.nodeOpenProbe);
    fs.writeFileSync(`${output}.json`, JSON.stringify({ before, ...result }, null, 2));
    await page.screenshot({ path: `${output}.png`, fullPage: true });
    console.log(JSON.stringify(result));
  } finally {
    await browser.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
