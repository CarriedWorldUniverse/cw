package tokenstore

import (
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestRefreshAndAccessRoundTrip(t *testing.T) {
	keyring.MockInit() // in-memory keychain
	dir := t.TempDir()
	t.Setenv("CW_CONFIG_DIR", dir)
	s := New("http://edge:8080", "ctx", "u1")

	if err := s.SaveRefresh("rtok-123"); err != nil {
		t.Fatalf("SaveRefresh: %v", err)
	}
	got, err := s.Refresh()
	if err != nil || got != "rtok-123" {
		t.Fatalf("Refresh = %q,%v", got, err)
	}

	exp := time.Now().Add(10 * time.Minute)
	if err := s.SaveAccess("atok-abc", exp); err != nil {
		t.Fatalf("SaveAccess: %v", err)
	}
	tok, gotExp, err := s.Access()
	if err != nil || tok != "atok-abc" {
		t.Fatalf("Access = %q,%v", tok, err)
	}
	if gotExp.Unix() != exp.Unix() {
		t.Fatalf("exp = %v want %v", gotExp, exp)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := s.Refresh(); err == nil {
		t.Fatal("Refresh after Clear should error")
	}
	if _, _, err := s.Access(); err == nil {
		t.Fatal("Access after Clear should error")
	}
}
