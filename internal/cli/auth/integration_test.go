package auth

import (
	"context"
	"os"
	"testing"
)

// TestLiveLogin exercises the full login→whoami→logout loop against a real
// deployment. Gated: set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD to run.
//
//	CW_IT_EDGE=http://dmonextreme.tail41686e.ts.net:8080 \
//	CW_IT_USER=cwadmin@carriedworld.com CW_IT_PASSWORD=... go test ./internal/cli/auth/ -run TestLiveLogin -v
func TestLiveLogin(t *testing.T) {
	edge := os.Getenv("CW_IT_EDGE")
	if edge == "" {
		t.Skip("set CW_IT_EDGE + CW_IT_USER + CW_IT_PASSWORD to run the live login test")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	if err := runLogin(context.Background(), loginOpts{
		edge: edge, contextName: "it", username: os.Getenv("CW_IT_USER"), password: os.Getenv("CW_IT_PASSWORD"),
	}); err != nil {
		t.Fatalf("live login: %v", err)
	}
	info, err := whoamiInfo(&GlobalFlags{Context: "it"})
	if err != nil || info.Subject == "" {
		t.Fatalf("whoami: %v %+v", err, info)
	}
	if err := runLogout(&GlobalFlags{Context: "it"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
}
