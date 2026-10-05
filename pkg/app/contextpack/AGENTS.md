# Context pack tests

Assert literal packed text, ordering, byte limits, and metadata at `Pack` or `PackDetailed`. Test compression pressure at `PackWithIntent` with a recorder; expected values must not come from another call to `Pack`. Provider prompt tests belong in `compress` and inspect the request sent to the provider.
