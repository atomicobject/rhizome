# Test ownership

- Shared browser fake contracts belong in their own tests. GraphQL route tests register a later nonmatching operation with a conflicting response so ignoring the operation predicate fails.
