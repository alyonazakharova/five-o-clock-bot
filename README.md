# Five o'clock bot ☕

Telegram bot that picks a random group member every weekday at 17:00 and posts a
funny "please make tea" message with two buttons for the chosen one:

- **🏝️ Absent** — they're skipped for the rest of the day and someone else is picked.
- **☕ Tea is ready** — the bot invites everyone to come have some tea.

Message texts live in [`messages.go`](messages.go); edit them freely.

## Commands

| Command    | What it does                                     |
|------------|--------------------------------------------------|
| `/tea`     | Pick someone right now (works on weekends too)   |
| `/members` | Show who can be picked and who is excluded       |
| `/chatid`  | Print the chat id (for setup)                    |

## Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy the token.
2. In BotFather run `/setprivacy` → your bot → **Disable**, so the bot sees group messages.
3. Add the bot to the group and **make it an admin** (it needs this to see join/leave events).
4. `cp .env.example .env`, set `BOT_TOKEN`, leave `CHAT_ID` empty, run the bot and send `/chatid` in the group.
5. Put the id in `CHAT_ID` (group ids are negative, e.g. `-1001234567890`) and restart.

### Who gets picked

Everyone in the group except bots and `EXCLUDE` is in the rotation. Telegram doesn't let
bots download the member list, though, so the bot learns members as it sees them:

- group admins (fetched on start),
- anyone who writes anything in the group,
- people joining the group while the bot is in it.

Someone who has never written since the bot was added can't be picked yet — ask quiet
people to send any message once. People who leave the group are removed.
The same person isn't picked twice in a row within a day unless they're the only one left.

### Configuration (`.env`)

| Variable        | Default              | Description                                             |
|-----------------|----------------------|---------------------------------------------------------|
| `BOT_TOKEN`     | —                    | Token from BotFather (required)                         |
| `CHAT_ID`       | —                    | Group id; without it only `/chatid` works               |
| `EXCLUDE`       | —                    | Comma-separated usernames (`@` optional) or user ids     |
| `DUTY_TIME`     | `17:00`              | Pick time on weekdays (Mon–Fri), `HH:MM`                 |
| `TZ`            | server timezone      | IANA timezone, e.g. `Europe/Moscow`                      |
| `STATE_PATH`    | `./data/state.json`  | Members and today's round are stored here               |
| `HTTP_ADDR`     | — (off)              | Address for the HTTP API, e.g. `:8080`                   |
| `API_TOKEN`     | —                    | Token the HTTP API requires (strongly recommended)      |

## HTTP API

Set `HTTP_ADDR=:8080` and `API_TOKEN` to let other apps see who's on tea duty.
Send the token as `Authorization: Bearer <token>` or `?token=<token>`.

`GET /api/duty`

```json
{
  "version": 12,
  "status": "assigned",
  "date": "2026-10-14",
  "assignment_id": 5,
  "assignee": { "id": 123456789, "username": "alice", "name": "Alice Smith" },
  "assigned_at": "2026-10-14T17:00:01+02:00",
  "absent": [{ "id": 987654321, "username": "bob", "name": "Bob" }]
}
```

- `status`: `idle` (nothing picked yet), `assigned` (waiting for tea), `ready` (tea is ready,
  `ready_at` is set), `nobody` (everyone was absent).
- `assignment_id` goes up every time a new person is picked — compare it to spot a new assignee.
- `version` goes up on every change (new pick, absent, tea ready).

**Long polling.** Pass the last `version` you saw as `since`; the request waits until something
changes (or `wait` seconds pass, default 30, max 60) and then returns the current state:

```bash
curl -H "Authorization: Bearer $API_TOKEN" "http://your-server:8080/api/duty?since=12&wait=60"
```

A client loop: call without `since` once, then keep calling with `since=<version from last
response>`; whenever `assignment_id` changes, a new person was picked.

`GET /healthz` returns `ok` (no token needed).

### Making it reachable from outside

**Quick: open the port.** The bot listens on all interfaces, so open 8080 in the firewall:

```bash
sudo ufw allow 8080/tcp
```

Then use `http://<server-ip>:8080/api/duty`. Also allow the port in your hosting provider's
firewall / security group if it has one. Plain HTTP sends the token unencrypted, so use this
only for testing.

**Better: HTTPS with a domain via Caddy.** Point a DNS A record (e.g. `tea.example.com`) at the
server, set `HTTP_ADDR=127.0.0.1:8080` so the bot is only reachable through Caddy (for Docker,
change the compose port to `"127.0.0.1:8080:8080"` instead), then:

```bash
sudo apt install -y caddy
```

`/etc/caddy/Caddyfile`:

```
tea.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

```bash
sudo ufw allow 80,443/tcp && sudo systemctl reload caddy
```

Caddy gets a Let's Encrypt certificate automatically; the API is at
`https://tea.example.com/api/duty`.

## Run

**Docker (recommended):**

```bash
docker compose up -d --build
```

State is kept in `./data`.

**Plain binary + systemd:**

```bash
make linux   # -> bin/five-o-clock-bot (use GOARCH=arm64 for ARM servers)
scp bin/five-o-clock-bot .env five-o-clock-bot.service your-server:/tmp/
```

On the server:

```bash
sudo mkdir -p /opt/five-o-clock-bot
sudo mv /tmp/five-o-clock-bot /tmp/.env /opt/five-o-clock-bot/
sudo mv /tmp/five-o-clock-bot.service /etc/systemd/system/
sudo systemctl enable --now five-o-clock-bot
journalctl -u five-o-clock-bot -f
```

**Locally:** `set -a; source .env; set +a; go run .`

No third-party dependencies — just the Go standard library (Go 1.23+).
