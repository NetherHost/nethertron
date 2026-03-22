# Discord Bot

A Discord bot built in Go with ticket system functionality.

## Configuration

Secrets and your hosting panel origin are read from the environment (not committed to the repo). Copy `.env.example` to `.env`, fill in values, then export them before starting the bot:

| Variable | Description |
|----------|-------------|
| `DISCORD_BOT_TOKEN` | Discord bot token from the [Developer Portal](https://discord.com/developers/applications) |
| `DISCORD_BOT_API_KEY` | Shared secret your backend (e.g. Laravel) sends as `X-API-Key` |
| `PANEL_BASE_URL` | Base URL of your panel (no trailing slash), e.g. `https://panel.example.com` — used for status counts, dashboard links, and user service API calls. The bot calls `GET /api/v3/users/count`; include optional `online_count` in that JSON to alternate the “active users” presence line (otherwise only the total is shown). |

Discord channel, role, and guild IDs are placeholders in `config.go`; replace them with your server’s IDs before production use.

```bash
export DISCORD_BOT_TOKEN='...'
export DISCORD_BOT_API_KEY='...'
export PANEL_BASE_URL='https://your-panel.example.com'
go run .
```

## Running the Bot

Since this project uses multiple Go files in the same package, you need to run:

```bash
go run .
```

Or use the provided script (after exporting the variables above):
```bash
./run.sh
```

**Note:** `go run main.go` won't work because it only compiles `main.go` and ignores other files in the package.

## Project Structure

- `main.go` - Entry point and bot initialization
- `commands.go` - Command registration and interaction handling
- `handlers.go` - All ticket handler functions
- `database.go` - Database operations (SQLite)
- `models.go` - Data structures
- `config.go` - Constants and configuration
- `status.go` - Status update functions
- `utils.go` - Utility functions

## Building

To build the bot:
```bash
go build -o bot
./bot
```
