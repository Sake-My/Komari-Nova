package public

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Sake-My/Komari-Nova/database/accounts"
	"github.com/Sake-My/Komari-Nova/database/auditlog"
	"github.com/Sake-My/Komari-Nova/database/models"
	"github.com/Sake-My/Komari-Nova/internal/config"
	passkeycore "github.com/Sake-My/Komari-Nova/internal/passkey"
	"github.com/Sake-My/Komari-Nova/pkg/rpc"
	"github.com/Sake-My/Komari-Nova/utils"
	"github.com/Sake-My/Komari-Nova/web/api"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const passkeyCookie = "passkey_browser"
const maxPasskeyBody = 128 << 10

var passkeyChallenges = passkeycore.NewStore()

type passkeyProof struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	TwoFA    string `json:"2fa_code"`
}

type passkeyFinish struct {
	ChallengeID string          `json:"challenge_id"`
	Credential  json.RawMessage `json:"credential"`
}

func passkeyGuard(c *gin.Context, origin string) bool {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyBody)
	// 精确固定 Origin 与 WebAuthn 自身的 RP 校验同时生效，不继承宽泛 CORS 豁免。
	requestOrigin := c.GetHeader("Origin")
	if c.Request.Method == http.MethodGet && requestOrigin == "" {
		u, _ := url.Parse(origin)
		if c.Request.Host != u.Host || utils.GetScheme(c) != u.Scheme || c.GetHeader("Sec-Fetch-Site") != "same-origin" {
			api.RespondError(c, 403, "Passkey requests must be same-origin")
			return false
		}
	} else if requestOrigin != origin {
		api.RespondError(c, 403, "Passkey requests must be same-origin")
		return false
	}
	if !passkeyChallenges.Allow("ip:"+c.ClientIP(), time.Now()) {
		c.Header("Retry-After", "60")
		api.RespondError(c, http.StatusTooManyRequests, "Too many Passkey requests; retry later")
		return false
	}
	return true
}

func passkeyContext(c *gin.Context) (*webauthn.WebAuthn, string, bool) {
	if !config.PasskeyAvailable() {
		api.RespondError(c, http.StatusForbidden, "Passkey is disabled or its origin is not configured")
		return nil, "", false
	}
	raw, err := config.GetAs[string](config.PasskeyOriginKey, "")
	if err != nil {
		api.RespondError(c, 500, "Failed to read Passkey settings")
		return nil, "", false
	}
	origin, rpID, err := config.ParsePasskeyOrigin(raw)
	if err != nil {
		api.RespondError(c, 503, "Invalid Passkey origin configuration")
		return nil, "", false
	}
	if !passkeyGuard(c, origin) {
		return nil, "", false
	}
	name, err := config.GetAs[string](config.SitenameKey, "Komari")
	if err != nil || name == "" {
		name = "Komari"
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: name, RPOrigins: []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired, ResidentKey: protocol.ResidentKeyRequirementRequired},
		Timeouts:               webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeycore.ChallengeTTL}, Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeycore.ChallengeTTL}},
	})
	if err != nil {
		api.RespondError(c, 503, "Invalid Passkey configuration")
		return nil, "", false
	}
	return w, origin, true
}

// 关闭登录功能后仍可管理已有凭据；尚未配置 RP 时仅允许当前站点的管理请求。
func passkeyManagementContext(c *gin.Context) bool {
	raw, err := config.GetAs[string](config.PasskeyOriginKey, "")
	if err != nil {
		api.RespondError(c, 500, "Failed to read Passkey settings")
		return false
	}
	origin, _, err := config.ParsePasskeyOrigin(raw)
	if err != nil {
		origin = utils.GetScheme(c) + "://" + c.Request.Host
	}
	return passkeyGuard(c, origin)
}

func passkeyUser(c *gin.Context) (*accounts.PasskeyUser, string, bool) {
	p := api.GetPrincipal(c)
	if p == nil {
		p = api.IdentifyPrincipal(c)
	}
	if p.Type != rpc.PrincipalUser {
		api.RespondError(c, 401, "A signed-in user session is required")
		return nil, "", false
	}
	token, err := c.Cookie("session_token")
	if err != nil {
		api.RespondError(c, 401, "A signed-in user session is required")
		return nil, "", false
	}
	uuid, err := accounts.GetSession(token)
	if err != nil || uuid != p.UserUUID {
		api.RespondError(c, 401, "Session expired")
		return nil, "", false
	}
	u, err := accounts.LoadPasskeyUser(uuid)
	if err != nil {
		api.RespondError(c, 401, "Account unavailable")
		return nil, "", false
	}
	if !passkeyChallenges.Allow("user:"+uuid, time.Now()) {
		api.RespondError(c, 429, "Too many Passkey requests for this account")
		return nil, "", false
	}
	return u, token, true
}

func passkeyDecode(c *gin.Context, destination any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(destination); err != nil {
		api.RespondError(c, 400, "Invalid Passkey request")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		api.RespondError(c, 400, "Invalid Passkey request")
		return false
	}
	return true
}

func passkeyName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}

func verifyPasskeyProof(c *gin.Context, user models.User, proof passkeyProof) bool {
	if user.TwoFactor != "" {
		ok, err := accounts.Verify2Fa(user.UUID, proof.TwoFA)
		if err == nil && ok {
			return true
		}
	} else if uuid, ok := accounts.CheckPassword(user.Username, proof.Password); ok && uuid == user.UUID && proof.Password != "" {
		return true
	}
	api.RespondError(c, http.StatusUnauthorized, "Current password or 2FA verification failed")
	return false
}

func savePasskeyChallenge(c *gin.Context, challenge passkeycore.Challenge, options any) {
	browser, _ := c.Cookie(passkeyCookie)
	if len(browser) != 43 {
		var err error
		browser, err = passkeycore.RandomToken()
		if err != nil {
			api.RespondError(c, 500, "Failed to create Passkey challenge")
			return
		}
	}
	challenge.BrowserHash = passkeycore.TokenHash(browser)
	id, err := passkeyChallenges.Put(challenge, time.Now())
	if err != nil {
		api.RespondError(c, 429, "Too many pending Passkey requests")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: passkeyCookie, Value: browser, Path: "/api", MaxAge: int(passkeycore.ChallengeTTL.Seconds()), HttpOnly: true, Secure: strings.HasPrefix(challenge.Origin, "https://"), SameSite: http.SameSiteStrictMode})
	api.RespondSuccess(c, gin.H{"options": options, "challenge_id": id})
}

func PasskeyLoginBegin(c *gin.Context) {
	w, origin, ok := passkeyContext(c)
	if !ok {
		return
	}
	options, data, err := w.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		api.RespondError(c, 500, "Failed to start Passkey login")
		return
	}
	token, _ := c.Cookie("session_token")
	savePasskeyChallenge(c, passkeycore.Challenge{Operation: "login", SessionHash: passkeycore.TokenHash(token), Origin: origin, Data: *data}, options.Response)
}

func PasskeyLoginFinish(c *gin.Context) {
	w, origin, ok := passkeyContext(c)
	if !ok {
		return
	}
	var request passkeyFinish
	if !passkeyDecode(c, &request) {
		return
	}
	browser, _ := c.Cookie(passkeyCookie)
	tokenBeforeLogin, _ := c.Cookie("session_token")
	challenge, err := passkeyChallenges.Take(request.ChallengeID, "login", browser, tokenBeforeLogin, "", origin, time.Now())
	if err != nil {
		api.RespondError(c, 401, "Passkey challenge expired or already used")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(request.Credential))
	verified, credential, err := w.FinishPasskeyLogin(accounts.DiscoverPasskeyUser, challenge.Data, c.Request)
	if err != nil {
		api.RespondError(c, 401, "Passkey verification failed")
		return
	}
	user, ok := verified.(*accounts.PasskeyUser)
	if !ok {
		api.RespondError(c, 401, "Passkey account unavailable")
		return
	}
	token, err := accounts.CreatePasskeySession(user, credential, sessionCookieMaxAge, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		api.RespondError(c, 401, "Passkey changed or session could not be created; retry login")
		return
	}
	setSessionCookie(c, token, sessionCookieMaxAge)
	auditlog.Log(c.ClientIP(), user.User.UUID, "logged in (passkey)", "login")
	api.RespondSuccess(c, gin.H{"set-cookie": gin.H{"session_token": token}})
}

func PasskeyRegisterBegin(c *gin.Context) {
	w, origin, ok := passkeyContext(c)
	if !ok {
		return
	}
	user, token, ok := passkeyUser(c)
	if !ok {
		return
	}
	var proof passkeyProof
	if !passkeyDecode(c, &proof) {
		return
	}
	name, ok := passkeyName(proof.Name)
	if !ok {
		api.RespondError(c, 400, "Passkey name must contain 1 to 100 characters")
		return
	}
	if !verifyPasskeyProof(c, user.User, proof) {
		return
	}
	if len(user.Credentials) >= accounts.MaxPasskeysPerUser {
		api.RespondError(c, 400, "At most 20 Passkeys are allowed")
		return
	}
	options, data, err := w.BeginRegistration(user, webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired), webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()), webauthn.WithExtensions(webauthn.WithExtensionCredProps()))
	if err != nil {
		api.RespondError(c, 500, "Failed to start Passkey registration")
		return
	}
	savePasskeyChallenge(c, passkeycore.Challenge{Operation: "register", SessionHash: passkeycore.TokenHash(token), UserUUID: user.User.UUID, AccountStamp: accounts.PasskeyAccountStamp(user.User), Origin: origin, Name: name, Data: *data}, options.Response)
}

func PasskeyRegisterFinish(c *gin.Context) {
	w, origin, ok := passkeyContext(c)
	if !ok {
		return
	}
	user, token, ok := passkeyUser(c)
	if !ok {
		return
	}
	var request passkeyFinish
	if !passkeyDecode(c, &request) {
		return
	}
	browser, _ := c.Cookie(passkeyCookie)
	challenge, err := passkeyChallenges.Take(request.ChallengeID, "register", browser, token, user.User.UUID, origin, time.Now())
	if err != nil || challenge.AccountStamp != accounts.PasskeyAccountStamp(user.User) {
		api.RespondError(c, 401, "Passkey registration expired or account changed")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(request.Credential))
	credential, err := w.FinishRegistration(user, challenge.Data, c.Request)
	if err != nil {
		api.RespondError(c, 400, "Passkey registration verification failed")
		return
	}
	if err := accounts.AddPasskey(user.User, challenge.Name, credential); err != nil {
		api.RespondError(c, 409, "Passkey already registered or account changed")
		return
	}
	auditlog.Log(c.ClientIP(), user.User.UUID, "Passkey registered", "account")
	api.RespondSuccess(c, gin.H{"id": accounts.PasskeyID(credential.ID)})
}

func PasskeyList(c *gin.Context) {
	if !passkeyManagementContext(c) {
		return
	}
	user, _, ok := passkeyUser(c)
	if !ok {
		return
	}
	rows := user.Records
	if rows == nil {
		rows = []models.PasskeyCredential{}
	}
	api.RespondSuccess(c, rows)
}

func PasskeyRename(c *gin.Context) {
	if !passkeyManagementContext(c) {
		return
	}
	user, _, ok := passkeyUser(c)
	if !ok {
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if !passkeyDecode(c, &request) {
		return
	}
	name, ok := passkeyName(request.Name)
	if !ok {
		api.RespondError(c, 400, "Passkey name must contain 1 to 100 characters")
		return
	}
	if err := accounts.RenamePasskey(user.User.UUID, c.Param("id"), name); err != nil {
		api.RespondError(c, 404, "Passkey not found")
		return
	}
	api.RespondSuccess(c, nil)
}

func PasskeyDelete(c *gin.Context) {
	if !passkeyManagementContext(c) {
		return
	}
	user, _, ok := passkeyUser(c)
	if !ok {
		return
	}
	var proof passkeyProof
	if !passkeyDecode(c, &proof) || !verifyPasskeyProof(c, user.User, proof) {
		return
	}
	if err := accounts.DeletePasskey(user.User, c.Param("id")); err != nil {
		api.RespondError(c, 409, err.Error())
		return
	}
	auditlog.Log(c.ClientIP(), user.User.UUID, "Passkey deleted", "account")
	api.RespondSuccess(c, nil)
}
