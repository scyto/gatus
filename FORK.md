# This is a fork

Upstream: [TwiN/gatus](https://github.com/TwiN/gatus). This fork exists for **two changes**, each kept deliberately small so it can be rebased onto each upstream release and dropped if upstream ever takes it.

## Change 1: `${VAR}_FILE`

`${VAR}` in a configuration file checks `${VAR}_FILE` first. If that names a readable file, its contents (trimmed) are used. Otherwise the environment variable is used, exactly as before.

```yaml
endpoints:
  - name: proxmox
    url: "https://pve.example.com:8006/api2/json/cluster/status"
    headers:
      Authorization: "PVEAPIToken=user@pam!token=${PROXMOX_TOKEN}"
```

```yaml
environment:
  - PROXMOX_TOKEN_FILE=/run/secrets/proxmox_api_token_v1
secrets:
  - proxmox_api_token_v1
```

One function in `config/config.go`, plus tests. Nothing else is modified.

## Why

Gatus substitutes `${VAR}` from the environment. **On Docker Swarm there is no supported way to get a secret into an environment variable.** Swarm secrets are files, and every route out was measured and found closed:

| Route | Result |
|---|---|
| Swarm secret → environment variable | No such mechanism exists |
| Entrypoint exporting from `/run/secrets` | The image is distroless: `docker run --entrypoint sh ghcr.io/twin/gatus:v5.36.0` → `exec: "sh": executable file not found` |
| `env_file:` at a host path | Portainer processes the compose file and mounts only its own `/data` |
| `env_file:` relative to the compose file | Resolves inside the git checkout, so the value must be committed |

The remaining option is a credential in the service spec, readable by anything that can reach the Docker API. That is what this avoids.

## Change 2: an hourly SQLite backup

With `GATUS_SQLITE_BACKUP_PATH` set and SQLite storage, Gatus writes a copy of its database to that path with `VACUUM INTO`, creating its directory if needed: once at start, then every hour at `GATUS_SQLITE_BACKUP_MINUTE` (default `50`). The copy goes to `<path>.tmp` and is renamed into place only when complete. Unset, nothing changes.

```yaml
environment:
  - GATUS_SQLITE_BACKUP_PATH=/data/backup/gatus.db
```

One new file, `storage/store/sql/backup.go`, started from `NewStore` and stopped from `Close`, plus tests.

### Why

Gatus runs SQLite in WAL mode. WAL needs every process that opens the database on the same host, sharing its `-shm` file, so a backup tool in another container is only safe on the same node as Gatus. On Docker Swarm nothing keeps two services on one node: placement is checked only when a task is scheduled, so a sidecar can be left on another node after Gatus moves, where opening the database risks the live copy. And the image is distroless, so nothing can run inside Gatus's own container.

`VACUUM INTO` from Gatus itself avoids all of that: the process that owns the database writes a consistent copy while it keeps running, wherever it runs. A snapshot of the live `.db`, `-wal` and `-shm` files is not a reliable substitute.

## Upstream position

- [#1399](https://github.com/TwiN/gatus/issues/1399) — closed `not_planned`: *"You can use environment variables in the configuration. That alone should be sufficient, as most deployment mechanisms allow ways to securely mount secrets as an environment variable."* True of Kubernetes; not of Swarm.
- [#690](https://github.com/TwiN/gatus/pull/690) — a PR implementing this, open since 2024-02-28, reviewed, now stale. This change is the same idea, rebased, with logging added.
- [#1626](https://github.com/TwiN/gatus/issues/1626) — the same request again, April 2026.

If #690 is ever merged, delete this fork and go back to the upstream image.

## Where the reasoning lives

**In this file, not in the code.** The diff is deliberately small and its comments deliberately plain: a doc comment on the function, one line explaining the trim, nothing else. Everything about *why the fork exists*, what was measured, and which routes were ruled out belongs here.

Two reasons. A reader of `config.go` wants to know what the function does, not the history of a deployment problem on someone else's cluster. And if this is ever offered upstream, a tight diff with ordinary comments has a chance; a diff carrying paragraphs of external context does not.

## Difference from #690

That PR returns an empty string when the file cannot be read. This logs the failure first. An empty credential surfaces as an authentication error against the monitored service, which is a long way from the actual cause — and a monitoring tool failing quietly is the specific thing worth avoiding.

## Releasing

Tag `v<upstream>-<n>-secrets-backup` and push, or run the `publish-fork` workflow. It runs the config and backup tests before publishing to `ghcr.io/scyto/gatus`. (`v5.36.0-12-secrets` predates the backup.)

The tag mirrors `git describe`: `v5.36.0-12-secrets` means *twelve upstream commits past v5.36.0, plus this fork's change*. A running image therefore says exactly which upstream code it carries, and that it is not stock.

### Why not a clean release tag

**v5.36.0 does not build with current Go.** The image build fails with:

```
grpc@v1.81.1/internal/transport/handler_server.go:271:18: undefined: http2.TrailerPrefix
```

Upstream fixed it after cutting the release, in `chore(deps): bump golang.org/x/net to v0.58.0 to fix build on Go 1.27+` (#1787). Cherry-picking just that commit onto the v5.36.0 tag conflicts on `go.mod`, and resolving it by taking master's dependency set risks the Dockerfile's `go mod tidy -diff` step failing on dependencies the older tree does not use.

So this branch sits on upstream `master` rather than the release tag. When the next release ships with the fix included, rebase onto it and the tag goes back to a plain `v5.37.0-secrets` form.

## Rebasing onto a new upstream release

```bash
git fetch upstream --tags
git rebase v5.37.0          # expect a conflict only if config.go's expansion changes
#                           # (v5.36.0 needed master instead -- see above)
go test ./config/... ./storage/store/sql/...
git tag v5.37.0-secrets-backup && git push origin v5.37.0-secrets-backup
```
