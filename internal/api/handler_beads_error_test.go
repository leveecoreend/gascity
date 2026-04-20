package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

type failingGetStore struct {
	beads.Store
	failID  string
	failErr error
}

func (s *failingGetStore) Get(id string) (beads.Bead, error) {
	if id == s.failID {
		return beads.Bead{}, s.failErr
	}
	return s.Store.Get(id)
}

func TestBeadDepsReturnsErrorWhenAttachedLookupFails(t *testing.T) {
	state, _, betaStore := configureBeadRouteState(t)
	parent, err := betaStore.Create(beads.Bead{Title: "Parent"})
	if err != nil {
		t.Fatalf("Create(parent): %v", err)
	}
	attached, err := betaStore.Create(beads.Bead{Title: "Attached", Type: "molecule"})
	if err != nil {
		t.Fatalf("Create(attached): %v", err)
	}
	if err := betaStore.SetMetadata(parent.ID, "molecule_id", attached.ID); err != nil {
		t.Fatalf("SetMetadata(molecule_id): %v", err)
	}
	state.stores["beta"] = &failingGetStore{
		Store:   betaStore,
		failID:  attached.ID,
		failErr: errors.New("attached lookup failed"),
	}

	h := newTestCityHandler(t, state)
	req := httptest.NewRequest("GET", cityURL(state, "/bead/")+parent.ID+"/deps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("attached lookup failed")) {
		t.Fatalf("body = %s, want attached lookup failure", rec.Body.String())
	}
}
