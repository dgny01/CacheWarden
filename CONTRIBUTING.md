# Contributing to CacheWarden

Thank you for improving CacheWarden.

## Repository language

English is the working language for every repository artifact:

- source code, identifiers, comments, log messages, and error messages;
- commit subjects and bodies;
- documentation and architecture decision records;
- issue descriptions, pull request descriptions, and review comments.

Use clear technical English. User-facing localization can be added in a future
milestone without changing the repository's working language.

## Development workflow

1. Create a focused branch from `main`.
2. Make the smallest coherent change.
3. Run `make lint`, `make test`, and `make build`.
4. Use a Conventional Commit message, such as
   `feat(victim): expose workload duration metrics`.
5. Complete the pull request template and include validation evidence.

Do not commit generated benchmark result files. Summarize relevant measurements
in a pull request or a dedicated document instead.

## Safety

The aggressor is a bounded experimental workload. Keep safe defaults, retain
resource limits, and document any change that can increase memory or CPU use.
Never present it as a denial-of-service or general-purpose stress-testing tool.

