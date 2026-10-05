package web

import (
	"encoding/json"
	"fmt"
)

const htmlViewerBridge = `(function(config) {
  "use strict";
  const VERSION = 1;
  const MAX_EXPORT_BYTES = 32 * 1024 * 1024;
  const RATE_WINDOW_MS = 60000;
  const RATE_LIMIT = 4;
  let sequence = 0;
  let acknowledged = false;
  let preparingDownload = false;
  let pendingDownload = null;
  const recentDownloads = [];
  const completed = new Set();
  const completedOrder = [];
  function requestId() { sequence += 1; return config.viewerId + ":" + sequence; }
  function envelope(type, payload, id) {
    return Object.assign({ version: VERSION, nonce: config.nonce, viewerId: config.viewerId,
      notePath: config.notePath, requestId: id || requestId(), type: type }, payload || {});
  }
  function send(type, payload, id, transfer) {
    if (parent === window) throw new Error("Rhizome viewer controls require an application parent");
    parent.postMessage(envelope(type, payload, id), "*", transfer || []);
  }
  function resolveTarget(target) {
    try { return new URL(String(target), document.baseURI).href; } catch (_) { return ""; }
  }
  function rememberCompleted(id) {
    completed.add(id); completedOrder.push(id);
    while (completedOrder.length > 128) completed.delete(completedOrder.shift());
  }
  function checkRate() {
    const now = Date.now();
    while (recentDownloads.length && now - recentDownloads[0] > RATE_WINDOW_MS) recentDownloads.shift();
    if (recentDownloads.length >= RATE_LIMIT) throw new Error("download request limit reached; wait before trying again");
    recentDownloads.push(now);
  }
  function validDownloadLabel(value, maxLength) {
    return value.length <= maxLength && !/[\u0000-\u001f\u007f]/.test(value);
  }
  async function exportBytes(filename, mediaType, value) {
    if (!acknowledged) throw new Error("Rhizome viewer is not connected to its application parent");
    if (preparingDownload || pendingDownload) throw new Error("another download is already awaiting a response");
    preparingDownload = true;
    try {
      checkRate();
      if (value instanceof Blob && value.size > MAX_EXPORT_BYTES) throw new RangeError("download exceeds the 32 MiB limit");
      if (typeof value === "string" && value.length > MAX_EXPORT_BYTES) throw new RangeError("download exceeds the 32 MiB limit");
      if (value instanceof ArrayBuffer && value.byteLength > MAX_EXPORT_BYTES) throw new RangeError("download exceeds the 32 MiB limit");
      if (ArrayBuffer.isView(value) && value.byteLength > MAX_EXPORT_BYTES) throw new RangeError("download exceeds the 32 MiB limit");
      let bytes;
      if (value instanceof Blob) bytes = new Uint8Array(await value.arrayBuffer());
      else if (typeof value === "string") bytes = new TextEncoder().encode(value);
      else if (value instanceof ArrayBuffer) bytes = new Uint8Array(value);
      else if (ArrayBuffer.isView(value)) bytes = new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
      else throw new TypeError("download bytes must be a string, Blob, ArrayBuffer, or typed array");
      if (bytes.byteLength > MAX_EXPORT_BYTES) throw new RangeError("download exceeds the 32 MiB limit");
      const requestedFilename = String(filename || "download");
      const requestedMediaType = String(mediaType || "application/octet-stream");
      if (!validDownloadLabel(requestedFilename, 512) || !validDownloadLabel(requestedMediaType, 200)) {
        throw new TypeError("download filename or media type is invalid");
      }
      const id = requestId();
      const transferred = bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength ? bytes.buffer : bytes.slice().buffer;
      return await new Promise(function(resolve, reject) {
        pendingDownload = { id: id, resolve: resolve, reject: reject };
        send("download", { filename: requestedFilename, mediaType: requestedMediaType, size: bytes.byteLength, encoding: "arrayBuffer", data: transferred }, id, [transferred]);
      });
    } finally {
      preparingDownload = false;
    }
  }
  function requestNavigation(type, target) {
    const authored = String(target);
    send(type, { target: authored, resolved: resolveTarget(authored) });
  }
  Object.defineProperty(window, "rhizome", { configurable: false, enumerable: true, writable: false, value: Object.freeze({
    version: VERSION,
    currentNote: Object.freeze({ path: config.notePath }),
    open: function(target) { requestNavigation("navigate", target); },
    openExternal: function(target) { requestNavigation("external", target); },
    download: exportBytes
  }) });
  addEventListener("message", function(event) {
    const data = event.data;
    if (event.source !== parent || !data || data.version !== VERSION || data.nonce !== config.nonce || data.viewerId !== config.viewerId || data.notePath !== config.notePath) return;
    if (data.type === "ack") { acknowledged = true; return; }
    if (data.type === "set-fragment" && typeof data.fragment === "string") {
      const next = data.fragment ? "#" + encodeURIComponent(data.fragment.replace(/^#/, "")) : "";
      if (location.hash !== next) history.replaceState(null, "", location.pathname + location.search + next);
      const target = data.fragment && document.getElementById(data.fragment.replace(/^#/, ""));
      if (target) target.scrollIntoView({ block: "start" });
      return;
    }
    if (data.type === "download-result" && pendingDownload && data.requestId === pendingDownload.id && !completed.has(data.requestId)) {
      const pending = pendingDownload; pendingDownload = null; rememberCompleted(data.requestId);
      if (data.ok) pending.resolve(data.requestId); else pending.reject(new Error(String(data.message || "download canceled")));
    }
  });
  addEventListener("click", function(event) {
    if (event.defaultPrevented || event.button !== 0) return;
    const anchor = event.target && event.target.closest ? event.target.closest("a[href]") : null;
    if (!anchor) return;
    const authored = anchor.getAttribute("href") || "";
    if (anchor.hasAttribute("download") && (authored.startsWith("blob:") || authored.startsWith("data:"))) {
      event.preventDefault();
      fetch(anchor.href).then(function(response) { return response.blob(); }).then(function(blob) {
        return exportBytes(anchor.getAttribute("download") || "download", blob.type, blob);
      }).catch(function(error) { try { send("error", { scope: "download", message: String(error) }); } catch (_) {} });
      return;
    }
    event.preventDefault();
    send("navigate", { target: authored, resolved: resolveTarget(authored), beside: !!(event.metaKey || event.ctrlKey) });
  }, true);
  addEventListener("error", function(event) { try { send("error", { scope: "resource", message: String(event.message || "Resource failed to load") }); } catch (_) {} }, true);
  function ready() { try { send("ready", { href: location.pathname + location.search + location.hash }); } catch (_) {} }
  if (document.readyState === "loading") addEventListener("DOMContentLoaded", ready, { once: true });
  else queueMicrotask(ready);
})`

func injectHTMLViewerBootstrap(source []byte, offset int, grant htmlViewerGrant) ([]byte, error) {
	if offset < 0 || offset > len(source) {
		return nil, fmt.Errorf("HTML viewer bootstrap offset is outside source")
	}
	config, err := json.Marshal(map[string]string{
		"viewerId": grant.id,
		"nonce":    grant.nonce,
		"notePath": grant.notePath,
	})
	if err != nil {
		return nil, fmt.Errorf("encode HTML viewer bootstrap: %w", err)
	}
	bootstrap := []byte("<script>" + htmlViewerBridge + "(" + string(config) + ");</script>")
	out := make([]byte, 0, len(source)+len(bootstrap))
	out = append(out, source[:offset]...)
	out = append(out, bootstrap...)
	out = append(out, source[offset:]...)
	return out, nil
}
