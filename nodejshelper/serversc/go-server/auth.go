package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	errTokenInvalid = errors.New("invalid token signature")
	errTokenExpired = errors.New("token expired")
)

// authToken is the payload produced by `socket.setAuthToken({...})` in
// serversc/lhc/worker.js. The very same structure is embedded in the signed
// token (JWT) which the browser stores and sends back with `#handshake`.
type authToken struct {
	Token       string `json:"token"`
	Exp         int64  `json:"exp"`
	Iat         int64  `json:"iat,omitempty"`
	ChanelName  string `json:"chanelName"`
	InstanceID  int    `json:"instance_id"`
	IsChatToken bool   `json:"isChatToken"`
	IsVisitor   bool   `json:"isVisitor"`

	// ChanelNameChat mirrors `socket.authToken.chanelNameChat`, which worker.js sets
	// inside the subscribe middleware. It is runtime only, never part of the token.
	ChanelNameChat string `json:"-"`
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomHexKey() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable in practice, fall back to time based
		return []byte(strconv.FormatInt(time.Now().UnixNano(), 16))
	}
	return []byte(hex.EncodeToString(b))
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// validateLoginToken is a port of the `login` handler from serversc/lhc/worker.js:
//
//	SHA1(ts + 'Visitor' + secretHash [ + '_' + chatId]) . ts   -> visitor
//	SHA1(ts + 'Operator' + secretHash) . ts                     -> operator
//
// The timestamp part must be newer than one hour.
func validateLoginToken(hash, chanelName, secretHash string, now int64) (isVisitor, isChatToken, ok bool) {
	parts := strings.Split(hash, ".")
	if len(parts) < 2 {
		return false, false, false
	}

	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false, false, false
	}

	// `tokenParts[1] > (secNow - 60*60)`
	if ts <= now-60*60 {
		return false, false, false
	}

	var visitorHash string
	if strings.Contains(chanelName, "chat_") {
		bits := strings.Split(chanelName, "_")
		chatID := bits[len(bits)-1]
		visitorHash = sha1Hex(parts[1] + "Visitor" + secretHash + "_" + chatID)
		isChatToken = true
	} else {
		visitorHash = sha1Hex(parts[1] + "Visitor" + secretHash)
	}
	operatorHash := sha1Hex(parts[1] + "Operator" + secretHash)

	if constantTimeEqual(parts[0], visitorHash) {
		return true, isChatToken, true
	}
	if constantTimeEqual(parts[0], operatorHash) {
		return false, isChatToken, true
	}
	return false, false, false
}

// lastTokenPart returns the value worker.js uses for the `vid` presence field:
// `authToken.chanelName.split('_')` last element.
func lastTokenPart(chanelName string) string {
	bits := strings.Split(chanelName, "_")
	return bits[len(bits)-1]
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// signToken creates a HS256 JWT, the same thing `jsonwebtoken.sign()` produced
// through sc-auth / socket.setAuthToken.
func signToken(key []byte, t *authToken) (string, error) {
	if t.Iat == 0 {
		t.Iat = time.Now().Unix()
	}

	header, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(t)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

// verifyToken validates a token produced by signToken and returns its payload.
func verifyToken(key []byte, signed string) (*authToken, error) {
	parts := strings.Split(signed, ".")
	if len(parts) != 3 {
		return nil, errTokenInvalid
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errTokenInvalid
	}
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, errTokenInvalid
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errTokenInvalid
	}

	var token authToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return nil, errTokenInvalid
	}

	if token.Exp > 0 && time.Now().Unix() >= token.Exp {
		return nil, errTokenExpired
	}

	return &token, nil
}

// tokenErrorName mirrors the names sc-errors gives to auth failures so the
// browser client reacts the same way (it clears the stored token on a bad one).
func tokenErrorName(err error) string {
	if errors.Is(err, errTokenExpired) {
		return "AuthTokenExpiredError"
	}
	return "AuthTokenInvalidError"
}
