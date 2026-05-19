# Twilio SMS setup

> **5-minute portability promise**: any operator with a Twilio account can get request-sudo's SMS approval flow working end-to-end with three env vars and zero console template rituals. No A2P 10DLC registration. No custom-template approval cycle. No Twilio sales contact.

## What you need

1. A Twilio account — free trial works for testing; paid for production.
2. A Twilio **Verify Service** (Twilio Console → Verify → Services → Create). Name it whatever you want — the name you give it appears in the SMS body as `"Your <NAME> verification code is: 482910"`. Pick a friendly_name your operators will recognize (e.g. `"request-sudo"`, `"<your-company>-ops"`).
3. The three identifiers Twilio gives you:
   - Account SID (starts `AC...`)
   - Auth Token
   - Verify Service SID (starts `VA...`)

## Configure

Drop three env vars into `/etc/request-sudo/twilio.env`, mode `0600`:

```bash
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_VERIFY_SERVICE_SID=VAxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Map operator phones in `/etc/request-sudo/approvers.yaml`:

```yaml
approver_sets:
  operators:
    - alice
    - bob

approvers:
  alice:
    phones: ["+15551234567"]
  bob:
    phones: ["+15557654321"]

routing:
  myapp:
    approver_set: operators
    max_execution_seconds: 300
```

Start the adapter:

```bash
request-sudo-twilio-adapter \
  --env-file /etc/request-sudo/twilio.env \
  --approvers /etc/request-sudo/approvers.yaml \
  --event-log /var/lib/request-sudo/canon/events.jsonl \
  --review-socket /run/request-sudo/review.sock
```

Done. A submitted request triggers an SMS like `"Your <Service name> verification code is: 482910"` to the operator. The operator looks up the request via `request-sudoctl pending` to see the argv, then replies `"482910"`. The adapter calls Twilio VerificationCheck; on `approved`, the broker executes.

## Why this works without 10DLC paperwork

The Twilio Messages API requires A2P 10DLC brand+campaign registration to send to US numbers from an unregistered long code (1–3 weeks, ~$44 setup, ~$1.50/month). Twilio **Verify** routes SMS through Twilio's pre-registered managed sender pool — no per-operator registration needed. Same Twilio account, different sending path.

## Deliverability tiers (honest)

| Operator scenario | Status | Notes |
|---|---|---|
| US-registered Twilio (10DLC complete) | Full throughput | Same path moltpod airlock uses daily |
| US-unregistered Twilio (default new account) | ~60–200 sends/day | Adapter's `awaiting_local_approval` handles throttle. Sufficient for most ops volumes. |
| International recipients | Per-country | Configure `allowed_country_codes`; default `["+1"]`. India needs DLT registration. China blocked. EU is GDPR-clean. |

## What the SMS looks like

The SMS body is the Twilio Verify default template:

```
Your <Verify Service friendly_name> verification code is: 482910
```

The Service's friendly_name is set when you create the Verify Service in the Twilio Console. **Per-request `CustomFriendlyName` is rejected on Verify Services configured without explicit allow** (Twilio error `60204` — we observed this on the moltpod prod account). Set the Service friendly_name to something your operators will recognize at the Console.

**The SMS does not carry argv.** Operators see `"Your request-sudo verification code is: 482910"` and must look up what they're approving via `request-sudoctl pending` before replying. This is intentional: Twilio's content moderation blocks command-like keywords (`restart`, `delete`, `sudo`) in friendly_name, so trying to carry argv is brittle. Treat the SMS as a notification + cryptographic gate, not as a complete approval surface.

## Reply flow

1. Operator gets SMS, reads code (e.g. `482910`).
2. Operator runs `request-sudoctl pending` to see what's waiting.
3. Operator replies to the SMS with just the code: `482910` (or `yes 482910` / `no 482910`).
4. Twilio forwards the reply to the adapter's webhook (must be publicly reachable — see "Webhook setup" below).
5. Adapter validates the code via Twilio VerificationCheck, then calls broker's `review.approve` or `review.deny`.

If you don't have a public webhook URL: operator can also approve locally via `request-sudoctl approve <request_id> --code <code>`. The adapter still validates against Twilio VerificationCheck.

## Webhook setup (optional)

The adapter listens on a localhost-only port for inbound Twilio webhooks. For real SMS reply roundtrip:

1. Expose the adapter's `/twilio/inbound` endpoint via nginx, ngrok, Cloudflare Tunnel, or similar — pick whatever fits your deployment
2. In Twilio Console → Verify Service → Webhooks, paste the public URL
3. The adapter validates `X-Twilio-Signature` (HMAC-SHA1) on every inbound; spoofed webhooks are rejected with 401

If you skip this step, the adapter still works — operators just approve via the local `request-sudoctl approve` path with the code from their SMS.

## Race-freedom guarantee

The adapter enforces **at most one pending Verify per recipient phone at any time**. If two requests route to the same phone within the 10-minute Verify session TTL, the second goes to `awaiting_local_approval` until the first resolves. This eliminates cross-request approval ambiguity by construction: when the operator's reply arrives, there is exactly one pending request bound to that phone.

## Dual-mode invariant (release-blocker)

request-sudo MUST run end-to-end (submit → approve → execute → canon verify) under all three:

1. No `request-sudo-twilio-adapter` binary installed — broker still works, local `request-sudoctl approve` still works
2. No `/etc/request-sudo/twilio.env` file — adapter doesn't have to start
3. No `phones:` entries in approvers.yaml — routing falls back to local-only with `sms_routing_empty` canon event

The Twilio adapter is additive, never required.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Twilio API returns `60204: Custom friendly name not allowed` | Verify Service is configured to lock friendly_name | Adapter falls back to default friendly_name automatically; no action needed |
| SMS arrives but `Messages API` shows `status=undelivered, error_code=30034` | You're using the legacy Messages-API path; A2P 10DLC unregistered | Switch to Verify (this guide); restart adapter |
| `60410` / `60226` rate-limit | Unregistered account caps | Wait for rolling window, or complete A2P 10DLC registration for full throughput |
| Operator sees `"Your <wrong-brand> verification code is: ..."` | Your Verify Service was created with a different friendly_name | Edit the Service in Twilio Console; new SMSes use the updated name |
