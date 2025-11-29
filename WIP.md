Current Goal: Fix staticcheck issues preventing `make all` from passing

Action Plan:
- Run full checks and tests
- Fix linter issues in tests and generated files
- Re-run checks until all pass

Progress Log:
- Located staticcheck failures in generated proto tests and ineffective break statements
- Removed ineffective breaks and replaced with `return` where appropriate
- Added `//lint:file-ignore SA1019` on generated proto test files
- Re-ran `make all` and verified all checks pass
