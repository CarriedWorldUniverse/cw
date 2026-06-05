package credential

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// fetchFunc retrieves the git credential for a host from the custodian seam.
type fetchFunc func(host string) (username, password string, err error)

// runGitHelper implements git's credential-helper protocol for one op.
// Only "get" produces output (username/password from the seam); "store"
// and "erase" are no-ops — the seam is the source of truth, nothing is
// cached locally. Input is `key=value\n` lines terminated by a blank line.
func runGitHelper(op string, in io.Reader, out io.Writer, fetch fetchFunc) error {
	attrs := map[string]string{}
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			attrs[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if op != "get" {
		return nil // store/erase are no-ops
	}
	host := attrs["host"]
	if host == "" {
		return fmt.Errorf("git-helper: no host attribute on stdin")
	}
	user, pass, err := fetch(host)
	if err != nil {
		return err
	}
	// NEVER log user/pass — write only to the fd git reads.
	fmt.Fprintf(out, "username=%s\npassword=%s\n", user, pass)
	return nil
}

// seamFetchGit calls the broker custodian seam as the worker, returning
// the git username/password without the secret ever touching the
// environment, shell, or repo config. The broker session JWT is supplied
// via the global --token (CW_TOKEN); the seam base URL via CW_SEAM_URL.
// Both are provided by the worker runtime (M2 / NEX-436).
func seamFetchGit(gf *cmdutil.GlobalFlags, host string) (string, string, error) {
	seamURL := strings.TrimRight(os.Getenv("CW_SEAM_URL"), "/")
	if seamURL == "" {
		return "", "", fmt.Errorf("git-helper: CW_SEAM_URL not set")
	}
	if gf.Token == "" {
		return "", "", fmt.Errorf("git-helper: no session token (set --token / CW_TOKEN)")
	}
	body, _ := json.Marshal(map[string]string{"kind": "git", "host": host})
	req, err := http.NewRequest("POST", seamURL+"/api/agent/credential.fetch", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+gf.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("git-helper: seam returned %d", resp.StatusCode)
	}
	var out struct {
		Bundle struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"bundle"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Bundle.Username, out.Bundle.Password, nil
}
