# `cw agent pubkey` Implementation Plan (#9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add `identity.Fingerprint` + a local, no-network `cw agent pubkey --slug <slug>` that derives the agent keypair from `CW_OWNER_SEED`+slug and prints the base64-std pubkey + casket fingerprint.

**Architecture:** cw-only, offline. Reuses `casket.DeriveAgentKey`; a fingerprint helper mirroring herald's; a new subcommand under `cw agent`. No session/network.

**Tech Stack:** Go 1.26, cobra, `crypto/ed25519`+`crypto/sha256`+`encoding/base64`, `casket-go`.

Sub-project **#9**. Spec: `docs/superpowers/specs/2026-06-03-cw-agent-pubkey-design.md`.

## Verified facts

- `casket.DeriveAgentKey([]byte(seed), slug) (ed25519.PrivateKey, ed25519.PublicKey, error)` (deterministic). `cw agent create` derives the pubkey via `base64.StdEncoding.EncodeToString(pub)`.
- herald's fingerprint (its `internal/identity/fingerprint.go`): `base64.RawURLEncoding(sha256(pub)[:16])` — herald owns the convention; cw mirrors it.
- **Pinned test vector** (seed `cw-pubkey-test-seed`, slug `builder`): pubkey `YK99cMH64LflXEUjEHD38AMCrOStGKPE8uyj0GP28wI=`, fingerprint `vhkj2Fplk7uTkGzGSKDEJQ`.
- `internal/cli/agent/agent.go`: `NewCmd(gf *cmdutil.GlobalFlags)` adds `newKeygenCmd()` + `newCreateCmd(gf)`; imports `casket`, `base64`, `json`, `fmt`, `os`, `cmdutil`, `herald`, `cobra`. `create` reads `os.Getenv("CW_OWNER_SEED")` raw + has `gf.JSON`.
- `internal/identity/identity.go` imports `encoding/base64`, `casket`, etc. (NOT `crypto/ed25519`/`crypto/sha256`).

---

## Task 1: `internal/identity.Fingerprint`

**Files:**
- Create: `internal/identity/fingerprint.go`, `internal/identity/fingerprint_test.go`

- [ ] **Step 1: Write the failing test** — `internal/identity/fingerprint_test.go`:

```go
package identity

import (
	"encoding/base64"
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
)

func TestFingerprint(t *testing.T) {
	_, pub, err := casket.DeriveAgentKey([]byte("cw-pubkey-test-seed"), "builder")
	if err != nil {
		t.Fatal(err)
	}
	fp := Fingerprint(pub)
	// Pinned value (must match herald's base64url(sha256(pub)[:16])).
	if fp != "vhkj2Fplk7uTkGzGSKDEJQ" {
		t.Fatalf("fingerprint = %q, want vhkj2Fplk7uTkGzGSKDEJQ", fp)
	}
	// Format: base64url (no padding) of 16 bytes = 22 chars.
	if len(fp) != 22 {
		t.Fatalf("fingerprint length = %d, want 22", len(fp))
	}
	raw, err := base64.RawURLEncoding.DecodeString(fp)
	if err != nil || len(raw) != 16 {
		t.Fatalf("fingerprint not 16-byte base64url: %v (%d bytes)", err, len(raw))
	}
	// Deterministic.
	if Fingerprint(pub) != fp {
		t.Fatal("Fingerprint not deterministic")
	}
}
```

- [ ] **Step 2: Run — expect FAIL** — `cd /Users/jacinta/Source/cw && go test ./internal/identity/ -run TestFingerprint`
Expected: build error (`Fingerprint` undefined).

- [ ] **Step 3: Implement** — `internal/identity/fingerprint.go`:

```go
package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
)

// Fingerprint is the casket Ed25519 pubkey's stable identifier, matching
// herald's identity.Fingerprint: base64url(sha256(pubkey)[:16]). Deterministic.
// Herald owns this convention (its internal/identity/fingerprint.go); cw mirrors
// it so a locally-derived fingerprint matches herald's stored value + /api/me.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
```

- [ ] **Step 4: Run — expect PASS** — `cd /Users/jacinta/Source/cw && go test ./internal/identity/ -v && go build ./... && go vet ./internal/identity/`
Expected: `TestFingerprint` (+ existing identity tests) PASS; build + vet clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/identity/fingerprint.go internal/identity/fingerprint_test.go
git commit -m "identity: add Fingerprint (base64url(sha256(pub)[:16]), aligns with herald)"
```

---

## Task 2: `cw agent pubkey`

**Files:**
- Modify: `internal/cli/agent/agent.go` (add `newPubkeyCmd` + register + identity import)
- Modify: `internal/cli/agent/agent_test.go` (tests)

- [ ] **Step 1: Write the failing tests** — append to `internal/cli/agent/agent_test.go` (reuses its `bytes`/`encoding/base64`/`encoding/json`/`strings`/`testing` + `cmdutil`/`casket` imports; add `identity` import):

```go
func TestPubkey(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "cw-pubkey-test-seed")
	gf := &cmdutil.GlobalFlags{}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"pubkey", "--slug", "builder"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "YK99cMH64LflXEUjEHD38AMCrOStGKPE8uyj0GP28wI=") {
		t.Fatalf("missing pubkey:\n%s", s)
	}
	if !strings.Contains(s, "vhkj2Fplk7uTkGzGSKDEJQ") {
		t.Fatalf("missing fingerprint:\n%s", s)
	}
}

func TestPubkeyJSON(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "cw-pubkey-test-seed")
	gf := &cmdutil.GlobalFlags{JSON: true}
	cmd := NewCmd(gf)
	cmd.SetArgs([]string{"pubkey", "--slug", "builder"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pubkey --json: %v", err)
	}
	var got struct{ Slug, Pubkey, Fingerprint string }
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got.Slug != "builder" || got.Fingerprint != "vhkj2Fplk7uTkGzGSKDEJQ" ||
		got.Pubkey != "YK99cMH64LflXEUjEHD38AMCrOStGKPE8uyj0GP28wI=" {
		t.Fatalf("json: %+v", got)
	}
	// Cross-check against a direct derivation.
	_, pub, _ := casket.DeriveAgentKey([]byte("cw-pubkey-test-seed"), "builder")
	if got.Fingerprint != identity.Fingerprint(pub) {
		t.Fatalf("fingerprint mismatch vs identity.Fingerprint")
	}
}

func TestPubkeyRequiresSeed(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "")
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"pubkey", "--slug", "builder"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing-seed error")
	}
}

func TestPubkeyRequiresSlug(t *testing.T) {
	t.Setenv("CW_OWNER_SEED", "cw-pubkey-test-seed")
	cmd := NewCmd(&cmdutil.GlobalFlags{})
	cmd.SetArgs([]string{"pubkey"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing-slug error")
	}
}
```

Add the import `"github.com/CarriedWorldUniverse/cw/internal/identity"` to the test file.

- [ ] **Step 2: Run — expect FAIL** — `cd /Users/jacinta/Source/cw && go test ./internal/cli/agent/ -run TestPubkey`
Expected: FAIL — `pubkey` is an unknown command (cobra errors).

- [ ] **Step 3: Implement** — in `internal/cli/agent/agent.go`, add the `identity` import, register `newPubkeyCmd(gf)` in `NewCmd`, and add the func:

In `NewCmd`:
```go
	cmd.AddCommand(newKeygenCmd(), newCreateCmd(gf), newPubkeyCmd(gf))
```

Add the import `"github.com/CarriedWorldUniverse/cw/internal/identity"`, then:
```go
func newPubkeyCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "pubkey --slug <slug>",
		Short: "Derive an agent's casket pubkey + fingerprint from CW_OWNER_SEED (offline; no herald call)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" {
				return fmt.Errorf("--slug is required")
			}
			seed := os.Getenv("CW_OWNER_SEED")
			if seed == "" {
				return fmt.Errorf("agent pubkey requires the owner seed in CW_OWNER_SEED")
			}
			_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
			if err != nil {
				return fmt.Errorf("derive agent key: %w", err)
			}
			pubB64 := base64.StdEncoding.EncodeToString(pub)
			fp := identity.Fingerprint(pub)
			out := cmd.OutOrStdout()
			if gf.JSON {
				return json.NewEncoder(out).Encode(struct {
					Slug        string `json:"slug"`
					Pubkey      string `json:"pubkey"`
					Fingerprint string `json:"fingerprint"`
				}{slug, pubB64, fp})
			}
			fmt.Fprintf(out, "pubkey:      %s\nfingerprint: %s\n", pubB64, fp)
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "casket key slug (required)")
	return cmd
}
```

- [ ] **Step 4: Run — expect PASS** — `cd /Users/jacinta/Source/cw && go build ./... && go test ./internal/cli/agent/ -v && go test ./... && go vet ./...`
Expected: all four `TestPubkey*` + existing agent tests PASS; full suite green. `go run ./cmd/cw agent --help` lists `pubkey`; `go run ./cmd/cw agent pubkey --help` shows `--slug`.

- [ ] **Step 5: Commit**

```bash
cd /Users/jacinta/Source/cw && git add internal/cli/agent/
git commit -m "cw agent: pubkey (offline derive of casket pubkey + fingerprint)"
```

---

## Task 3: README + gated live drift-guard test

**Files:**
- Modify: `README.md`
- Create: `internal/cli/agent/pubkey_integration_test.go` (gated)

- [ ] **Step 1: README** — under `## Agents (herald admin)`, add:

```markdown
    cw agent pubkey --slug builder   # offline: derive this agent's casket pubkey + fingerprint from CW_OWNER_SEED

`cw agent pubkey` makes no network call. Its `fingerprint` matches herald's
stored value, so you can verify a local `CW_OWNER_SEED`+`--slug` derives an
already-registered agent by comparing it to `cw whoami --remote`.
```

- [ ] **Step 2: Gated live drift-guard test** — `internal/cli/agent/pubkey_integration_test.go` (package `agent`):

```go
package agent

import (
	"context"
	"os"
	"testing"

	casket "github.com/CarriedWorldUniverse/casket-go"
	"github.com/CarriedWorldUniverse/cw/internal/herald"
	"github.com/CarriedWorldUniverse/cw/internal/identity"
)

// TestLiveFingerprintMatchesHerald proves cw's local Fingerprint equals herald's
// stored value: log the provisioned agent in, fetch /api/me, and assert its
// fingerprint == identity.Fingerprint(DeriveAgentKey(seed, slug).pub). Guards
// against drift from herald's algorithm.
//
// Gated on CW_IT_EDGE + CW_IT_OWNER_SEED + CW_IT_AGENT_ID + CW_IT_AGENT_SLUG.
func TestLiveFingerprintMatchesHerald(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	seed := os.Getenv("CW_IT_OWNER_SEED")
	agentID := os.Getenv("CW_IT_AGENT_ID")
	slug := os.Getenv("CW_IT_AGENT_SLUG")
	if edge == "" || seed == "" || agentID == "" || slug == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_OWNER_SEED + CW_IT_AGENT_ID + CW_IT_AGENT_SLUG to run the live fingerprint test")
	}
	c, err := liveAgentClient(t, edge, seed, slug, agentID) // copy the helper from a sibling gated test (login.go agent path)
	if err != nil {
		t.Fatalf("agent login: %v", err)
	}
	ui, err := herald.Me(context.Background(), c)
	if err != nil {
		t.Fatalf("herald.Me: %v", err)
	}
	_, pub, err := casket.DeriveAgentKey([]byte(seed), slug)
	if err != nil {
		t.Fatal(err)
	}
	local := identity.Fingerprint(pub)
	if ui.Fingerprint != local {
		t.Fatalf("herald fingerprint %q != local %q (algorithm drift)", ui.Fingerprint, local)
	}
}
```

> **Implementer note:** implement `liveAgentClient(t, edge, seed, slug, agentID) (*client.Client, error)` in this file (or reuse one if `internal/cli/agent/integration_test.go` already exposes an agent-login helper) — mirror `internal/cli/auth/login.go`'s agent path: `oidc.New(edge)` → `TokenEndpoint(ctx)` → `identity.AgentAssertion([]byte(seed), slug, agentID, tu)` → `JWTBearerGrant(ctx, assertion)` → `client.WithStaticToken(edge, tok.AccessToken)`. (`internal/cli/auth/whoami_remote_integration_test.go` has this exact helper to copy.) Import only what you use; no identifier collision with `integration_test.go`'s `TestLiveAgent`/its helpers.

- [ ] **Step 3: Offline suite** — `cd /Users/jacinta/Source/cw && go build ./... && go vet ./... && go test ./...`
Expected: green; `TestLiveFingerprintMatchesHerald` SKIPs without `CW_IT_*`.

- [ ] **Step 4: Commit**

```bash
cd /Users/jacinta/Source/cw && git add -A
git commit -m "cw: README agent pubkey + gated live fingerprint drift-guard test"
```

- [ ] **Step 5: Controller — live smoke + merge** — provision an agent via cw (as cwadmin, like the #5/#6/#7b smokes), export `CW_IT_OWNER_SEED`/`CW_IT_AGENT_ID`/`CW_IT_AGENT_SLUG`, run `TestLiveFingerprintMatchesHerald` + a manual `cw agent pubkey --slug <slug>` vs `cw whoami --remote` fingerprint comparison. Then PR + merge.

---

## Self-review

**Spec coverage (#9):**
- `identity.Fingerprint(pub)` = base64url(sha256(pub)[:16]), mirroring herald → Task 1 (+ pinned-value unit test). ✔
- `cw agent pubkey --slug` (read CW_OWNER_SEED, derive, print pubkey + fingerprint; `--json`; fail-fast on missing seed/slug; offline) → Task 2. ✔
- never prints seed/private key (only pub + fp) → Task 2 (only pubB64/fp emitted). ✔
- README + gated live drift-guard (cw fp == herald /api/me fp) → Task 3. ✔

**Placeholder scan:** the `liveAgentClient` helper is the one copy-from-sibling spot (the exact code is in `whoami_remote_integration_test.go`); pinned test vectors are concrete.

**Type consistency:** `identity.Fingerprint(ed25519.PublicKey) string`; `newPubkeyCmd(*cmdutil.GlobalFlags)` mirrors `newCreateCmd` (gf for JSON, no Session); `casket.DeriveAgentKey`; `cmd.OutOrStdout()`. Consistent with the agent group + the keygen/create pattern.
