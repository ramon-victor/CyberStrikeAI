package c2

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// 集成验证：路由、鉴权伪装 404、明文 check-in JSON 回包。
func TestHTTPBeaconListener_CheckInMatrix(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "c2.sqlite")
	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	lnPick, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lnPick.Addr().(*net.TCPAddr).Port
	_ = lnPick.Close()

	keyB64, err := GenerateAESKey()
	if err != nil {
		t.Fatal(err)
	}
	token := "test-implant-token-fixed"

	lid := "l_testhttpbeacon01"
	rec := &database.C2Listener{
		ID:            lid,
		Name:          "t",
		Type:          string(ListenerTypeHTTPBeacon),
		BindHost:      "127.0.0.1",
		BindPort:      port,
		EncryptionKey: keyB64,
		ImplantToken:  token,
		Status:        "stopped",
		ConfigJSON:    `{"beacon_check_in_path":"/check_in"}`,
		CreatedAt:     time.Now(),
	}
	if err := db.CreateC2Listener(rec); err != nil {
		t.Fatal(err)
	}

	m := NewManager(db, zap.NewNop(), filepath.Join(tmp, "c2store"))
	m.Registry().Register(string(ListenerTypeHTTPBeacon), NewHTTPBeaconListener)
	if _, err := m.StartListener(lid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.StopListener(lid) })

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("wrong_path_go_default_404", func(t *testing.T) {
		resp, err := client.Post(base+"/nope", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d body=%q", resp.StatusCode, b)
		}
		if !strings.Contains(string(b), "404") || !strings.Contains(strings.ToLower(string(b)), "not found") {
			t.Fatalf("unexpected body: %q", b)
		}
	})

	t.Run("check_in_wrong_token_disguised_html_404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, base+"/check_in", bytes.NewBufferString(`{"hostname":"h"}`))
		req.Header.Set("X-Implant-Token", "wrong-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("content-type=%q body=%q", ct, b)
		}
		if !strings.Contains(string(b), "404 Not Found") {
			t.Fatalf("expected disguised HTML, got: %q", b)
		}
	})

	t.Run("check_in_ok_plaintext_json", func(t *testing.T) {
		body := `{"hostname":"n","username":"u","os":"Linux","arch":"amd64","internal_ip":"10.0.0.1","pid":42}`
		req, _ := http.NewRequest(http.MethodPost, base+"/check_in", strings.NewReader(body))
		req.Header.Set("X-Implant-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, b)
		}
		var out ImplantCheckInResponse
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("json: %v body=%s", err, b)
		}
		if out.SessionID == "" || out.NextSleep <= 0 {
			t.Fatalf("bad response: %+v", out)
		}
	})
}
