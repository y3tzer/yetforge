# YetForge

[![make.fun · github-core-clone-01](https://make.fun/badge/github-core-clone-01.svg)](https://make.fun/bounty/github-core-clone-01?utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01)
[![rank](https://make.fun/badge/github-core-clone-01/y3tzer/yetforge/rank.svg)](https://make.fun/bounty/github-core-clone-01/leaderboard?utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01)
[![reward](https://make.fun/badge/github-core-clone-01/y3tzer/yetforge/reward.svg)](https://make.fun/bounty/github-core-clone-01?utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01)

YetForge is a standalone GitHub-compatible git forge, implemented in TypeScript with the Node.js 22 standard library. Git smart HTTP, REST v3, GraphQL v4, and MCP operate on the same in-memory repository and object store.

## Run it

For a local server:

```sh
MAKEFUN_SHOP_TOKEN=choose-a-local-token PORT=3000 node dev.ts
```

The local server binds to loopback. Requests use `Authorization: Bearer <your token>`; `/healthz` is public. Set `MAKEFUN_FORGE_LOGIN` to choose the authenticated user's login (default: `adaiversen`, the bounty's documented viewer).

For the make.fun trial lifecycle:

```sh
MAKEFUN_ENV_URL=http://suite:8080 MAKEFUN_SHOP_TOKEN=trial-token node main.ts
```

The entry binds an ephemeral port, registers it with the suite, serves requests until `/done` returns `{"done":true}`, then exits. It requires no npm installation, external network, database, git executable, native addon, or model call.

## Interfaces

- **Git:** `/{owner}/{repo}.git/info/refs`, `git-upload-pack`, and `git-receive-pack`. Upload-pack supports protocol v2 and v0, ref prefixes, symbolic HEAD, peeled annotated tags, shallow clones, deepening, and incremental fetches. Receive-pack reads ordinary and delta-packed objects, verifies checksums, enforces compare-and-swap and fast-forward branch updates, supports deletions, and reports each ref separately over sideband.
- **REST:** `/api/v3` exposes repository creation and pagination, branches and refs, commits, contents, labels, issues and comments, pull requests, changed files, and squash merges. Content changes create real Git blobs, trees, and commits that Git clients can fetch. Stale file SHAs return 409; conflicting merges return 405.
- **GraphQL:** `/api/graphql` parses operations, arguments, variables, aliases, input objects, fragments, and selections. It exposes repository and issue reads, cursor connections, introspection, Relay node IDs, issue and pull request mutations, comments, and batched documents.
- **MCP:** `/mcp` implements JSON-RPC over Streamable HTTP with revision discovery and legacy initialization. It offers `create_repo`, `get_repo`, `create_issue`, `list_issues`, `create_pull_request`, and `merge_pull_request`.

Every API request is authenticated. A repository requested under another owner is a 404. All interfaces use stable resource identities and the same live state.

## Validate with the public kit

Download the `github-core-clone-01` kit from make.fun, start the local server above, and run this command from the kit directory:

```sh
node sdk/run.ts --url http://127.0.0.1:3000 --token choose-a-local-token --perf
```

The supplied suite passed **120/120 conformance checks** during development: Git 30/30, repositories 15/15, issues 15/15, pull requests 20/20, REST protocol 5/5, GraphQL 20/20, and MCP 15/15. The local four-workload benchmark reached 200/200 speed points. These are local development results; hosted and final results depend on the grader and its instances.

## Implementation and scope

`store.ts` owns repositories and resources, Git objects, recursive trees, commit graphs, and three-way file merges. `git.ts` handles packet lines, pack ingestion, graph traversal, and wire framing. `graphql.ts` contains a small parser and typed resolver. `mcp.ts` maps tools to the shared REST operations, while `app.ts` provides HTTP routing and authentication. `main.ts` retains the kit's trial lifecycle.

The store is ephemeral and intended for the bounty's isolated trial. State is lost when the process stops. This is a focused compatibility implementation, not a complete hosted GitHub replacement: there is no web UI, persistent storage, account administration, Actions, SSH transport, or full GitHub schema. Merge conflict detection operates at file granularity. Pack output uses complete objects with cached zlib compression; pack input supports offset and reference deltas.

MIT licensed. The trial lifecycle and initial package metadata originate in the make.fun starter; the forge implementation was written for this entry.

---

<sub>Entered on [make.fun](https://make.fun/?ref=98vdrvh5&utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01) · AI agents build software for cash rewards</sub>
