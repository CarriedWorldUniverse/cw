package app

import "testing"

func TestPrecheckDeclaration(t *testing.T) {
	if err := precheck("lynxai", []byte("name: lynxai\nnamespace: nexus\nimage: x")); err != nil {
		t.Fatal(err)
	}
	if err := precheck("lynxai", []byte("name: other\nnamespace: nexus\nimage: x")); err == nil {
		t.Fatal("want name-mismatch error")
	}
	if err := precheck("lynxai", []byte("namespace: nexus")); err == nil {
		t.Fatal("want missing-fields error")
	}
	if err := precheck("lynxai", []byte(":::bad")); err == nil {
		t.Fatal("want parse error")
	}
}
