# cw — the CWB platform CLI

One binary for humans and agents. Anchored on a single edge URL (the interchange
gateway); authenticates against herald and keeps the session fresh.

## Auth

    cw auth login --edge https://cwb.example --context prod    # human: prompts email + password
    cw auth login --agent --agent-id <id> --slug shadow        # agent: CW_OWNER_SEED in env
    cw auth whoami
    cw auth token            # print a fresh access token (scripting)
    cw auth status           # list contexts + freshness
    cw auth switch prod
    cw auth logout

A *context* is `{edge, identity}`. The refresh token is stored in the OS
keychain; the access token is cached (0600) and silently refreshed. Use
`--token`/`CW_TOKEN` to present a bearer directly (no stored state).

Command groups for cairn (`repo`/`pr`), ledger (`issue`), commonplace (`kb`),
and herald admin (`org`) build on this core and ship separately.
