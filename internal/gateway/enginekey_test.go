package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/T-Crypt/berth/internal/config"
)

// TestEngineKey proves the gateway authenticates to a keyed engine with that
// engine's own key (api_key_env): its readiness probe and model listing carry
// it, and a proxied request has the client's gateway key replaced. Without
// this a Strata served with api_key answers 401 to every probe, so the gateway
// treats it as down and starts a second copy.
func TestEngineKey(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer engine-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()

	t.Setenv("BERTH_TEST_ENGINE_KEY", "engine-key")
	g := &Gateway{
		Key: "gateway-key",
		Engines: []config.Engine{{
			Name: "strata", URL: srv.URL, APIKeyEnv: "BERTH_TEST_ENGINE_KEY",
			Serves: []string{"m1"}, Start: []string{"true"},
		}},
	}

	if tok := g.tokenFor(srv.URL); tok != "engine-key" {
		t.Errorf("tokenFor = %q, want engine-key", tok)
	}
	if tok := g.tokenFor(srv.URL + "/"); tok != "engine-key" {
		t.Errorf("tokenFor with a trailing slash = %q", tok)
	}
	if tok := g.tokenFor("http://127.0.0.1:1"); tok != "" {
		t.Errorf("tokenFor for an unknown engine = %q, want empty", tok)
	}

	if !g.answers(context.Background(), srv.URL) {
		t.Error("answers: a keyed engine is not seen as up")
	}
	if got := g.listed(context.Background(), srv.URL); len(got) != 1 || got[0] != "m1" {
		t.Errorf("listed = %v, want [m1]", got)
	}
	for _, a := range auth {
		if a != "Bearer engine-key" {
			t.Errorf("a probe sent %q, want the engine's key", a)
		}
	}
}
