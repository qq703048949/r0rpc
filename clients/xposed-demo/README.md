# Xposed Demo Project

This is the Android/Xposed demo project.

## Important files

- bundled relay jar: `app/libs/r0rpc-relay-client.jar`
- xposed API jar: `app/libs/xposed-api-82.jar`
- demo app code: `app/src/main/java`

## Build

From this directory run:

```bash
./gradlew assembleDebug
```

Windows-specific startup flow is no longer part of the main project guidance.
This demo is kept as a client integration example only.

## What to change before building

1. update your target package and hook entry
2. replace server address with your Linux deployment address
3. replace client credentials if needed

## Demo actions

- `ping`
- `decrypt`

