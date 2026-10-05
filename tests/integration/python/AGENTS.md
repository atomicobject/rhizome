# Subprocess test ownership

- Capture CLI stdout and stderr separately. Assert diagnostics on the stream the command actually writes; never return an invented empty stderr.
