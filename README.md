# interactions

Discord interactions service for PurgeBot. Receives slash commands, modals, and component interactions via HTTP and routes them to the appropriate handlers.

## Responsibilities

- Exposes an HTTP endpoint (`POST /interactions`) that Discord delivers interactions to
- Verifies Ed25519 signatures on every incoming request
- Registers global slash commands on startup
- Handles `/purge`, `/help`, `/stats`, and `/customize` commands
- Handles autocomplete for the `target_id` option on `/purge`
- Handles the `customize_modal` modal submission
- Handles purge cancellation and channel-skip UI via `cancel:*` and `skip:*` components
- Exposes `GET /health` for container health checks

## Configuration

All configuration is loaded from environment variables (see `.env.example` in the docker repo).

| Variable                                   | Description                                            |
| ------------------------------------------ | ------------------------------------------------------ |
| `DISCORD_TOKEN`                            | Bot token                                              |
| `DISCORD_PUBLIC_KEY`                       | Ed25519 public key for signature verification          |
| `DISCORD_APPLICATION_ID`                   | Application ID                                         |
| `DATABASE_*`                               | PostgreSQL connection                                  |
| `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB` | Redis connection                                       |
| `INTERACTIONS_ADDR`                        | Listen address (default `:8080`)                       |
| `PREMIUM_SKU_ID`                           | Discord premium SKU ID (optional)                      |
| `FREE_PREMIUM_GUILD_IDS`                   | Comma-separated guild IDs with free premium (optional) |
| `SENTRY_DSN`                               | Sentry error reporting (optional)                      |
| `LOG_LEVEL`                                | `debug`, `info`, `warn`, `error`                       |
| `LOG_JSON`                                 | `true` for JSON log output                             |
