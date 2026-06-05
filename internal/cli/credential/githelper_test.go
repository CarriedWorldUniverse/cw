package credential

import (
	"bytes"
	"strings"
	"testing"
)

func TestGitHelper_Get(t *testing.T) {
	fetch := func(host string) (string, string, error) {
		if host != "github.com" {
			t.Fatalf("host=%q", host)
		}
		return "nexus-cw", "ghp_x", nil
	}
	in := strings.NewReader("protocol=https\nhost=github.com\n\n")
	var out bytes.Buffer
	if err := runGitHelper("get", in, &out, fetch); err != nil {
		t.Fatalf("runGitHelper: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "username=nexus-cw") || !strings.Contains(got, "password=ghp_x") {
		t.Fatalf("out=%q", got)
	}
}

func TestGitHelper_StoreEraseAreNoOps(t *testing.T) {
	for _, op := range []string{"store", "erase"} {
		var out bytes.Buffer
		fetch := func(string) (string, string, error) {
			t.Fatalf("%s must not call fetch", op)
			return "", "", nil
		}
		if err := runGitHelper(op, strings.NewReader("host=github.com\n\n"), &out, fetch); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s wrote output: %q", op, out.String())
		}
	}
}

func TestGitHelper_GetRequiresHost(t *testing.T) {
	var out bytes.Buffer
	err := runGitHelper("get", strings.NewReader("protocol=https\n\n"), &out, func(string) (string, string, error) {
		return "u", "p", nil
	})
	if err == nil {
		t.Fatal("want error when host attribute missing")
	}
}
