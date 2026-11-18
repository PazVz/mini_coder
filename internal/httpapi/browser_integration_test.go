//go:build integration && browser

package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/httpapi"
	"github.com/PazVz/mini_coder/internal/testutil"
)

type browserMailbox struct {
	mu    sync.Mutex
	links map[string]string
}

func (m *browserMailbox) Send(_ context.Context, to, _, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "http://") {
			m.links[to] = line
		}
	}
	return nil
}

func TestBrowserAuthFlow(t *testing.T) {
	pool, migrations := testutil.NewDatabase(t)
	if _, err := migrations.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	const setupToken = "browser-test-setup-token-with-32-bytes"
	accounts, err := auth.NewService(pool, setupToken)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	baseURL := "http://" + server.Listener.Addr().String()
	mailbox := &browserMailbox{links: make(map[string]string)}
	emails, err := auth.NewEmailService(accounts, auth.EmailConfig{
		Sender:              mailbox,
		PublicURL:           baseURL,
		RegistrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	router := http.NewServeMux()
	// This mailbox exists only in the isolated browser test process.
	router.HandleFunc("GET /__test/mail", func(w http.ResponseWriter, r *http.Request) {
		mailbox.mu.Lock()
		defer mailbox.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"link": mailbox.links[r.URL.Query().Get("email")]})
	})
	router.Handle("/", httpapi.NewHandler(httpapi.Config{
		Auth:   accounts,
		Users:  accounts,
		Emails: emails,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	server.Config.Handler = router
	server.Start()
	webDir, err := filepath.Abs("../../web")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "npm", "run", "test:e2e")
	command.Dir = webDir
	command.Env = append(os.Environ(), "MINICODER_BROWSER_URL="+baseURL, "MINICODER_BROWSER_SETUP_TOKEN="+setupToken)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("browser test: %v\n%s", err, output)
	}
	t.Log(string(output))
}
