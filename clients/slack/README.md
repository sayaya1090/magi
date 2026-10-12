# Magi for Slack

This Socket Mode app provides Magi chat in Slack DMs and mention threads, plus an App Home introduction. Each workspace, user, channel and thread gets its own Magi binding. The agent helps read conversations and prepare drafts; its registered Slack tools cannot send messages to other channels or run code.

## Setup

1. Create a Slack app and enable Socket Mode. Create an app-level token with `connections:write`.
2. Grant bot scopes `app_mentions:read`, `chat:write`, `im:history`. Reading channel threads additionally needs `channels:history` or `groups:history`; invite the bot to channels it should read. Do not grant administrator scopes.
3. Subscribe to `app_mention`, `message.im` and `app_home_opened`. Enable the Home tab and Messages tab. Enable Interactivity for approval buttons.
4. Install the app to your workspace. Supply `SLACK_APP_TOKEN` and `SLACK_BOT_TOKEN` through your local secret manager or environment. Never commit them.
5. With the `magi` executable on PATH and your provider configured, run `go run ./clients/slack/cmd`.

Send a DM or mention `@Magi` in a channel. Replies stay in the initiating thread. Approval and option buttons work only for the user who started that thread. Question reports are included as decision grounds, within the displayed text limit. Questions without options accept `답변 REQUEST_ID answer text` in that same thread. For DMs, use Slack's reply-in-thread action to continue an existing conversation; a new top-level DM creates a new binding.

## Tools and limits

- `slack_read_thread` reads up to 30 messages from the bound thread and reports whether more exist. Text is limited to 1,200 characters per message and 12,000 total; truncation is explicit. It does not follow links, download attachments or search other channels.
- `slack_draft_message` returns a draft with `sent: false`. No delivery tool is registered.
- Replies are capped at 2,800 characters. Long output is explicitly marked as truncated. Bot events and edited-message events do not initiate prompts.

Socket Mode authenticates inbound delivery; opaque approval tokens are single-use and bound to team, user, channel and thread. Event-ID deduplication retains the latest 4,096 IDs in memory. Bindings, deduplication and pending buttons do not survive process restart. Failed deliveries keep the current event pending and retry once per second; cursors advance only after successful delivery. A network timeout after Slack accepted a post can still produce a duplicate, since Slack delivery has no transaction with Magi. Failed approval submissions preserve the button for retry. Historical events from before a newly activated connection are not reposted. At most 256 conversation bindings are held until restart. This client does not implement persistent Slack delivery retries (temporary rate limits are retried up to three times), file uploads or organization-wide installation. API errors are logged without token values or Slack response bodies.

## Verification

Run `go test -race ./clients/slack/...`. Mock transports test thread isolation, replay suppression, absence of unsolicited messages, bound single-use approvals and read/draft tool behavior. There is no browser surface to test. A real Slack workspace, rate limiting and token revocation have not been tested; complete those checks before production deployment.

## Official API review (2026-10-12)

Reviewed [Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/), [App Home events](https://docs.slack.dev/reference/events/app_home_opened/), [mentions](https://docs.slack.dev/reference/events/app_mention/), [message events](https://docs.slack.dev/reference/events/message/), [Block Kit actions](https://docs.slack.dev/reference/interaction-payloads/block_actions-payload/), [thread replies](https://docs.slack.dev/reference/methods/conversations.replies/) and [rate limits](https://docs.slack.dev/apis/web-api/rate-limits/).

Envelope acknowledgement is sent before model work; Socket Mode needs no public callback URL. Home views publish only for `tab: home`. Interactive buttons use `action_id` and opaque `value`, with team/user/channel/thread checks. The app handles `message.im`, not group DMs. Free answers use ordinary text starting with `답변`, because `/answer` can be intercepted by Slack as an unregistered slash command.

Posts are scheduled at most once per second per channel. HTTP 429 honors the full `Retry-After` interval, including waits longer than 30 seconds; cancellation interrupts the wait. Current official thread-read documentation lists bot and user history scopes. Internal customer-built apps retain Tier 3; newly commercially distributed non-Marketplace apps have a one-request-per-minute and 15-item limit from May 29, 2025. This client targets internal installations and requests 30 items, with no automatic pagination. Do not use that internal rate assumption for commercial distribution. Socket Mode apps cannot currently be listed in the public Slack Marketplace.

## Execution controls

Send `magi stop` or `Magi 중단` in the same Magi conversation thread to request interruption. `magi model model-name` or `Magi 모델 모델이름` changes the model for that binding. Mention @Magi in channels. These are ordinary message commands and require no Slack slash-command registration. They are not sent as model prompts and cannot control another user or thread. Start a new session with a new top-level DM or mention thread.
