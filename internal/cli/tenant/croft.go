package tenant

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	casket "github.com/CarriedWorldUniverse/casket-go"
	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cwb-client/herald"
	"github.com/CarriedWorldUniverse/cwb-client/identity"

	cwkeyfile "github.com/CarriedWorldUniverse/cw/internal/keyfile"
)

// croftSlug is the casket key slug + agent display name for an org's managing AI.
const croftSlug = "croft"

// defaultBrokerSeam is the nexus broker/agora seam a croft connects through; the
// croft pod reaches it as CW_SEAM_URL. Overridable via --broker-seam.
const defaultBrokerSeam = "https://nexus.tail41686e.ts.net:7888"

// SecretWriter persists a k8s secret. Abstracted so provisioning is unit-testable
// without a cluster; the live impl applies via kubectl.
type SecretWriter interface {
	WriteSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
}

// croftResult is what provisionCroft reports — the herald identity, never the seed.
type croftResult struct {
	AgentID     string `json:"agent_id"`
	Fingerprint string `json:"fingerprint"`
}

// provisionCroft creates the org's managing-AI croft identity and persists its
// secrets. It generates a fresh per-org root seed, derives the croft's casket
// key from it, registers an org-scoped herald agent (role:croft, owner as the
// responsible human), and writes two secrets: croft-seed-<org> (the root seed,
// from which the broker keyfile is re-derivable) and aspect-keyfile-croft-<org>
// (the herald-rooted bootstrap keyfile the croft pod boots with). The seed is
// never returned or printed.
func provisionCroft(ctx context.Context, c client.Doer, sw SecretWriter, org, ownerID, brokerSeam, namespace string) (croftResult, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return croftResult{}, fmt.Errorf("generate croft seed: %w", err)
	}
	priv, pub, err := casket.DeriveAgentKey(seed, croftSlug)
	if err != nil {
		return croftResult{}, fmt.Errorf("derive croft key: %w", err)
	}
	fp := identity.Fingerprint(pub)

	agent, err := herald.CreateAgent(ctx, c, org, herald.CreateAgentInput{
		DisplayName:      croftSlug,
		ResponsibleHuman: ownerID,
		CasketPubkey:     base64.StdEncoding.EncodeToString(pub),
		Scopes:           []string{roleCroft},
	})
	if err != nil {
		return croftResult{}, fmt.Errorf("register croft agent: %w", err)
	}

	kf := cwkeyfile.Build(priv, agent.ID, brokerSeam, croftSlug, fp)
	kfJSON, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return croftResult{}, fmt.Errorf("marshal croft keyfile: %w", err)
	}

	if err := sw.WriteSecret(ctx, namespace, "croft-seed-"+org, map[string][]byte{"seed": seed}); err != nil {
		return croftResult{}, fmt.Errorf("write croft seed secret: %w", err)
	}
	if err := sw.WriteSecret(ctx, namespace, "aspect-keyfile-croft-"+org, map[string][]byte{"keyfile.json": kfJSON}); err != nil {
		return croftResult{}, fmt.Errorf("write croft keyfile secret: %w", err)
	}
	return croftResult{AgentID: agent.ID, Fingerprint: fp}, nil
}

// roleCroft is the wire token herald expands to the croft scope bundle.
const roleCroft = "role:croft"

// kubectlSecretWriter applies an Opaque Secret via `kubectl apply -f -`, piping
// the manifest over stdin so secret material never lands in argv or a temp file.
// The kubectl invocation is overridable via CW_KUBECTL (e.g. "sudo kubectl").
type kubectlSecretWriter struct{}

func (kubectlSecretWriter) WriteSecret(ctx context.Context, namespace, name string, data map[string][]byte) error {
	enc := make(map[string]string, len(data))
	for k, v := range data {
		enc[k] = base64.StdEncoding.EncodeToString(v)
	}
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"data":       enc,
	})
	if err != nil {
		return fmt.Errorf("marshal secret %s: %w", name, err)
	}
	bin, args := kubectlCmd()
	args = append(args, "apply", "-f", "-")
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = bytes.NewReader(manifest)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply secret %s: %w", name, err)
	}
	return nil
}

// kubectlCmd splits CW_KUBECTL (default "kubectl") into a binary + leading args,
// so "sudo kubectl" works on hosts that need it.
func kubectlCmd() (string, []string) {
	v := os.Getenv("CW_KUBECTL")
	if v == "" {
		return "kubectl", nil
	}
	fields := splitFields(v)
	return fields[0], fields[1:]
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
