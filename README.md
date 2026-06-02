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

## Repos & PRs (cairn)

    cw repo create widgets                 # in your org
    cw repo list
    cw repo clone <org>/widgets [dir]      # shells git with a fresh bearer
    cw pr create --repo <org>/widgets --head feat --base main \
        --title "Add X" --project NEX [--body ...] [--dod ...]
    cw pr list  --repo <org>/widgets [--state open|merged|all]
    cw pr view  7 --repo <org>/widgets
    cw pr merge 7 --repo <org>/widgets     # fast-forward only

Inside a `cw repo clone`d directory the `--repo` flag is inferred from `origin`.
A bare `<slug>` uses your context's org; `<org>/<slug>` or `--org` targets another.

**Pushing** (no `cw push` yet): from a clone,

    git -c http.extraHeader="Authorization: Bearer $(cw auth token)" push

Command groups for ledger (`issue`), commonplace (`kb`), and herald admin
(`org`) build on this core and ship separately.
