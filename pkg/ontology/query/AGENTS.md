# Query test ownership

- Assert selected GraphQL fields in the returned result. A clean response or bounded read count alone can pass when a field is dropped.
- Public limit tests need more authored rows than the limit; small fixtures cannot detect a missing cap.
- When comparing `queryPlan` with execution, use a valid executable selection and a reader that fails if unexpected content projection occurs.
