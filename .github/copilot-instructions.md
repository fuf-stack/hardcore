# Pull-request review instructions

Review Hardcore as a compatibility-focused maintainer of a public Go module.
Follow the nearest `AGENTS.md`.

Prioritize concrete defects over style commentary:

- exported API or documented runtime behavior regressions;
- lifecycle races, leaked goroutines, incomplete graceful shutdown, and ignored
  cancellation or deadlines;
- readiness behavior that can report healthy before startup completes or while
  shutdown is in progress;
- abstractions that expose private consumer concepts or speculate beyond a
  demonstrated consumer need;
- dependency-direction violations and unnecessary third-party dependencies;
- tests that skip failure, cancellation, timeout, or concurrency behavior;
- release, workflow, permission, credential, or provenance weaknesses.

Expect `make check` and, for database or lifecycle changes,
`make test-integration` to pass. Keep comments actionable and cite the exact
path and failure mode.
