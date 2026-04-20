# request-sudo — Architecture Outline

_Last updated: 2026-04-20 UTC_

## Goal

Build a literate permission broker for Linux.

A requester asks for one privileged action.
A human sees a clear explanation.
The exact action runs once if approved.

## Main components

### `request-sudo`
Requester CLI.

### `request-sudod`
Root-owned broker daemon.
Single writer.
Single executor.

### `request-sudoctl`
Local admin/review tool.

### Future adapter layer
For later SMS/chat integration only.
Not part of the current local-first slice.

## Communication model

Use Unix sockets:
- `/run/request-sudo/request.sock`
- `/run/request-sudo/review.sock`

## State model

- append-only hash-chained event log
- rebuildable projection

## Current phase boundary

The current codebase is at the local-first hardening/installer stage.
Remote approval transport is intentionally later work.
