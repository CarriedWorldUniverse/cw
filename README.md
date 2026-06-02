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

The herald admin command group (`org`) builds on this core and ships separately.

## Issues (ledger)

    cw issue create --project NEX --type Story --title "Add X" [--body ...] [--dod ...] [--priority ...]
    cw issue list   [--mine | --ready | --project NEX]      # --mine is the default
    cw issue view   NEX-12
    cw issue claim  NEX-12
    cw issue transition NEX-12 "In Review"
    cw issue comment NEX-12 "looks good"

Issues are scoped to your org by the token (no --org). Needs an identity with
`issue:read`/`issue:write`/`issue:claim`.

## Knowledge (commonplace)

    cw kb store --topic onboarding [--visibility org|private] [--tag x] < doc.md
    echo "..." | cw kb store --topic notes
    cw kb search "how does auth work" [--top-k 5]   # semantic (returns full entries)
    cw kb list

Knowledge is scoped to your org by the token (no --org). Needs an identity with
`knowledge:read`/`knowledge:write`. `store` reads content from `--content` or stdin.

## Orgs (herald admin)

    cw org create acme [--product cairn --product ledger]
    cw org list
    cw org products <org-id>
    cw org enable  <org-id> ledger
    cw org disable <org-id> ledger
    cw org delete  <org-id> --confirm acme      # --confirm must equal the org name

## Humans (herald admin)

    cw human create --org <org-id> --name alice \
        --scope knowledge:read --scope knowledge:write --password-stdin <<< "$PW"
    cw human set-password <human-id> --password-stdin <<< "$PW"   # else prompts no-echo

Org and identity admin require a platform-admin (`herald:platform-admin`) or
org-admin (`herald:org-admin`) bearer. Passwords are read from stdin
(`--password-stdin`) or an interactive prompt — never a plaintext flag.
Provisioning a working identity end to end:

    ORG=$(cw org create acme)
    H=$(cw human create --org "$ORG" --name alice --scope knowledge:read --password-stdin <<< "$PW")
    cw auth login --edge <edge>      # log in as $H
