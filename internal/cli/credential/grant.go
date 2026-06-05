package credential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

// postGitGrant grants a worker identity scoped access to a git credential
// by calling the broker admin grant endpoint. This is an operator/admin
// action — gf.Token must be an admin operator token (not a worker session
// JWT); CW_SEAM_URL is the broker base URL.
func postGitGrant(gf *cmdutil.GlobalFlags, name, aspect string) error {
	base := strings.TrimRight(os.Getenv("CW_SEAM_URL"), "/")
	if base == "" {
		return fmt.Errorf("issue-git-permission: CW_SEAM_URL not set")
	}
	if gf.Token == "" {
		return fmt.Errorf("issue-git-permission: no admin token (set --token / CW_TOKEN)")
	}
	body, _ := json.Marshal(map[string]string{"aspect": aspect})
	endpoint := base + "/api/admin/credentials/" + url.PathEscape(name) + "/grant"
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+gf.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("issue-git-permission: grant returned %d", resp.StatusCode)
	}
	fmt.Fprintf(os.Stderr, "granted %q scoped access to git credential %q\n", aspect, name)
	return nil
}
