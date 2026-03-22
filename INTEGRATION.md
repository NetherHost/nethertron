# Discord Bot Integration Guide

This guide explains how to integrate your Laravel application with the Discord bot to sync Discord account emails.

## Overview

When a user links their Discord account on your website, your Laravel app should notify the Discord bot via an API endpoint. The bot will then store the Discord ID → Email mapping and display it in support tickets.

## API Endpoint

The bot exposes an HTTP API endpoint that your Laravel app can call:

**Endpoint:** `POST http://your-bot-server:8080/api/discord/account`  
(Port matches `APIServerPort` in `config.go`; change either side to match your deployment.)

**Headers:**
- `Content-Type: application/json`
- `X-API-Key: <same value as DISCORD_BOT_API_KEY on the bot>`

## Request Format

### Link Discord Account

```json
{
  "discord_id": "987654321098765432",
  "email": "user@example.com",
  "action": "link"
}
```

### Unlink Discord Account

```json
{
  "discord_id": "987654321098765432",
  "action": "unlink"
}
```

## Laravel Integration

Update your `handleDiscordCallback` method in your Volt component to notify the bot:

```php
public function handleDiscordCallback($code)
{
    // ... existing code ...

    $discordUser = $userResponse->json();

    // Create or update Discord account
    $this->discordAccount = DiscordAccount::updateOrCreate(
        ['user_id' => $this->user->id],
        [
            'discord_id' => $discordUser['id'],
            'discord_username' => $discordUser['username'],
            'discord_access_token' => $tokens['access_token'],
            'discord_refresh_token' => $tokens['refresh_token'] ?? null
        ]
    );

    // Notify Discord bot about the account link
    Http::withHeaders([
        'X-API-Key' => env('DISCORD_BOT_API_KEY'),
        'Content-Type' => 'application/json'
    ])->post(env('DISCORD_BOT_API_URL') . '/api/discord/account', [
        'discord_id' => $discordUser['id'],
        'email' => $this->user->email,
        'action' => 'link'
    ]);

    // Add user to Discord server
    Http::withHeaders([
        'Authorization' => 'Bot ' . env('DISCORD_BOT_TOKEN')
    ])->put('https://discord.com/api/guilds/' . env('DISCORD_GUILD_ID') . '/members/' . $discordUser['id'], [
        'access_token' => $tokens['access_token']
    ]);

    session()->flash('toast_message', 'Discord account connected successfully!');
    session()->flash('toast_type', 'success');
}
```

Update your `unlinkDiscordAccount` method:

```php
public function unlinkDiscordAccount()
{
    // ... existing validation code ...

    // Notify Discord bot about the account unlink BEFORE deleting
    if ($this->discordAccount && $this->discordAccount->discord_id) {
        Http::withHeaders([
            'X-API-Key' => env('DISCORD_BOT_API_KEY'),
            'Content-Type' => 'application/json'
        ])->post(env('DISCORD_BOT_API_URL') . '/api/discord/account', [
            'discord_id' => $this->discordAccount->discord_id,
            'action' => 'unlink'
        ]);
    }

    // Revoke Discord token
    if ($this->discordAccount && $this->discordAccount->discord_access_token) {
        Http::asForm()->post('https://discord.com/api/oauth2/token/revoke', [
            'token' => $this->discordAccount->discord_access_token,
            'client_id' => env('DISCORD_CLIENT_ID'),
            'client_secret' => env('DISCORD_CLIENT_SECRET'),
        ]);
    }

    // Delete the Discord account record
    if ($this->discordAccount) {
        $this->discordAccount->delete();
        $this->discordAccount = null;
    }

    // ... rest of the code ...
}
```

## Environment Variables

Add these to your Laravel `.env` file:

```env
DISCORD_GUILD_ID=your_discord_server_snowflake_id
DISCORD_BOT_API_URL=http://your-bot-server:8080
DISCORD_BOT_API_KEY=generate_a_long_random_secret
```

On the bot host, use the same `DISCORD_BOT_API_KEY` plus `DISCORD_BOT_TOKEN` and `PANEL_BASE_URL` (see `.env.example` and README).

**Important:** Replace `your-bot-server` with the actual IP address or domain where your Discord bot is running.

## How It Works

1. User links Discord account on your website
2. Laravel calls the bot's API endpoint with Discord ID and email
3. Bot stores the mapping in its database
4. When user creates a ticket, bot looks up their email by Discord ID
5. Email is displayed in the ticket embed instead of "Not linked"

## Testing

You can test the API endpoint using curl (set `API_KEY` to your bot's `DISCORD_BOT_API_KEY`):

```bash
# Link account
curl -X POST http://localhost:8080/api/discord/account \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "discord_id": "987654321098765432",
    "email": "test@example.com",
    "action": "link"
  }'

# Unlink account
curl -X POST http://localhost:8080/api/discord/account \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "discord_id": "987654321098765432",
    "action": "unlink"
  }'

# Health check
curl http://localhost:8080/health
```

## Security Notes

- The API endpoint requires the `X-API-Key` header to match the bot's `DISCORD_BOT_API_KEY`
- Make sure your bot server is only accessible from your Laravel server (use firewall rules)
- Consider using HTTPS in production
- The API server port is set in `config.go` (`APIServerPort`)
