# Provider access

`pkg/app/provideraccess` is the one application factory that decides how Rhizome reaches Voyage and TypeSafe ([SPEC-0129](../../../docs/specs/technical/team-api-gateway.md)). Every production provider construction goes through `Default().NewEmbeddings`, `Default().EmbeddingConfig`, or `Default().NewTypeSafe`. Provider libraries receive the decision explicitly and never read the session store.

- Precedence: an explicit direct key (`VOYAGE_API_KEY`/`RHIZOME_VOYAGE_API_KEY`, `TYPESAFE_API_KEY`, or a caller-supplied key), then the team session, then the legacy bundle (`pkg/teamkeys`), then unavailable with a `rzm auth login` hint. A store error falls through to the bundle and is named in the unavailable message.
- `Factory.DevBuild` takes a signed-in session whose origin is loopback before direct keys. Without one, the normal order applies.
- The choice is made once per construction. A gateway failure is returned to the caller and never retried through another billing source.
- `VoyageSource` and `TypeSafeSource` report `direct`, `team`, `bundle`, or `none`; `TeamStatus` reports `{signedIn, email, origin, expiresAt}`. Neither ever contains a token or key.
- Non-Voyage embedding providers resolve exactly as `embeddings.NewProviderForConfig` does. The reranker keeps direct keys only.
- The application installs the real session loader once at startup with `Install`. The zero `Factory` has no loader and behaves as Rhizome did before team sessions. Callers keep close ownership of returned providers and caches.
