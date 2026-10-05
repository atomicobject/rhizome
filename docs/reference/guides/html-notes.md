---
type: ReferenceDoc
summary: "How Rhizome indexes, displays, and safely runs admitted HTML notes and interactive prototypes."
reference-kind: guide
status: active
---

# HTML notes and interactive prototypes

Rhizome treats an HTML file as a note only when `notes.includes` admits it. Markdown defaults do not include HTML automatically.

```yaml
notes:
  includes:
    - "**/*.md"
    - "reports/**/*.html"
    - "reports/**/*.htm"
```

An HTML note can declare root metadata with one JSON object in the document head:

```html
<script id="rhizome-metadata" type="application/json">
  {
    "type": "Report",
    "title": "Quarterly report",
    "status": "ready",
    "tags": ["finance"]
  }
</script>
```

Rhizome indexes the static document. It extracts visible text, headings, links, fragment targets, and bounded inline script or data bodies. It does not execute scripts during indexing, fetch remote data, or index changes made to the runtime DOM.

## Reading and editing properties

HTML notes open in normal Notes tabs. **Read** runs an interactive document in an isolated frame. **Source** shows the authored HTML as inert, read-only text. Rhizome does not provide an HTML body editor.

Root properties use the same staged Save and Discard controls as Markdown notes. Saving a file without Rhizome metadata previews and inserts the canonical JSON block while preserving the rest of the file byte for byte. A malformed, duplicate, or ambiguously placed metadata block remains read-only until it is fixed outside Rhizome.

Saving properties reloads the isolated document after indexing publishes the new source. This resets the prototype's runtime state.

## Scripts, assets, and navigation

The viewer supports classic and module scripts plus local relative and vault-root assets that remain inside the admitted vault and are not ignored or inside control directories. Links to Rhizome notes return to the parent Notes workspace. External HTTP and HTTPS links require a parent action before opening. Other navigation protocols are rejected.

The frame uses `sandbox="allow-scripts"` with an opaque origin. Forms, popups, direct downloads, application credentials, and application APIs are unavailable to the document.

A prototype can request a generated file through:

```js
await window.rhizome.download("results.csv", "text/csv", csvText);
```

Blob and data download links are also intercepted. Each file is limited to 32 MiB. Only one export can wait at a time, and the parent shows the filename and size before the user's click starts the download. Remote URL exports that depend on credential or CORS bypasses are not supported.

## Local and remote serving

Loopback serving uses a separate capability host such as `http://<token>.localhost:<port>` automatically.

A remotely accessed server needs two HTTPS origins and wildcard DNS and TLS for the content hostname:

```bash
rzm serve \
  --application-origin https://rhizome.example.com \
  --html-content-origin 'https://{token}.content.example.com'
```

Route both hosts to the same private Rhizome listener, preserve the original Host header, and replace `X-Forwarded-Proto` with `https`. Rhizome rejects configured remote origins that are not HTTPS and rejects capability requests whose effective transport does not match the minted URL. The `{token}` placeholder must occupy a complete hostname label. Keep `content.example.com` dedicated to Rhizome viewer traffic. Unknown, nested, expired, and revoked capability hosts return no application UI.

The content virtual host carries a capability in its hostname, so keep it out of access and error logs. For example, an Nginx content host can use:

```nginx
server {
  listen 443 ssl;
  server_name *.content.example.com;
  access_log off;
  error_log /var/log/nginx/rhizome-content-error.log crit;

  location / {
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto https;
    proxy_pass http://127.0.0.1:8080;
  }
}
```

Do not expose the private listener directly. The application origin should use the same authentication boundary that controls capability creation.

HTML file moves and authored-link rewrites are not supported in this release. Move files outside Rhizome, then reindex and resolve any broken links. Source, metadata, link, resource, and viewer problems appear as note diagnostics where the source can be identified.

A complete example vault is available at `testdata/integration/html-notes`.
