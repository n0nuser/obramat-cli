package api_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
)

// TestLiveSearch is the one test that talks to obramat.es. It is off unless
// OBRAMAT_LIVE_TEST=1, and it needs a stored browser session (auth harvest or
// auth login). Everything else in the suite stays offline.
//
//	OBRAMAT_LIVE_TEST=1 go test ./internal/api -run TestLiveSearch -v
func TestLiveSearch(t *testing.T) {
	if os.Getenv("OBRAMAT_LIVE_TEST") != "1" {
		t.Skip("live test: set OBRAMAT_LIVE_TEST=1 to run it against obramat.es")
	}
	s, err := config.LoadSession(config.DefaultPaths())
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if s == nil {
		t.Skip("live test needs a stored session: run `obramat auth harvest` (or `auth login --cookies FILE`) first")
	}

	c := api.New("live-test")
	c.Session = &s.Session
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	l, err := c.List(ctx, api.ListParams{Query: "tablero osb"})
	if err != nil {
		t.Fatalf("live search: %v", err)
	}
	if l.Total == 0 || len(l.Products) == 0 {
		t.Fatalf("live search returned no products (total %d)", l.Total)
	}
	t.Logf("%d results, first %q at %s", l.Total, l.Products[0].Title, l.Products[0].URL)
}
