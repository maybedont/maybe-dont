## v1.5.0 (2026-03-11)

### Feat

- agent hook scripts for 5 AI agents (#139)

## v1.4.0 (2026-03-05)

### Feat

- add AI response rule for agent-restricted content (#136)
- add POST /api/v1/intercept endpoint for agent hook validation (#134)

### Fix

- session affinity — context enrichment and connected state (#137)
- data race in AI engine async audit goroutines (#135)
- use endpoint config as fully qualified URL (#132)

## v1.3.0 (2026-02-19)

### Feat

- add action validation endpoint for OpenHands integration (#127)

### Fix

- improve AI policy test accuracy and match rate semantics (#125)
- allow gateway to start without downstream MCP servers (#124)
- improve AI policy test accuracy (#121)

## v1.2.0 (2026-02-13)

### Feat

- categorize extra-policy-only failures in model comparison (#118)

### Fix

- per-test-case content hashing for correct model comparison (#117)
- widen comparison table columns and add alignment tests (#115)
- add space before units in model comparison table (#113)
- add space before units in model comparison table columns

### Refactor

- move include_argument_values from CLI to audit config (#116)

## v1.1.0 (2026-02-10)

### Feat

- move gateway server to `gateway` sub-command (#107)
- rolling pass rate history and stability reporting (#106)
- pre-release updates with AI parameter defaults and env var support (#96)
- clean up AI policy prompts and fix redact decision logic (#93)
- implement policy test suite CLI (#85)
- CLI proxy for AI agent command validation (#86)
- implement provider-agnostic AI validation (#83)
- add gateway authentication header for caller identification (#81)

### Fix

- harden CI state file handling and add model comparison output (#112)
- treat zero-decided tests as vacuously meeting thresholds
- rename state file to prevent silent upload exclusion
- propagate request ID to context for CLI validation logs (#111)
- defaults export no-clobber and temperature handling (#110)
- remove duplicate config validation causing double deprecation warnings (#108)
- align test executor response evaluation with production engine (Phase 2) (#105)
- deny action should suppress redacted content in response validation (#104)
- improve AI policy test accuracy and executor alignment (#102)
- align summary stats across stdout, JSON, and GH action summary (#101)
- sanitize invalid JSON escapes and fix temperature config (#100)
- sanitize invalid JSON escapes from AI responses and fix temperature config
- default temperature to 0.0 for deterministic AI policy decisions (#99)
- default temperature to 0.0 in AI provider factory for deterministic output
- bump default test suite timeout to 60s to reduce false timeouts
- persist AI test state on failure and reduce rate limit noise (#97)
- persist AI test state on failure and reduce rate limit output noise
- resolve data race in TestBackgroundFlush (#89)
- add session validation to native tools for consistent recovery (#87)

## v1.0.0 (2026-02-02)

### BREAKING CHANGE

- 1.0.0 is not compatible with previous versions.

### Feat

- implement XDG Base Directory support and self-contained binary (#77)
- implement async audit-only AI validation (#67)

## v0.7.2 (2025-12-11)

### Fix

- Correct publishing of the .sha256 checksum files.

## v0.7.1 (2025-12-10)

### Fix

- release script missing sha256 files and docs version update

## v0.7.0 (2025-12-10)

### Feat

- add individual SHA256 checksum files for release archives

### Fix

- improve Dockerfile build order and simplify build script
- correct multi-arch Docker build issues

## v0.6.0 (2025-12-09)

### Feat

- add multi-architecture Docker image support

## v0.5.8 (2025-11-17)

### Fix

- default to http

## v0.5.7 (2025-11-16)

## v0.5.6 (2025-11-16)

### Fix

- speed up build by removing garble

## v0.5.5 (2025-11-16)

## v0.5.4 (2025-11-16)

## v0.5.3 (2025-11-16)

## v0.5.2 (2025-11-16)

### Fix

- clarify config loading
- update gateway config for happy path
- bundle the rules into distribution

## v0.5.1 (2025-11-03)

### Fix

- fix rules paths to be relative to config path

## v0.5.0 (2025-11-03)

### Feat

- add metrics collection

### Fix

- use garble without const

## v0.4.0 (2025-10-28)

### Feat

- log sessionId
- add pass-through authentication infrastructure
- add response validation

### Fix

- lint and build
- clean up logging
- clean up logging
- loggni with context

## v0.3.2 (2025-10-12)

### Fix

- handle meta properly
- update libs

## v0.3.1 (2025-07-21)

### Fix

- update readme

## v0.3.0 (2025-07-21)

### Feat

- add merge option to release script

### Refactor

- remove curl fallback from release script

## v0.2.2 (2025-07-21)

## v0.2.3 (2025-07-21)

## v0.2.1 (2025-07-21)

## v0.2.0 (2025-07-21)

### BREAKING CHANGE

- makes the config multi-client, requiring changes to
older configurations.

### Feat

- Add multi-client support for connecting to multiple MCP servers

## v0.1.6 (2025-07-06)

### Fix

- update config handling

## v0.1.5 (2025-07-06)

### Fix

- default the server and listen address

## v0.1.4 (2025-07-06)

### Fix

- allow sending logs to stderr only
- logger for stdio didn't make sense

## v0.1.3 (2025-07-06)

### Fix

- linter errors
- update logging config and remove unnecessary options

### Refactor

- add some info logging on start
- more renaming
- clean up a couple log lines and rename stuff
- rename proxy to gateway

## v0.1.2 (2025-07-05)

### Fix

- update goreleaser to wrap in directory

## v0.1.1 (2025-06-29)

### Fix

- revert changelog
- change listener to 0.0.0.0 for docker
- audit log flag supersedes the config file
- update base image to distroless so certs work

## v0.1.0 (2025-06-29)

### Feat

- support http auth using env vars for header
- add support for HTTP on the client and server
- output a more coherent error message for the user

### Fix

- capture all failed policies
- add policy name to error message
- tweak rules to be less restrictive
- fix bugs in validation chain and tests

### Refactor

- add error handling to satisfy linter

## v0.0.7 (2025-06-29)

### Refactor

- remove unused request.json file

## v0.0.6 (2025-06-29)

## v0.0.5 (2025-06-29)

### Fix

- whitespace

## v0.0.4 (2025-06-29)

## v0.0.3 (2025-06-28)

### Fix

- tweak config

## v0.0.2 (2025-06-08)

### Feat

- use embed. change names

### Fix

- pass version info better
- embed files
- remove docker since it doesn't do podman

## v0.0.1 (2025-06-08)

### Feat

- add a bunch of rules
- ai calls are working
- ai rules engine and first rule
- initial policy violation
- detect downstream capabilities
- basic proxy config
- config
- better cli
- initial CLI
- initial commit

### Fix

- ci and docs stuff
- turn AI back on
- cel policies are logged
- ai policies working in p arallel
- tweaking AI rules
- format ai rules properly
- use the has() function to make sure fields exist
- error handling
- most logs should be debug
- some logs cleanup
- fully remove default
- fix the policy engine logic
- collate policies properly
- tidy
- implement stdio for server and refactor slightly
- tidy
- remove unsed CEL secrets code
- remove a bunch of unused config
- audit logger config properly from config
- separate loggers
- move logging to a separate file
- better logging of results
- remove excess unused crap
- update readme
- ai validator
- better messages
- slightly better error handling
- CEL validation is working
- rename tool validations to be more accurate
- tool validation chain is at least somewhat functional
- some more detailed logging
- small tweaks
- proxy is working now
- function signatures working
- logging
- ctrl+c works
- idk
- it runs but still can't connect
- error handling
- correct the sse endpoint
- small things
- rules
