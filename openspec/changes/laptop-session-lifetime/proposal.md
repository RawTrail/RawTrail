## Why

A laptop install's owner signs in through the break-glass ceremony, so their session gets the recovery limits, 15 minutes idle and an hour at most, and the console signs them out constantly (#1613). On a server those limits are right, because break-glass is an emergency path. On a laptop it is the only way in, and the console is reached from the same machine.

## What Changes

- On a laptop install, a break-glass session lasts 7 days idle and 30 days at most, unless `EDR_BREAKGLASS_SESSION_IDLE_TIMEOUT` or `EDR_BREAKGLASS_SESSION_ABSOLUTE_TIMEOUT` is set.
- A server install keeps 15 minutes and an hour. The reauth window for sensitive writes is unchanged on both.

## Impact

- `server/config/config.go`: the laptop defaults, applied after the profile is read.
- `docs/install-server.md` and `docs/breakglass.md`: the defaults per profile.
