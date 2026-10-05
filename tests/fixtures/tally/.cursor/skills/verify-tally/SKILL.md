---
name: verify-tally
description: "Drive the tally inventory CLI against a disposable store; use when proving that stores open and items are stored and listed."
---

# Verify tally

The user-facing surface is the `tally` CLI in `bin/`. Read [the feature map](features/README.md) before choosing a path.

## Launch

Create a disposable store directory owned by this run and record the owner:

```bash
RUN_ID="$(date +%s)-$$"
STORE="${TMPDIR:-/tmp}/tally-$RUN_ID"
mkdir -m 700 "$STORE" && echo "$RUN_ID" > "$STORE/owner"
```

## Doctor

Confirm `cat "$STORE/owner"` prints `$RUN_ID` and `bin/tally --version --store "$STORE"` prints `tally 1.0`. Never drive a store this run did not create.

## Refresh

To keep driving a store from an earlier session, confirm its `owner` file still names that session's run and `store.json` still reads. tally loads its code from the checkout on every call, so nothing restarts.

## Drive

Run `bin/tally --store "$STORE" <command>` as a user does. Follow the feature files for each command.

## Evidence

Keep the command, stdout, stderr and exit code, plus a second observation of the store file `$STORE/store.json`, in an evidence directory outside the store.

## Cleanup

Remove `$STORE` only when its `owner` file names this run. Keep the evidence.

## Helpers

None.
