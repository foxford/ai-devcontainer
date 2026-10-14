package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckUpdatesReportsOnlyVersionedDocumentChanges(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"platform-rules", "instructions/base.md"},
		{"usage-guide", "documents/guide.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, upstream := updateFixture(t)
			if tc.name == "usage-guide" {
				data, err := os.ReadFile(filepath.Join(upstream, "delivery", "catalog.json"))
				if err != nil {
					t.Fatal(err)
				}
				var catalog DeliveryCatalog
				if err := json.Unmarshal(data, &catalog); err != nil {
					t.Fatal(err)
				}
				catalog.Base.Documents = []DeliveryDocument{{Source: tc.source, Target: "GUIDE.md", Mode: "copy"}}
				data, err = json.Marshal(catalog)
				if err != nil {
					t.Fatal(err)
				}
				updatesWrite(t, upstream, "delivery/catalog.json", string(data))
			}
			metadata := DeliveryUnit{Kind: "document", Name: tc.name, Version: "1.0.0"}
			writeMetadata := func() {
				t.Helper()
				data, err := json.Marshal(metadata)
				if err != nil {
					t.Fatal(err)
				}
				updatesWrite(t, upstream, "delivery/"+tc.source+".meta.json", string(data))
			}
			updatesWrite(t, upstream, "delivery/"+tc.source, "Original document\n")
			writeMetadata()
			updatesGit(t, upstream, "add", ".")
			updatesGit(t, upstream, "commit", "--quiet", "-m", "version published document")
			updatesGit(t, a.Platform, "pull", "--quiet", "--ff-only")
			if _, err := a.DeliveryCatalog(); err != nil {
				t.Fatal("document fixture does not belong to a valid delivery:", err)
			}
			updatesWrite(t, upstream, "delivery/"+tc.source, "Changed document with the same version\n")
			updatesGit(t, upstream, "add", ".")
			updatesGit(t, upstream, "commit", "--quiet", "-m", "document content changed")
			status, err := a.CheckUpdates(context.Background(), true)
			if err != nil || status.Available || len(status.Changes) != 0 {
				t.Fatalf("same-version document caused an update notice: %+v, %v", status, err)
			}
			metadata.Version = "1.0.1"
			writeMetadata()
			updatesGit(t, upstream, "add", ".")
			updatesGit(t, upstream, "commit", "--quiet", "-m", "publish document patch")
			status, err = a.CheckUpdates(context.Background(), true)
			want := []DeliveryChange{{Kind: "document", Name: tc.name, Action: "updated", BeforeVersion: "1.0.0", AfterVersion: "1.0.1"}}
			if err != nil || !status.Available || !reflect.DeepEqual(status.Changes, want) {
				t.Fatalf("document-only update was not reported: %+v, %v", status, err)
			}
			var output bytes.Buffer
			if err := WriteUpdateStatus(&output, status, false); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "документ "+tc.name+" · 1.0.0 → 1.0.1") {
				t.Fatal("document update has no readable label:", output.String())
			}
		})
	}
}
