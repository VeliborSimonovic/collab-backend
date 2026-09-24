package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Claims struct {
	Sub   string `json:"sub"`
	Doc   string `json:"doc"`
	Role  string `json:"role"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Exp   int64  `json:"exp"`
}

type Authenticator interface {
	Authenticate(r *http.Request, doc string) (*Claims, error)
}

type tokenAuth struct {
	pub ed25519.PublicKey
}

type headerT struct {
	Alg string `json:"alg"`
}

type devAuth struct{}

var (
	ErrUnauthorized = errors.New("UNAUTHORIZED")
	ErrForbidden    = errors.New("FORBIDDENß")
)

func Sign(priv ed25519.PrivateKey, c Claims) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := ed25519.Sign(priv, []byte(header+"."+payload))
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func Verify(pub ed25519.PublicKey, token string, now time.Time) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrUnauthorized
	}

	header, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	payload, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	signature, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, ErrUnauthorized
	}

	var h headerT
	if err := json.Unmarshal(header, &h); err != nil || h.Alg != "EdDSA" {
		return nil, ErrUnauthorized
	}

	if len(signature) != ed25519.SignatureSize {
		return nil, ErrUnauthorized
	}
	if !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), signature) {
		return nil, ErrUnauthorized
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrUnauthorized
	}
	if claims.Exp <= now.Unix() || claims.Doc == "" || (claims.Role != "viewer" && claims.Role != "editor") {
		return nil, ErrUnauthorized
	}

	return &claims, nil
}

func NewTokenAuth(pub ed25519.PublicKey) Authenticator {
	return tokenAuth{pub: pub}
}

func (a tokenAuth) Authenticate(r *http.Request, doc string) (*Claims, error) {
	token := r.URL.Query().Get("token")

	if token == "" {
		header := r.Header.Get("Authorization")
		token = strings.TrimPrefix(header, "Bearer ")
	}
	if token == "" {
		return nil, ErrUnauthorized
	}

	claims, err := Verify(a.pub, token, time.Now())
	if err != nil {
		return nil, err
	}

	if claims.Doc != doc {
		return nil, ErrForbidden
	}

	return claims, nil
}

func NewDevAuth() Authenticator {
	fmt.Print("WARNING: COLLAB_PUBLIC_KEY is not set. DEV MODE: anyone can connect as an editor.")
	return &devAuth{}
}

func (devAuth) Authenticate(r *http.Request, doc string) (*Claims, error) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "anonymous"
	}

	return &Claims{
		Sub:   name,
		Doc:   doc,
		Role:  "editor",
		Name:  name,
		Color: "#888888",
	}, nil
}
