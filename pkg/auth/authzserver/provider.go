package authzserver

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/flyteorg/flyteidl/gen/pb-go/flyteidl/service"
	"github.com/flyteorg/flytestdlib/logger"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/lestrrat-go/jwx/jwk"

	"github.com/ory/x/jwtx"

	"github.com/flyteorg/flyteadmin/pkg/auth/interfaces"

	"github.com/flyteorg/flyteadmin/pkg/auth"
	"github.com/flyteorg/flyteplugins/go/tasks/pluginmachinery/core"

	jwtgo "github.com/dgrijalva/jwt-go"
	fositeOAuth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/jwt"

	"github.com/flyteorg/flyteadmin/pkg/auth/config"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/storage"
)

const (
	ClientIDClaim = "client_id"
	UserIDClaim   = "user_info"
	ScopeClaim    = "scp"
	KeyIDClaim    = "key_id"
)

type Provider struct {
	fosite.OAuth2Provider
	audience  string
	cfg       config.AuthorizationServer
	publicKey []rsa.PublicKey
	keySet    jwk.Set
}

func (p Provider) PublicKeys() []rsa.PublicKey {
	return p.publicKey
}

func (p Provider) KeySet() jwk.Set {
	return p.keySet
}

// NewJWTSessionToken is a helper function for creating a new session.
func (p Provider) NewJWTSessionToken(subject string, userInfoClaims interface{}, appID, issuer, audience string) *fositeOAuth2.JWTSession {
	key, found := p.keySet.Get(0)
	keyID := ""
	if found {
		keyID = key.KeyID()
	}

	return &fositeOAuth2.JWTSession{
		JWTClaims: &jwt.JWTClaims{
			Audience:  []string{audience},
			Issuer:    issuer,
			Subject:   subject,
			ExpiresAt: time.Now().Add(p.cfg.AccessTokenLifespan.Duration),
			IssuedAt:  time.Now(),
			Extra: map[string]interface{}{
				ClientIDClaim: appID,
				UserIDClaim:   userInfoClaims,
			},
		},
		JWTHeader: &jwt.Headers{
			Extra: map[string]interface{}{
				KeyIDClaim: keyID,
			},
		},
	}
}

func (p Provider) ValidateAccessToken(ctx context.Context, tokenStr string) (interfaces.IdentityContext, error) {
	// Parse and validate the token.
	parsedToken, err := jwtgo.Parse(tokenStr, func(t *jwtgo.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwtgo.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}

		publicKey := &rsa.PublicKey{}
		if keyID, found := t.Header[KeyIDClaim]; !found {
			return &p.PublicKeys()[0], nil
		} else if key, found := p.keySet.LookupKeyID(keyID.(string)); !found {
			return &p.PublicKeys()[0], nil
		} else if err := key.Raw(publicKey); err != nil {
			logger.Errorf(ctx, "Failed to load public key from key [%v]. Will default to the first key. Error: %v", keyID)
			return &p.PublicKeys()[0], nil
		} else {
			return publicKey, nil
		}
	})

	if err != nil {
		return nil, err
	}

	if !parsedToken.Valid {
		return nil, fmt.Errorf("parsed token is invalid")
	}

	claimsRaw := parsedToken.Claims.(jwtgo.MapClaims)
	claims := jwtx.ParseMapStringInterfaceClaims(claimsRaw)
	if len(claims.Audience) != 1 {
		return nil, fmt.Errorf("expected exactly one granted audience. found [%v]", len(claims.Audience))
	}

	if claims.Audience[0] != p.audience {
		return nil, fmt.Errorf("invalid audience [%v]", claims.Audience[0])
	}

	userInfoRaw := claimsRaw[UserIDClaim].(map[string]interface{})
	raw, err := json.Marshal(userInfoRaw)
	if err != nil {
		return nil, err
	}

	userInfo := &service.UserInfoResponse{}
	if err = json.Unmarshal(raw, userInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user info claim into UserInfo type. Error: %w", err)
	}

	return auth.NewIdentityContext(claims.Audience[0], claims.Subject, claimsRaw[ClientIDClaim].(string),
		claims.IssuedAt, sets.NewString(interfaceSliceToStringSlice(claimsRaw[ScopeClaim].([]interface{}))...), userInfo), nil
}

// Creates a new OAuth2 Provider that is able to do OAuth 2-legged and 3-legged flows.
// It'll lookup auth.SecretClaimSymmetricKey and auth.SecretTokenSigningRSAKey secrets from the secret manager to use to sign
// and generate hashes for tokens. The RSA Private key is expected to be in PEM format with the public key embedded.
// Use auth.GetInitSecretsCommand() to generate new valid secrets that will be accepted by this provider.
// The auth.SecretClaimSymmetricKey must be a 32-bytes long key in Base64Encoding.
func NewProvider(ctx context.Context, cfg config.AuthorizationServer, audience string, sm core.SecretManager) (Provider, error) {
	// fosite requires four parameters for the server to get up and running:
	// 1. config - for any enforcement you may desire, you can do this using `compose.Config`. You like PKCE, enforce it!
	// 2. store - no auth service is generally useful unless it can remember clients and users.
	//    fosite is incredibly composable, and the store parameter enables you to build and BYODb (Bring Your Own Database)
	// 3. secret - required for code, access and refresh token generation.
	// 4. privateKey - required for id/jwt token generation.

	composeConfig := &compose.Config{
		AccessTokenLifespan: cfg.AccessTokenLifespan.Duration,
		RefreshTokenScopes:  []string{refreshTokenScope},
	}

	// This secret is used to encryptString/decrypt challenge code to maintain a stateless authcode token.
	tokenHashBase64, err := sm.Get(ctx, cfg.ClaimSymmetricEncryptionKeySecretName)
	if err != nil {
		return Provider{}, fmt.Errorf("failed to read secretTokenHash file. Error: %w", err)
	}

	secret, err := base64.RawStdEncoding.DecodeString(tokenHashBase64)
	if err != nil {
		return Provider{}, fmt.Errorf("failed to decode token hash using base64 encoding. Error: %w", err)
	}

	// privateKey is used to sign JWT tokens. The default strategy uses RS256 (RSA Signature with SHA-256)
	privateKeyPEM, err := sm.Get(ctx, cfg.TokenSigningRSAKeySecretName)
	if err != nil {
		return Provider{}, fmt.Errorf("failed to read token signing RSA Key. Error: %w", err)
	}

	block, _ := pem.Decode([]byte(privateKeyPEM))
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return Provider{}, fmt.Errorf("failed to parse PKCS1PrivateKey. Error: %w", err)
	}

	// Build an in-memory store with static clients defined in Config. This gives us the potential to move the clients
	// storage into DB and allow registration of new clients to users.
	store := &StatelessTokenStore{
		MemoryStore: &storage.MemoryStore{
			IDSessions:             make(map[string]fosite.Requester),
			Clients:                toClientIface(cfg.StaticClients),
			AuthorizeCodes:         map[string]storage.StoreAuthorizeCode{},
			AccessTokens:           map[string]fosite.Requester{},
			RefreshTokens:          map[string]storage.StoreRefreshToken{},
			PKCES:                  map[string]fosite.Requester{},
			AccessTokenRequestIDs:  map[string]string{},
			RefreshTokenRequestIDs: map[string]string{},
			IssuerPublicKeys:       map[string]storage.IssuerPublicKeys{},
		},
	}

	sec := [auth.SymmetricKeyLength]byte{}
	copy(sec[:], secret)
	codeProvider := NewStatelessCodeProvider(cfg, sec, compose.NewOAuth2JWTStrategy(privateKey, nil))

	// Build a fosite instance with all OAuth2 and OpenID Connect handlers enabled, plugging in our configurations as specified above.
	oauth2Provider := composeOAuth2Provider(codeProvider, composeConfig, store, privateKey)
	store.JWTStrategy = &jwt.RS256JWTStrategy{
		PrivateKey: privateKey,
	}
	store.encryptor = codeProvider

	publicKeys := []rsa.PublicKey{privateKey.PublicKey}

	// Try to load old key to validate tokens using it to support key rotation.
	privateKeyPEM, err = sm.Get(ctx, cfg.OldTokenSigningRSAKeySecretName)
	if err == nil {
		block, _ = pem.Decode([]byte(privateKeyPEM))
		oldPrivateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return Provider{}, fmt.Errorf("failed to parse PKCS1PrivateKey. Error: %w", err)
		}

		publicKeys = append(publicKeys, oldPrivateKey.PublicKey)
	}

	keysSet, err := newJSONWebKeySet(publicKeys)
	if err != nil {
		return Provider{}, err
	}

	return Provider{
		OAuth2Provider: oauth2Provider,
		audience:       audience,
		publicKey:      publicKeys,
		keySet:         keysSet,
	}, nil
}
