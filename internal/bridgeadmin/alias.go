package bridgeadmin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

// aliasPattern is the shape of a Newsletter Source's alias: the lowercase
// local-part of its receiving address and its D1 sources.id. Letters, digits
// and inner hyphens only, so it is safe in an email address, a URL path
// (/feed/<alias>.atom) and the Worker's feed regex alike.
var aliasPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxAliasLength keeps the alias well inside an email local-part (64).
const maxAliasLength = 48

// ValidateAlias reports why alias cannot name a Newsletter Source, or nil.
// Uniqueness is the store's question, not this function's.
func ValidateAlias(alias string) error {
	switch {
	case alias == "":
		return errors.New("the alias is empty")
	case len(alias) > maxAliasLength:
		return errors.New("the alias is longer than 48 characters")
	case !aliasPattern.MatchString(alias):
		return errors.New("use lowercase letters, digits and single hyphens (not at the start or end)")
	}
	return nil
}

// fold turns the accented letters a newsletter name is likely to carry into
// ASCII, so "Café" suggests "cafe" rather than "caf".
var fold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ß", "ss", "æ", "ae", "œ", "oe",
)

// SuggestAlias derives an alias from a display name: lowercase, accents
// folded, every run of other characters collapsed to one hyphen. It drops
// apostrophes rather than splitting on them ("Ferriss'" gives "ferriss").
// The result may still fail ValidateAlias (an empty or overlong name); the
// form shows it as an editable suggestion, never as a final answer.
func SuggestAlias(displayName string) string {
	name := fold.Replace(strings.ToLower(displayName))
	var b strings.Builder
	pendingHyphen := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		case r == '\'' || r == '’':
		default:
			pendingHyphen = true
		}
	}
	alias := b.String()
	if len(alias) > maxAliasLength {
		alias = strings.TrimRight(alias[:maxAliasLength], "-")
	}
	return alias
}

// HashSecret is the Worker's hashSecret (email-worker/src/basicAuth.ts):
// lowercase hex SHA-256 of the UTF-8 password. D1 stores only this.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// NewSecret makes a feed password: 24 random bytes as 48 hex characters, the
// same strength the manual runbook's `openssl rand -hex 24` gave.
func NewSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
