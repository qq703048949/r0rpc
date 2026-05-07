# Xposed Usage Notes

Recommended pattern:

1. In your Xposed entry class, start a background thread after the target app is ready.
2. Create one `RelayClient` instance per process.
3. Let the client log in once and keep one WebSocket session alive in the background.
4. Register each RPC action as a thin adapter around your real hook logic.
5. Keep action names stable because the backend monitoring layer groups by `group + action + clientId`.
6. Use a deterministic `clientId`, such as `brand-model-androidId` or your own device fingerprint.

Suggested action naming style:

- `account.profile`
- `order.query`
- `message.send`

If your hook logic depends on UI thread access, let the action post into the main thread and block until the result is ready.