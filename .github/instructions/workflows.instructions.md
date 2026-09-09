---
applyTo: '.github/**/*.{yml,yaml}'
---

Review workflows with least privilege. Fork pull requests must not receive
secrets or write tokens. Keep dependency updates intentional, ensure untrusted
pull-request code cannot reach privileged contexts, and preserve the same
formatting, vet, race-test, and module-integrity gates used locally.
