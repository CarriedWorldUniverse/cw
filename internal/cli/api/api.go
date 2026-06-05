package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CarriedWorldUniverse/cw/internal/cli/credential"
	"github.com/CarriedWorldUniverse/cw/internal/cli/setupgit"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
)

var (
	githubAPIBase = "https://api.github.com"
	httpClient    = &http.Client{Timeout: 30 * time.Second}
)

type options struct {
	method    string
	host      string
	fields    []field
	rawFields []field
	headers   []string
}

type field struct {
	key   string
	value string
}

func NewCmd(gf *cmdutil.GlobalFlags) *cobra.Command {
	opts := &options{method: http.MethodGet}
	cmd := &cobra.Command{
		Use:   "api <endpoint>",
		Short: "Make an authenticated GitHub API request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, gf, args[0], opts)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&opts.method, "method", "X", http.MethodGet, "HTTP method")
	f.VarP((*fieldValues)(&opts.fields), "field", "f", "typed JSON body field key=value")
	f.VarP((*fieldValues)(&opts.rawFields), "raw-field", "F", "string JSON body field key=value")
	f.StringArrayVarP(&opts.headers, "header", "H", nil, "request header key:value")
	f.StringVar(&opts.host, "host", "", "primary API host (github or cairn)")
	return cmd
}

func run(cmd *cobra.Command, gf *cmdutil.GlobalFlags, endpoint string, opts *options) error {
	spec, err := setupgit.ResolveHost(opts.host)
	if err != nil {
		return err
	}
	if spec.Name == "cairn" {
		return fmt.Errorf("api: cairn api not yet supported")
	}
	base, err := apiBase(spec)
	if err != nil {
		return err
	}
	body, err := requestBody(opts.fields, opts.rawFields)
	if err != nil {
		return err
	}
	token, err := gitToken(gf, spec.CredentialID)
	if err != nil {
		return err
	}
	req, err := newRequest(strings.ToUpper(opts.method), base, endpoint, body)
	if err != nil {
		return err
	}
	for _, h := range opts.headers {
		key, val, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return fmt.Errorf("api: invalid header %q (want key:value)", h)
		}
		req.Header.Set(strings.TrimSpace(key), strings.TrimSpace(val))
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "token "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("api: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if _, err := cmd.OutOrStdout().Write(respBody); err != nil {
		return err
	}
	return nil
}

func apiBase(spec setupgit.HostSpec) (string, error) {
	switch spec.Name {
	case "github":
		return githubAPIBase, nil
	default:
		return "", fmt.Errorf("api: host %q not supported", spec.Name)
	}
}

func gitToken(gf *cmdutil.GlobalFlags, host string) (string, error) {
	_, pass, err := credential.SeamFetchGit(gf, host)
	if err != nil {
		return "", err
	}
	if pass == "" {
		return "", fmt.Errorf("api: seam returned empty credential password")
	}
	return pass, nil
}

func newRequest(method, base, endpoint string, body []byte) (*http.Request, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/" + strings.TrimLeft(endpoint, "/"))
	if err != nil {
		return nil, err
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	return http.NewRequest(method, u.String(), r)
}

func requestBody(fields, rawFields []field) ([]byte, error) {
	if len(fields) == 0 && len(rawFields) == 0 {
		return nil, nil
	}
	body := map[string]any{}
	for _, f := range fields {
		body[f.key] = typedValue(f.value)
	}
	for _, f := range rawFields {
		body[f.key] = f.value
	}
	return json.Marshal(body)
}

func typedValue(s string) any {
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

type fieldValues []field

func (v *fieldValues) String() string { return fmt.Sprint([]field(*v)) }
func (v *fieldValues) Type() string   { return "key=value" }

func (v *fieldValues) Set(s string) error {
	key, val, ok := strings.Cut(s, "=")
	if !ok || key == "" {
		return fmt.Errorf("want key=value")
	}
	*v = append(*v, field{key: key, value: val})
	return nil
}
