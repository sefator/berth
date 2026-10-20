package runner

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDrainTokenSent proves the drain reads carry the bearer token an engine
// behind an API key needs: without it Strata's /slots answers 401, which is not
// a fail-open status, so a drain would wait forever.
func TestDrainTokenSent(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		if seen != "Bearer sekret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/slots" {
			_, _ = w.Write([]byte(`[{"id":0,"is_processing":false}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	busy, err := slotsBusy(srv.URL+"/slots", "sekret")
	if err != nil || busy {
		t.Fatalf("slotsBusy with a token: busy=%v err=%v", busy, err)
	}
	if seen != "Bearer sekret" {
		t.Errorf("Authorization sent = %q, want %q", seen, "Bearer sekret")
	}

	// Without the token the boundary is unreadable, and 401 is not fail-open:
	// the caller waits rather than cutting a request off.
	if _, err := slotsBusy(srv.URL+"/slots", ""); err == nil {
		t.Error("slotsBusy without a token: want an error")
	} else if failOpen(err) {
		t.Error("a 401 must not fail open")
	}

	// The option takes the token from the environment variable it names.
	t.Setenv("BERTH_TEST_TOKEN", "sekret")
	if tok := (Options{DrainTokenEnv: "BERTH_TEST_TOKEN"}).drainToken(); tok != "sekret" {
		t.Errorf("drainToken = %q, want sekret", tok)
	}
	if tok := (Options{}).drainToken(); tok != "" {
		t.Errorf("drainToken with no env name = %q, want empty", tok)
	}
}
