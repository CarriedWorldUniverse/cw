package cmdutil

import "testing"

func TestResolveRepo(t *testing.T) {
	cases := []struct {
		ref, flagOrg, defOrg string
		wantOrg, wantSlug    string
		wantErr              bool
	}{
		{"widgets", "", "org-1", "org-1", "widgets", false},            // bare slug -> default org
		{"org-2/widgets", "", "org-1", "org-2", "widgets", false},      // explicit org/slug
		{"widgets", "org-3", "org-1", "org-3", "widgets", false},       // --org overrides default
		{"org-2/widgets", "org-3", "org-1", "org-3", "widgets", false}, // --org overrides ref org
		{"widgets", "", "", "", "", true},                              // no org anywhere -> error
		{"", "", "org-1", "", "", true},                                // empty ref -> error
		{"org-2/", "", "org-1", "", "", true},                          // explicit org, empty slug -> error
	}
	for _, c := range cases {
		org, slug, err := ResolveRepo(c.ref, c.flagOrg, c.defOrg)
		if c.wantErr {
			if err == nil {
				t.Fatalf("ResolveRepo(%q,%q,%q): want error", c.ref, c.flagOrg, c.defOrg)
			}
			continue
		}
		if err != nil || org != c.wantOrg || slug != c.wantSlug {
			t.Fatalf("ResolveRepo(%q,%q,%q) = %q,%q,%v; want %q,%q", c.ref, c.flagOrg, c.defOrg, org, slug, err, c.wantOrg, c.wantSlug)
		}
	}
}

func TestCairnGitURL(t *testing.T) {
	want := "http://edge:8080/cairn/org-1/widgets.git"
	// Both trailing-slash and no-trailing-slash edges yield the same URL.
	for _, edge := range []string{"http://edge:8080/", "http://edge:8080"} {
		if got := CairnGitURL(edge, "org-1", "widgets"); got != want {
			t.Fatalf("CairnGitURL(%q) = %q, want %q", edge, got, want)
		}
	}
}
