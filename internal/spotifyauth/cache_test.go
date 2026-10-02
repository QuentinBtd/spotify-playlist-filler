package spotifyauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

const testScopes = "playlist-modify-private playlist-modify-public playlist-read-collaborative playlist-read-private user-read-private"

func cacheFixture(t *testing.T, token *oauth2.Token) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tokens")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPF_TOKEN_CACHE", dir)
	sum := sha256.Sum256([]byte("test-id\n" + testScopes))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".spf-token.json")
	data, err := json.Marshal(map[string]any{"version": 1, "client_id": "test-id", "scopes": testScopes, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}
func validToken() *oauth2.Token {
	return &oauth2.Token{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func oauthFixtureContext(t *testing.T, handler http.HandlerFunc) context.Context {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "accounts.spotify.com" {
			if r.URL.Hostname() != "127.0.0.1" {
				return nil, fmt.Errorf("unexpected fixture destination")
			}
			return http.DefaultTransport.RoundTrip(r)
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = base.Scheme
		u.Host = base.Host
		clone.URL = &u
		return http.DefaultTransport.RoundTrip(clone)
	}), Timeout: time.Second}
	return context.WithValue(context.Background(), oauth2.HTTPClient, client)
}
func TestLoginRefreshesExpiredTokenAndPersistsRotation(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	calls := 0
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "synthetic-refresh" {
			t.Error("wrong refresh request")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"rotated-access","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":3600}`)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Skip("port occupied")
	}
	defer listener.Close()
	var output bytes.Buffer
	_, err = Login(ctx, "test-id", "synthetic-secret", &output)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(string(data), "rotated-refresh") || !strings.Contains(string(data), "rotated-access") || strings.Contains(string(data), "synthetic-secret") || output.Len() != 0 {
		t.Fatal("refresh not privately persisted")
	}
}
func TestRefreshFailureDoesNotExposeCredentialsOrRelogin(t *testing.T) {
	for _, status := range []int{400, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			token := validToken()
			token.Expiry = time.Now().Add(-time.Hour)
			_, path := cacheFixture(t, token)
			before, _ := os.ReadFile(path)
			ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"temporarily_unavailable","error_description":"synthetic-secret synthetic-refresh"}`)
			})
			var output bytes.Buffer
			_, err := Login(ctx, "test-id", "synthetic-secret", &output)
			after, _ := os.ReadFile(path)
			if err == nil || strings.Contains(err.Error(), "synthetic-") || output.Len() != 0 || !bytes.Equal(before, after) {
				t.Fatalf("unsafe refresh error: %v", err)
			}
		})
	}
}
func TestInvalidGrantDiscardsCacheBeforeInteractiveLogin(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	calls := 0
	base := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":"invalid_grant","error_description":"synthetic-secret"}`)
	})
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	printed := false
	_, err := Login(ctx, "test-id", "synthetic-secret", outputWriter(func(data []byte) (int, error) {
		printed = true
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("cache not removed before relogin")
		}
		cancel()
		return len(data), nil
	}))
	if !printed || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("invalid_grant did not relogin once: printed=%v calls=%d err=%v", printed, calls, err)
	}
}
func TestInteractiveLoginPersistsToken(t *testing.T) {
	_, path := cacheFixture(t, validToken())
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Error("not code exchange")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"interactive-access","refresh_token":"interactive-refresh","token_type":"Bearer","expires_in":3600}`)
	})
	output := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := Login(ctx, "test-id", "synthetic-secret", outputWriter(func(b []byte) (int, error) { output <- string(b); return len(b), nil }))
		done <- err
	}()
	var line string
	select {
	case line = <-output:
	case err := <-done:
		t.Fatalf("login failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("no login URL")
	}
	fields := strings.Fields(line)
	u, err := url.Parse(fields[len(fields)-1])
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(RedirectURI + "?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=synthetic-code")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("login blocked")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("interactive token not persisted: %v", err)
	}
	if !strings.Contains(string(data), "interactive-refresh") || strings.Contains(string(data), "synthetic-secret") {
		t.Fatal("unsafe interactive cache")
	}
}
func TestInteractiveWriteErrorReachesLogin(t *testing.T) {
	dir, path := cacheFixture(t, validToken())
	os.Remove(path)
	base := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","token_type":"Bearer","expires_in":3600}`)
	})
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	output := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := Login(ctx, "test-id", "synthetic-secret", outputWriter(func(b []byte) (int, error) { output <- string(b); return len(b), nil }))
		done <- err
	}()
	var line string
	select {
	case line = <-output:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("no login URL")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(line)
	u, _ := url.Parse(fields[len(fields)-1])
	response, err := http.Get(RedirectURI + "?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=synthetic-code")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case err := <-done:
		if err == nil || strings.Contains(err.Error(), "synthetic-") {
			t.Fatalf("unsafe error %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("write error silently swallowed by callback")
	}
}
func TestAPIRevocationInvalidatesBeforeSubsequentMutation(t *testing.T) {
	_, path := cacheFixture(t, validToken())
	client, err := Login(context.Background(), "test-id", "synthetic-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(401)
		io.WriteString(w, "synthetic-refresh")
	}))
	defer server.Close()
	response, err := client.Get(server.URL)
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Error("revoked API token not surfaced")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("revoked cache still exists")
	}
	response, err = client.Post(server.URL, "application/json", strings.NewReader(`{}`))
	if response != nil {
		response.Body.Close()
	}
	if err == nil || requests != 1 {
		t.Fatalf("revoked authorization allowed mutation/retry: requests=%d err=%v", requests, err)
	}
}
func TestInvalidTokenFieldsFailClosed(t *testing.T) {
	for _, kind := range []string{"missing-expiry", "missing-refresh", "missing-access", "wrong-type"} {
		t.Run(kind, func(t *testing.T) {
			token := validToken()
			switch kind {
			case "missing-expiry":
				token.Expiry = time.Time{}
			case "missing-refresh":
				token.RefreshToken = ""
			case "missing-access":
				token.AccessToken = ""
			case "wrong-type":
				token.TokenType = "MAC"
			}
			cacheFixture(t, token)
			ctx, cancel := context.WithCancel(oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("invalid cache contacted token endpoint")
				w.WriteHeader(500)
			}))
			defer cancel()
			printed := false
			client, err := Login(ctx, "test-id", "synthetic-secret", outputWriter(func(b []byte) (int, error) { printed = true; cancel(); return len(b), nil }))
			if client != nil || err == nil || printed {
				t.Fatalf("invalid cache not rejected before login: %v", err)
			}
		})
	}
}
func TestRefreshWriteFailureStopsSubsequentRequests(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	dir, path := cacheFixture(t, token)
	before, _ := os.ReadFile(path)
	calls := 0
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := os.Chmod(dir, 0755); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"rotated-access","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":3600}`)
	})
	cache, err := newTokenCache("test-id")
	if err != nil {
		t.Fatal(err)
	}
	source := newSource(ctx, "test-id", "synthetic-secret", cache, token)
	for i := 0; i < 2; i++ {
		if _, err := source.Token(); err == nil {
			t.Error("write failure ignored")
		}
	}
	after, _ := os.ReadFile(path)
	if calls != 1 || !bytes.Equal(before, after) {
		t.Fatalf("failed persistence retried refresh or destroyed cache: %d", calls)
	}
}
func TestConcurrentRefreshPreservesOmittedRefreshToken(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	var calls atomic.Int32
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"rotated-access","token_type":"Bearer","expires_in":3600}`)
	})
	cache, err := newTokenCache("test-id")
	if err != nil {
		t.Fatal(err)
	}
	source := newSource(ctx, "test-id", "synthetic-secret", cache, token)
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			token, err := source.Token()
			if err != nil || token.RefreshToken != "synthetic-refresh" {
				t.Errorf("refresh lost previous token: %v", err)
			}
		}()
	}
	group.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !strings.Contains(string(data), "synthetic-refresh") {
		t.Fatal("concurrent refresh not persisted once")
	}
}
func TestCacheMetadataAndDefaultPath(t *testing.T) {
	dir, path := cacheFixture(t, validToken())
	for _, field := range []string{"version", "client_id", "scopes"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record map[string]any
		if err = json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		saved := record[field]
		delete(record, field)
		bad, _ := json.Marshal(record)
		os.WriteFile(path, bad, 0600)
		if _, err := Login(context.Background(), "test-id", "synthetic-secret", nil); err == nil {
			t.Fatalf("missing metadata %s accepted", field)
		}
		record[field] = saved
		good, _ := json.Marshal(record)
		os.WriteFile(path, good, 0600)
	}
	a, _ := newTokenCache("test-id")
	b, _ := newTokenCache("other-id")
	if a.name == b.name || a.dir != dir {
		t.Fatal("client separation/override failed")
	}
	t.Setenv("SPF_TOKEN_CACHE", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(dir))
	c, err := newTokenCache("test-id")
	base, baseErr := os.UserCacheDir()
	if err != nil || baseErr != nil || c.dir != filepath.Join(base, "spotify-playlist-filler") {
		t.Fatal("OS native default missing")
	}
}
func TestRefreshNeverFollowsRedirects(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	before, _ := os.ReadFile(path)
	var leaked atomic.Int32
	hostile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"redirect-access","refresh_token":"redirect-refresh","token_type":"Bearer","expires_in":3600}`)
	}))
	defer hostile.Close()
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", hostile.URL)
		w.WriteHeader(307)
	})
	_, err := Login(ctx, "test-id", "synthetic-secret", nil)
	after, _ := os.ReadFile(path)
	if err == nil || leaked.Load() != 0 || !bytes.Equal(before, after) {
		t.Fatalf("redirect leaked refresh credentials or changed cache: leaked=%d err=%v", leaked.Load(), err)
	}
}
func TestCacheNamesPermitRepositoryAndDockerExclusion(t *testing.T) {
	cache, err := newTokenCache("test-id")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cache.name, ".spf-token.json") {
		t.Fatal("token filename cannot be safely excluded without ignoring unrelated JSON")
	}
}
func TestCacheAcceptsPrivateDirectoryBelowStickyAncestor(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(parent, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPF_TOKEN_CACHE", filepath.Join(private, "tokens"))
	cache, err := newTokenCache("test-id")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.save(validToken()); err != nil {
		t.Fatalf("private cache under sticky ancestor rejected: %v", err)
	}
	info, err := os.Stat(cache.dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("cache directory not private")
	}
	info, err = os.Stat(filepath.Join(cache.dir, cache.name))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache file not private")
	}
}
func TestInteractiveRefreshUsesApplicationLifetime(t *testing.T) {
	dir, path := cacheFixture(t, validToken())
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	base := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshes.Add(1)
			io.WriteString(w, `{"access_token":"lifetime-access","token_type":"Bearer","expires_in":3600}`)
		} else {
			io.WriteString(w, `{"access_token":"interactive-access","refresh_token":"interactive-refresh","token_type":"Bearer","expires_in":1}`)
		}
	})
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	output := make(chan string, 1)
	clients := make(chan *http.Client, 1)
	failures := make(chan error, 1)
	go func() {
		client, err := Login(ctx, "test-id", "synthetic-secret", outputWriter(func(b []byte) (int, error) { output <- string(b); return len(b), nil }))
		if err != nil {
			failures <- err
		} else {
			clients <- client
		}
	}()
	var line string
	select {
	case line = <-output:
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("no login URL")
	}
	fields := strings.Fields(line)
	u, _ := url.Parse(fields[len(fields)-1])
	response, err := http.Get(RedirectURI + "?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=synthetic-code")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	var client *http.Client
	select {
	case client = <-clients:
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("login blocked")
	}
	source := client.Transport.(authorizationTransport).source
	token, err := source.Token()
	if err != nil || token.AccessToken != "lifetime-access" || refreshes.Load() != 1 {
		t.Fatalf("callback cancellation killed refresh: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "interactive-refresh") {
		t.Fatal("runtime refresh not persisted")
	}
	source.mu.Lock()
	source.token.Expiry = time.Now().Add(-time.Hour)
	source.mu.Unlock()
	cancel()
	if _, err := source.Token(); !errors.Is(err, context.Canceled) {
		t.Fatalf("main cancellation ignored: %v", err)
	}
	if refreshes.Load() != 1 {
		t.Fatal("refresh occurred after main cancellation")
	}
}
func TestMalformedRefreshResponseDoesNotReplaceCache(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	before, _ := os.ReadFile(path)
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"synthetic-replacement","token_type":"Bearer"}`)
	})
	client, err := Login(ctx, "test-id", "synthetic-secret", nil)
	after, _ := os.ReadFile(path)
	if client != nil || err == nil || !bytes.Equal(before, after) {
		t.Fatal("malformed refresh response replaced durable authorization")
	}
}
func TestRuntimeInvalidGrantStopsBeforeMutation(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	ctx := oauthFixtureContext(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":"invalid_grant"}`)
	})
	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mutations.Add(1); w.WriteHeader(201) }))
	defer server.Close()
	cache, err := newTokenCache("test-id")
	if err != nil {
		t.Fatal(err)
	}
	client := authorizationClient(ctx, newSource(ctx, "test-id", "synthetic-secret", cache, token))
	response, err := client.Post(server.URL, "application/json", strings.NewReader(`{}`))
	if response != nil {
		response.Body.Close()
	}
	if err == nil || mutations.Load() != 0 {
		t.Fatal("invalid_grant allowed playlist mutation")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid_grant retained cache")
	}
}
func TestRefreshTransportFailureDoesNotRelogin(t *testing.T) {
	token := validToken()
	token.Expiry = time.Now().Add(-time.Hour)
	_, path := cacheFixture(t, token)
	before, _ := os.ReadFile(path)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport detail synthetic-secret synthetic-refresh")
	})})
	var output bytes.Buffer
	_, err := Login(ctx, "test-id", "synthetic-secret", &output)
	after, _ := os.ReadFile(path)
	if err == nil || strings.Contains(err.Error(), "synthetic-") || output.Len() != 0 || !bytes.Equal(before, after) {
		t.Fatal("transport failure leaked credentials or reset login")
	}
}
func TestCacheRejectsUnsafeFilesystem(t *testing.T) {
	for _, kind := range []string{"directory-permissions", "file-permissions", "file-symlink", "directory-symlink", "ancestor-symlink", "writable-ancestor"} {
		t.Run(kind, func(t *testing.T) {
			dir, path := cacheFixture(t, validToken())
			switch kind {
			case "directory-permissions":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			case "file-permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(filepath.Dir(dir), "target")
				if err = os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory-symlink":
				link := filepath.Join(filepath.Dir(dir), "link")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				t.Setenv("SPF_TOKEN_CACHE", link)
			case "ancestor-symlink":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(filepath.Dir(dir), link); err != nil {
					t.Fatal(err)
				}
				t.Setenv("SPF_TOKEN_CACHE", filepath.Join(link, "tokens"))
			case "writable-ancestor":
				if err := os.Chmod(filepath.Dir(dir), 0777); err != nil {
					t.Fatal(err)
				}
			}
			client, err := Login(context.Background(), "test-id", "synthetic-secret", nil)
			if err == nil || client != nil {
				t.Fatal("unsafe cache accepted")
			}
		})
	}
}
func TestLoginUsesCachedTokenWithoutCallback(t *testing.T) {
	cacheFixture(t, validToken())
	listener, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Skip("port occupied")
	}
	defer listener.Close()
	var output bytes.Buffer
	client, err := Login(context.Background(), "test-id", "synthetic-secret", &output)
	if err != nil {
		t.Fatalf("cached login must not listen: %v", err)
	}
	if client == nil || output.Len() != 0 {
		t.Fatal("cached login opened interactive flow")
	}
}
