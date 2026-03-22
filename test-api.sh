#!/bin/bash
# Uses DISCORD_BOT_API_KEY from the environment (same as the bot). Example:
#   export DISCORD_BOT_API_KEY='your-secret'
#   ./test-api.sh

API_KEY="${DISCORD_BOT_API_KEY:-}"
if [[ -z "$API_KEY" ]]; then
  echo "Set DISCORD_BOT_API_KEY to match the bot's DISCORD_BOT_API_KEY, then re-run."
  exit 1
fi

echo "Testing Discord Bot API endpoints..."
echo ""

echo "1. Testing health endpoint on localhost:8080"
curl -v http://localhost:8080/health 2>&1 | head -20
echo ""
echo ""

echo "2. Testing health endpoint on 127.0.0.1:8080"
curl -v http://127.0.0.1:8080/health 2>&1 | head -20
echo ""
echo ""

echo "3. Testing /api/discord/account endpoint (should fail with 401 - no API key)"
curl -v -X POST http://localhost:8080/api/discord/account \
  -H "Content-Type: application/json" \
  -d '{"discord_id":"test123","email":"test@example.com","action":"link"}' 2>&1 | head -20
echo ""
echo ""

echo "4. Testing /api/discord/account endpoint with API key"
curl -v -X POST http://localhost:8080/api/discord/account \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${API_KEY}" \
  -d '{"discord_id":"test123","email":"test@example.com","action":"link"}' 2>&1 | head -30
