# Update test ownership

- ResolveInvocation owns executable location and pin gating. BuildUpdatePlan owns target selection. Run owns prepare, commit order, and truthful partial results.
- Exercise trust checks with HTTP and archive fixtures. Keep process tests for delegation, standard streams, PID, and exit behavior at the command boundary.
- Keep failure injection where production code owns the observable ordering. Do not replace it with a fake that supplies the expected sequence.
