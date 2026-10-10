package shared

import "testing"

func TestVersionReportsRevisionAndDirtyBuild(t *testing.T) {
	for revision, want := range map[string]string{"": "0.1.0 (unknown)", "abcdef0123456789012345678901234567890123": "0.1.0 (abcdef012345)", "abcdef0123456789012345678901234567890123-dirty": "0.1.0 (abcdef012345-dirty)"} {
		if got := versionWithRevision(revision); got != want {
			t.Fatalf("got %s wanted %s", got, want)
		}
	}
}
