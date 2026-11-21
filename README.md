# interactions

Discord interactions service for PurgeBot. Receives slash commands, modals, and component interactions via HTTP and routes them to the appropriate handlers.

## Responsibilities

- Exposes an HTTP endpoint (`POST /interactions`) that Discord delivers interactions to
- Verifies the Ed25519 signature of every `POST /interactions` request
- Registers global slash commands on startup; `cmd/register` does the same as a one-off
- Handles `/purge`, `/cancel`, `/help`, `/stats`, `/customize`, and `/settings` commands
- Checks that the bot has View Channel, Read Message History and Manage Messages in every target channel, then claims the guild's single active-purge slot and queues the job in Redis for the purger
- Requires Premium for `/customize` and `/stats`: a guild listed in `FREE_PREMIUM_GUILD_IDS` or holding an active entitlement to `PREMIUM_SKU_ID`, cached for two minutes
- Lets only the server owner and members holding a role the owner granted use `/purge`, `/customize`, `/stats` and `/settings`, and refuses when a lookup fails unless the other already confirmed access; `/help` and every way to stop a purge stay open
- Lets the server owner grant and revoke roles with `/settings access add` and `remove`, and anyone with access list them with `/settings access list` and turn purge stats on or off with `/settings stats`; turning them off deletes the server's recorded stats
- Handles autocomplete for the `target` option on `/purge`
- Handles the `customize_modal` modal submission
- Handles purge cancellation and channel-skip UI via `cancel:*` and `skip:*` components
- Authorises a cancel on job ownership or server ownership, never on a permission
- Exposes `GET /health` for container health checks

## Configuration

All configuration is loaded from environment variables (see `.env.example` in the docker repo).

| Variable                 | Description                                              |
| ------------------------ | -------------------------------------------------------- |
| `DISCORD_TOKEN`          | Bot token (required)                                     |
| `DISCORD_PUBLIC_KEY`     | Ed25519 public key for signature verification (required) |
| `DISCORD_APPLICATION_ID` | Application ID (required)                                |
| `DATABASE_HOST`          | PostgreSQL host (default `localhost`)                    |
| `DATABASE_PORT`          | PostgreSQL port (default `5432`)                         |
| `DATABASE_NAME`          | Database name (default `purgebot`)                       |
| `DATABASE_USER`          | Database user (default `purgebot`)                       |
| `DATABASE_PASSWORD`      | Database password                                        |
| `REDIS_ADDR`             | Redis address (default `localhost:6379`)                 |
| `REDIS_PASSWORD`         | Redis password                                           |
| `REDIS_DB`               | Redis database index (default `0`)                       |
| `INTERACTIONS_ADDR`      | Listen address (default `:8080`)                         |
| `PREMIUM_SKU_ID`         | Discord premium SKU ID (optional)                        |
| `FREE_PREMIUM_GUILD_IDS` | Comma-separated guild IDs with free premium (optional)   |
| `SENTRY_DSN`             | Sentry-compatible error reporting (optional)             |
| `LOG_LEVEL`              | `debug`, `info`, `warn`, `error` (default `info`)        |
| `LOG_JSON`               | `true` for JSON log output                               |
