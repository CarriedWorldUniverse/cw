package kb

import (
	"context"
	"os"
	"testing"
	"time"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
)

// Live tests exercise the REAL transport `cw kb` now uses: in-mesh mTLS straight
// to commonplace's KnowledgeService. Gated on the mesh cert (CW_APP_TLS_*) +
// CW_APP_ORG; skip otherwise so the offline suite stays green.
//
//	CW_APP_TLS_CERT=... CW_APP_TLS_KEY=... CW_APP_TLS_CA=... \
//	CW_APP_ORG=<org> CW_APP_COMMONPLACE_ADDR=commonplace.cwb.svc.cluster.local:8101 \
//	go test ./internal/cli/kb/ -run TestLiveKB -v
//
// Run from an in-mesh seat (e.g. croft) where commonplace is reachable and its
// embed backend (ollama nomic-embed-text) is up. The org/identity must hold
// knowledge:read/write.
func liveOrSkip(t *testing.T) (cwbv1.KnowledgeServiceClient, func(), string) {
	t.Helper()
	if os.Getenv("CW_APP_TLS_CERT") == "" || os.Getenv("CW_APP_ORG") == "" {
		t.Skip("set CW_APP_TLS_CERT/_KEY/_CA + CW_APP_ORG to run live kb tests")
	}
	kc, done, err := knowledgeClient()
	if err != nil {
		t.Fatalf("dial commonplace: %v", err)
	}
	return kc, done, os.Getenv("CW_APP_ORG")
}

// TestLiveKB stores an entry then proves SEMANTIC retrieval: a query worded
// differently from the stored content surfaces it (commonplace embeds via ollama).
func TestLiveKB(t *testing.T) {
	kc, done, org := liveOrSkip(t)
	defer done()
	ctx := context.Background()

	marker := "cwkb-" + time.Now().Format("150405")
	stored, err := kc.Store(mdCtx(ctx, org, "knowledge:write"), &cwbv1.StoreRequest{
		Topic:      marker,
		Content:    "The CWB platform is deployed on a single-node k3s cluster on the dMon machine. " + marker,
		Visibility: "org",
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	id := stored.GetEntry().GetId()
	resp, err := kc.Search(mdCtx(ctx, org, "knowledge:read"), &cwbv1.SearchRequest{Q: "where is the platform hosted", TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range resp.GetHits() {
		if h.GetEntry().GetId() == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("semantic search did not surface stored entry %s (hits=%d)", id, len(resp.GetHits()))
	}
	_, _ = kc.Delete(mdCtx(ctx, org, "knowledge:write"), &cwbv1.DeleteRequest{Id: id})
}

func TestLiveKBCrud(t *testing.T) {
	kc, done, org := liveOrSkip(t)
	defer done()
	ctx := context.Background()
	marker := "cwkbcrud-" + time.Now().Format("150405")

	stored, err := kc.Store(mdCtx(ctx, org, "knowledge:write"), &cwbv1.StoreRequest{Topic: marker, Content: "original " + marker, Visibility: "org"})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	id := stored.GetEntry().GetId()
	newTopic := marker + "-updated"
	if _, err := kc.Update(mdCtx(ctx, org, "knowledge:write"), &cwbv1.UpdateRequest{Id: id, Topic: newTopic}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	resp, err := kc.List(mdCtx(ctx, org, "knowledge:read"), &cwbv1.ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, e := range resp.GetEntries() {
		if e.GetId() == id {
			found = true
			if e.GetTopic() != newTopic {
				t.Fatalf("topic not updated: %q", e.GetTopic())
			}
		}
	}
	if !found {
		t.Fatalf("entry %s missing after update", id)
	}
	if _, err := kc.Delete(mdCtx(ctx, org, "knowledge:write"), &cwbv1.DeleteRequest{Id: id}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	resp, err = kc.List(mdCtx(ctx, org, "knowledge:read"), &cwbv1.ListRequest{})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	for _, e := range resp.GetEntries() {
		if e.GetId() == id {
			t.Fatalf("entry %s still present after delete", id)
		}
	}
}
