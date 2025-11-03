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
