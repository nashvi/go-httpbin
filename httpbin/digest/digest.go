// Package digest provides a limited implementation of HTTP Digest
// Authentication, as defined in RFC 2617.
//
// Only the "auth" QOP directive is handled at this time, and while support for
// the SHA-256 algorithm is implemented here it does not actually work in
// either Chrome or Firefox.
//
// For more info, see:
// https://tools.ietf.org/html/rfc2617
// https://en.wikipedia.org/wiki/Digest_access_authentication
package digest

import (
	"crypto"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// Check returns a bool indicating whether the request is correctly
// authenticated for the given username and password.
//
// TODO: use constant-time equality comparison.
func Check(req *http.Request, username, password string) bool {
	auth := parseAuthorizationHeader(req.Header.Get("Authorization"))
	if auth == nil {
		return false
	}
	expectedResponse := response(auth, password, req.Method, req.RequestURI)
	return compare(auth.response, expectedResponse)
}

// Challenge returns a WWW-Authenticate header value for the given realm and
// algorithm.
func Challenge(realm, algorithm string) string {
	entropy := make([]byte, 32)
	rand.Read(entropy)

	opaqueVal := entropy[:16]
	nonceVal := fmt.Sprintf("%s:%x", time.Now(), entropy[16:31])

	opaque := hash(opaqueVal, crypto.MD5)
	nonce := hash([]byte(nonceVal), crypto.MD5)

	return fmt.Sprintf("Digest qop=auth, realm=%s, algorithm=%s, nonce=%s, opaque=%s", realm, algorithm, nonce, opaque)
}

// authorization is the result of parsing an Authorization header
type authorization struct {
	algorithm crypto.Hash
	cnonce    string
	nc        string
	nonce     string
	opaque    string
	qop       string
	realm     string
	response  string
	uri       string
	username  string
}

// parseAuthorizationHeader parses an Authorization header into an
// Authorization struct, given a an authorization header like:
//
//    Authorization: Digest username="Mufasa",
//                         realm="testrealm@host.com",
//                         nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093",
//                         uri="/dir/index.html",
//                         qop=auth,
//                         nc=00000001,
//                         cnonce="0a4f113b",
//                         response="6629fae49393a05397450978507c4ef1",
//                         opaque="5ccc069c403ebaf9f0171e9517f40e41"
//
// If the given value does not contain a Digest authorization header, or is in
// some other way malformed, nil is returned.
//
// Example from Wikipedia: https://en.wikipedia.org/wiki/Digest_access_authentication#Example_with_explanation
func parseAuthorizationHeader(value string) *authorization {
	if value == "" {
		return nil
	}

	parts := strings.SplitN(value, " ", 2)
	if parts[0] != "Digest" || len(parts) != 2 {
		return nil
	}

	authInfo := parts[1]
	auth := parseDictHeader(authInfo)

	algo := crypto.MD5
	if strings.ToLower(auth["algorithm"]) == "sha-256" {
		algo = crypto.SHA256
	}

	return &authorization{
		algorithm: algo,
		cnonce:    auth["cnonce"],
		nc:        auth["nc"],
		nonce:     auth["nonce"],
		opaque:    auth["opaque"],
		qop:       auth["qop"],
		realm:     auth["realm"],
		response:  auth["response"],
		uri:       auth["uri"],
		username:  auth["username"],
	}
}

// parseDictHeader is a simplistic, buggy, and incomplete implementation of
// parsing key-value pairs from a header value into a map.
func parseDictHeader(value string) map[string]string {
	res := make(map[string]string)
	for _, pair := range strings.Split(value, ",") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 1 {
			res[parts[0]] = ""
			continue
		}
		key := parts[0]
		val := parts[1]
		if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
			val = val[1 : len(val)-1]
		}
		res[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return res
}

// hash generates the hex digest of the given data using the given hashing
// algorithm, which must be one of MD5 or SHA256.
func hash(data []byte, algorithm crypto.Hash) string {
	switch algorithm {
	case crypto.SHA256:
		return fmt.Sprintf("%x", sha256.Sum256(data))
	default:
		return fmt.Sprintf("%x", md5.Sum(data))
	}
}

// makeHA1 returns the HA1 hash, where
//
//     HA1 = H(A1) = H(username:realm:password)
//
// and H is one of MD5 or SHA256.
func makeHA1(realm, username, password string, algorithm crypto.Hash) string {
	A1 := fmt.Sprintf("%s:%s:%s", username, realm, password)
	return hash([]byte(A1), algorithm)
}

// makeHA2 returns the HA2 hash, where
//
//     HA2 = H(A2) = H(method:digestURI)
//
// and H is one of MD5 or SHA256.
func makeHA2(auth *authorization, method, uri string) string {
	A2 := fmt.Sprintf("%s:%s", method, uri)
	return hash([]byte(A2), auth.algorithm)
}

// Response calculates the correct digest auth response. If the qop directive's
// value is "auth" or "auth-int" , then compute the response as
//
//    RESPONSE = H(HA1:nonce:nonceCount:clientNonce:qop:HA2)
//
// and if the qop directive is unspecified, then compute the response as
//
//    RESPONSE = H(HA1:nonce:HA2)
//
// where H is one of MD5 or SHA256.
func response(auth *authorization, password, method, uri string) string {
	ha1 := makeHA1(auth.realm, auth.username, password, auth.algorithm)
	ha2 := makeHA2(auth, method, uri)

	var r string
	if auth.qop == "auth" || auth.qop == "auth-int" {
		r = fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, auth.nonce, auth.nc, auth.cnonce, auth.qop, ha2)
	} else {
		r = fmt.Sprintf("%s:%s:%s", ha1, auth.nonce, ha2)
	}
	return hash([]byte(r), auth.algorithm)
}

// compare is a constant-time string comparison
func compare(x, y string) bool {
	return subtle.ConstantTimeCompare([]byte(x), []byte(y)) == 1
}
