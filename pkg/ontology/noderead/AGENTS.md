# Test ownership

- Test apply refresh and link resolution through public `Scope` methods. Private cache maps and parser invocations do not prove caller-visible behavior.
- Keep one success fixture per contract. Carry a distinct assertion into the strongest owner test before removing a duplicate fixture.
- For overlay filters, include both matching and excluded staged rows; a one-row `exists` assertion can pass when filtering is bypassed.
- For apply refresh, warm and reread `TypeInstances`, `Graph`, and `GraphFacts` on the same scope.
- Keep SQL endpoint-selector and query adapter tests in their owning packages; `Scope.Graph` tests cover the additional assembled-result contract.
