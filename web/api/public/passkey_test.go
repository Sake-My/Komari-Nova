package public

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sake-My/Komari-Nova/database/accounts"
	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/internal/config"
	passkeycore "github.com/Sake-My/Komari-Nova/internal/passkey"
	"github.com/Sake-My/Komari-Nova/web/api"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/pquerna/otp/totp"
)

const testPasskeyOrigin = "https://panel.example"

type passkeyFixture struct {
	t       *testing.T
	router  *gin.Engine
	user    models.User
	cookies map[string]*http.Cookie
	key     *ecdsa.PrivateKey
	rawID   []byte
}

func newPasskeyFixture(t *testing.T) *passkeyFixture {
	t.Helper()
	old, err := config.GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetMany(map[string]any{config.PasskeyEnabledKey: true, config.PasskeyOriginKey: testPasskeyOrigin, config.LoginNotificationKey: false, config.PrivateSiteKey: true, config.DisablePasswordLoginKey: false}); err != nil {
		t.Fatal(err)
	}
	passkeyChallenges = passkeycore.NewStore()
	name, _ := passkeycore.RandomToken()
	user, err := accounts.CreateAccount(name, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	token, err := accounts.CreateSession(user.UUID, 3600, "test", "127.0.0.1", "password")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &passkeyFixture{t: t, user: user, key: key, rawID: []byte("credential-" + user.UUID), cookies: map[string]*http.Cookie{"session_token": {Name: "session_token", Value: token}}}
	gin.SetMode(gin.TestMode)
	f.router = gin.New()
	f.router.Use(api.IdentityMiddleware(), api.PrivateSiteMiddleware())
	f.router.POST("/api/login/passkey/begin", PasskeyLoginBegin)
	f.router.POST("/api/login/passkey/finish", PasskeyLoginFinish)
	f.router.POST("/api/admin/passkey/register/begin", PasskeyRegisterBegin)
	f.router.POST("/api/admin/passkey/register/finish", PasskeyRegisterFinish)
	f.router.GET("/api/admin/passkey", PasskeyList)
	f.router.PATCH("/api/admin/passkey/:id", PasskeyRename)
	f.router.DELETE("/api/admin/passkey/:id", PasskeyDelete)
	t.Cleanup(func() {
		db := dbcore.GetDBInstance()
		db.Where("user_uuid = ?", user.UUID).Delete(&models.PasskeyCredential{})
		db.Where("uuid = ?", user.UUID).Delete(&models.Session{})
		accounts.DeleteAccountByUsername(name)
		config.SetMany(old)
		passkeyChallenges = passkeycore.NewStore()
	})
	return f
}

func (f *passkeyFixture) request(method, path string, body any, origin string) *httptest.ResponseRecorder {
	f.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	r := httptest.NewRequest(method, testPasskeyOrigin+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", origin)
	for _, c := range f.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		f.cookies[c.Name] = c
	}
	return w
}

func (f *passkeyFixture) begin(path string, body any) (string, string) {
	f.t.Helper()
	w := f.request("POST", path, body, testPasskeyOrigin)
	if w.Code != 200 {
		f.t.Fatalf("begin %s: %d %s", path, w.Code, w.Body)
	}
	var response struct {
		Data struct {
			ChallengeID string `json:"challenge_id"`
			Options     struct {
				Challenge string `json:"challenge"`
			}
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		f.t.Fatal(err)
	}
	return response.Data.ChallengeID, response.Data.Options.Challenge
}

func b64(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
func (f *passkeyFixture) clientData(kind, challenge, origin string) []byte {
	f.t.Helper()
	data, err := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}
func authData(rp string, flags byte, count uint32) []byte {
	hash := sha256.Sum256([]byte(rp))
	data := append([]byte{}, hash[:]...)
	data = append(data, flags)
	return binary.BigEndian.AppendUint32(data, count)
}

func (f *passkeyFixture) registration(challenge string) any {
	f.t.Helper()
	x, y := f.key.X.FillBytes(make([]byte, 32)), f.key.Y.FillBytes(make([]byte, 32))
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		f.t.Fatal(err)
	}
	data := authData("panel.example", 0x45, 0)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(f.rawID)))
	data = append(data, f.rawID...)
	data = append(data, cose...)
	attestation, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		f.t.Fatal(err)
	}
	return map[string]any{"id": b64(f.rawID), "rawId": b64(f.rawID), "type": "public-key", "authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{"credProps": map[string]any{"rk": true}}, "response": map[string]any{"clientDataJSON": b64(f.clientData("webauthn.create", challenge, testPasskeyOrigin)), "attestationObject": b64(attestation), "transports": []string{"internal"}}}
}

func (f *passkeyFixture) assertion(challenge, origin, rp, handle string, flags byte, count uint32) map[string]any {
	f.t.Helper()
	client := f.clientData("webauthn.get", challenge, origin)
	clientHash := sha256.Sum256(client)
	data := authData(rp, flags, count)
	signed := append(append([]byte{}, data...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, f.key, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return map[string]any{"id": b64(f.rawID), "rawId": b64(f.rawID), "type": "public-key", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": b64(client), "authenticatorData": b64(data), "signature": b64(signature), "userHandle": b64([]byte(handle))}}
}

func (f *passkeyFixture) register() string {
	f.t.Helper()
	id, challenge := f.begin("/api/admin/passkey/register/begin", map[string]any{"name": "My Passkey", "password": "test-password"})
	w := f.request("POST", "/api/admin/passkey/register/finish", map[string]any{"challenge_id": id, "credential": f.registration(challenge)}, testPasskeyOrigin)
	if w.Code != 200 {
		f.t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	return accounts.PasskeyID(f.rawID)
}

func TestPasskeyRegistrationLoginAndRevocation(t *testing.T) {
	f := newPasskeyFixture(t)
	credentialID := f.register()
	w := f.request("GET", "/api/admin/passkey", nil, testPasskeyOrigin)
	if w.Code != 200 || strings.Contains(w.Body.String(), "credential_data") {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	delete(f.cookies, "session_token")
	id, challenge := f.begin("/api/login/passkey/begin", map[string]any{})
	payload := map[string]any{"challenge_id": id, "credential": f.assertion(challenge, testPasskeyOrigin, "panel.example", f.user.UUID, 0x05, 1)}
	w = f.request("POST", "/api/login/passkey/finish", payload, testPasskeyOrigin)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	sessionCookie := f.cookies["session_token"]
	if sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("bad cookie: %#v", sessionCookie)
	}
	var row models.Session
	if err := dbcore.GetDBInstance().Where("session = ?", sessionCookie.Value).First(&row).Error; err != nil || row.LoginMethod != "passkey" || row.UUID != f.user.UUID {
		t.Fatalf("session: %#v %v", row, err)
	}
	if w = f.request("POST", "/api/login/passkey/finish", payload, testPasskeyOrigin); w.Code != 401 {
		t.Fatalf("replay: %d", w.Code)
	}
	if w = f.request("PATCH", "/api/admin/passkey/"+credentialID, map[string]any{"name": "Renamed"}, testPasskeyOrigin); w.Code != 200 {
		t.Fatalf("rename: %s", w.Body)
	}
	if w = f.request("DELETE", "/api/admin/passkey/"+credentialID, map[string]any{"password": "test-password"}, testPasskeyOrigin); w.Code != 200 {
		t.Fatalf("delete: %s", w.Body)
	}
	delete(f.cookies, "session_token")
	id, challenge = f.begin("/api/login/passkey/begin", map[string]any{})
	w = f.request("POST", "/api/login/passkey/finish", map[string]any{"challenge_id": id, "credential": f.assertion(challenge, testPasskeyOrigin, "panel.example", f.user.UUID, 0x05, 2)}, testPasskeyOrigin)
	if w.Code != 401 {
		t.Fatalf("deleted credential accepted: %d", w.Code)
	}
}

func TestPasskeyRejectsInvalidAssertions(t *testing.T) {
	for _, mode := range []string{"origin", "rp", "user", "uv", "signature", "browser", "account_deleted"} {
		t.Run(mode, func(t *testing.T) {
			f := newPasskeyFixture(t)
			f.register()
			delete(f.cookies, "session_token")
			id, challenge := f.begin("/api/login/passkey/begin", map[string]any{})
			origin, rp, handle, flags := testPasskeyOrigin, "panel.example", f.user.UUID, byte(0x05)
			switch mode {
			case "origin":
				origin = "https://evil.example"
			case "rp":
				rp = "evil.example"
			case "user":
				handle = "other-user"
			case "uv":
				flags = 0x01
			case "browser":
				delete(f.cookies, passkeyCookie)
			case "account_deleted":
				accounts.DeleteAccountByUsername(f.user.Username)
			}
			assertion := f.assertion(challenge, origin, rp, handle, flags, 1)
			if mode == "signature" {
				assertion["response"].(map[string]any)["signature"] = b64([]byte("invalid"))
			}
			w := f.request("POST", "/api/login/passkey/finish", map[string]any{"challenge_id": id, "credential": assertion}, testPasskeyOrigin)
			if w.Code != 401 {
				t.Fatalf("%s accepted: %d %s", mode, w.Code, w.Body)
			}
		})
	}
}

func TestPasskeyRegistrationRequiresFreshSessionAndProof(t *testing.T) {
	f := newPasskeyFixture(t)
	w := f.request("POST", "/api/admin/passkey/register/begin", map[string]any{"name": "key", "password": "wrong"}, testPasskeyOrigin)
	if w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	id, challenge := f.begin("/api/admin/passkey/register/begin", map[string]any{"name": "key", "password": "test-password"})
	accounts.DeleteSession(f.cookies["session_token"].Value)
	w = f.request("POST", "/api/admin/passkey/register/finish", map[string]any{"challenge_id": id, "credential": f.registration(challenge)}, testPasskeyOrigin)
	if w.Code != 401 {
		t.Fatalf("revoked session accepted: %d", w.Code)
	}
}

func TestPasskeyOriginDisabledLimitsAndTOTP(t *testing.T) {
	f := newPasskeyFixture(t)
	if w := f.request("POST", "/api/login/passkey/begin", nil, "https://evil.example"); w.Code != 403 {
		t.Fatalf("cross origin accepted: %d", w.Code)
	}
	config.Set(config.PasskeyEnabledKey, false)
	if w := f.request("POST", "/api/login/passkey/begin", nil, testPasskeyOrigin); w.Code != 403 {
		t.Fatalf("disabled accepted: %d", w.Code)
	}
	config.Set(config.PasskeyEnabledKey, true)
	secret := "JBSWY3DPEHPK3PXP"
	if err := accounts.Enable2Fa(f.user.UUID, secret); err != nil {
		t.Fatal(err)
	}
	if w := f.request("POST", "/api/admin/passkey/register/begin", map[string]any{"name": "key", "password": "test-password"}, testPasskeyOrigin); w.Code != 401 {
		t.Fatalf("TOTP bypass accepted: %d", w.Code)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.begin("/api/admin/passkey/register/begin", map[string]any{"name": "key", "2fa_code": code})
	request := httptest.NewRequest("POST", testPasskeyOrigin+"/api/login/passkey/finish", strings.NewReader(strings.Repeat("x", maxPasskeyBody+1)))
	request.Header.Set("Origin", testPasskeyOrigin)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, request)
	if w.Code != 413 {
		t.Fatalf("oversize: %d", w.Code)
	}
}

func TestPasskeyManagementDisabledAndAPIKey(t *testing.T) {
	f := newPasskeyFixture(t)
	id := f.register()
	config.Set(config.PasskeyEnabledKey, false)
	if w := f.request("GET", "/api/admin/passkey", nil, testPasskeyOrigin); w.Code != 200 {
		t.Fatalf("disabled list: %d %s", w.Code, w.Body)
	}
	if w := f.request("PATCH", "/api/admin/passkey/"+id, map[string]any{"name": "Still managed"}, testPasskeyOrigin); w.Code != 200 {
		t.Fatalf("disabled rename: %d %s", w.Code, w.Body)
	}
	config.Set(config.ApiKeyKey, "test-api-key-long-enough")
	r := httptest.NewRequest("GET", testPasskeyOrigin+"/api/admin/passkey", nil)
	r.Header.Set("Origin", testPasskeyOrigin)
	r.Header.Set("Authorization", "Bearer test-api-key-long-enough")
	r.AddCookie(f.cookies["session_token"])
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("API key accepted: %d %s", w.Code, w.Body)
	}
	config.Set(config.ApiKeyKey, "")
	if w := f.request("DELETE", "/api/admin/passkey/"+id, map[string]any{"password": "test-password"}, testPasskeyOrigin); w.Code != 200 {
		t.Fatalf("disabled delete: %d %s", w.Code, w.Body)
	}
}

func TestPasskeyOwnershipAndLastCredential(t *testing.T) {
	f := newPasskeyFixture(t)
	id := f.register()
	config.Set(config.DisablePasswordLoginKey, true)
	config.Set(config.OAuthEnabledKey, false)
	if w := f.request("DELETE", "/api/admin/passkey/"+id, map[string]any{"password": "test-password"}, testPasskeyOrigin); w.Code != 409 {
		t.Fatalf("last credential deleted: %d %s", w.Code, w.Body)
	}
	config.Set(config.DisablePasswordLoginKey, false)
	name, _ := passkeycore.RandomToken()
	other, err := accounts.CreateAccount(name, "other-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dbcore.GetDBInstance().Where("uuid = ?", other.UUID).Delete(&models.Session{})
		accounts.DeleteAccountByUsername(name)
	})
	token, err := accounts.CreateSession(other.UUID, 3600, "test", "127.0.0.1", "password")
	if err != nil {
		t.Fatal(err)
	}
	f.cookies["session_token"] = &http.Cookie{Name: "session_token", Value: token}
	if w := f.request("PATCH", "/api/admin/passkey/"+id, map[string]any{"name": "stolen"}, testPasskeyOrigin); w.Code != 404 {
		t.Fatalf("other user rename accepted: %d", w.Code)
	}
	if w := f.request("DELETE", "/api/admin/passkey/"+id, map[string]any{"password": "other-password"}, testPasskeyOrigin); w.Code != 409 {
		t.Fatalf("other user delete accepted: %d", w.Code)
	}
}

func TestPasskeyAccountChangesInvalidateRegistration(t *testing.T) {
	f := newPasskeyFixture(t)
	id, challenge := f.begin("/api/admin/passkey/register/begin", map[string]any{"name": "key", "password": "test-password"})
	if err := accounts.ForceResetPassword(f.user.Username, "changed-password"); err != nil {
		t.Fatal(err)
	}
	w := f.request("POST", "/api/admin/passkey/register/finish", map[string]any{"challenge_id": id, "credential": f.registration(challenge)}, testPasskeyOrigin)
	if w.Code != 401 {
		t.Fatalf("changed password challenge accepted: %d %s", w.Code, w.Body)
	}
}

func TestPasskeyRateLimitsDoNotShareProxyPeer(t *testing.T) {
	f := newPasskeyFixture(t)
	request := func(ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", testPasskeyOrigin+"/api/login/passkey/begin", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Origin", testPasskeyOrigin)
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < passkeycore.RequestsPerMinute; i++ {
		if w := request("198.51.100.1"); w.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	if w := request("198.51.100.1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("IP limit: %d", w.Code)
	}
	if w := request("198.51.100.2"); w.Code != http.StatusOK {
		t.Fatalf("another proxy client blocked: %d", w.Code)
	}
}

func TestPasskeyProofRateLimitCannotBeBypassedByForwardedIP(t *testing.T) {
	f := newPasskeyFixture(t)
	for i := 0; i <= passkeycore.RequestsPerMinute; i++ {
		r := httptest.NewRequest("POST", testPasskeyOrigin+"/api/admin/passkey/register/begin", strings.NewReader(`{"name":"test","password":"incorrect"}`))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Origin", testPasskeyOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		r.AddCookie(f.cookies["session_token"])
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		want := http.StatusUnauthorized
		if i == passkeycore.RequestsPerMinute {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("attempt %d: got %d, want %d", i, w.Code, want)
		}
	}
}
