package proxmox

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"pvc/internal/command"
	"pvc/internal/validate"
)

// defaultTOTPIssuer is the name the authenticator application shows next to the
// account when the name of the node is not known.
const defaultTOTPIssuer = "PVE"

// totpSecretBytes is the size of a TOTP secret: 160 bits, the size RFC 4226 recommends.
const totpSecretBytes = 20

var (
	totpCodePattern    = regexp.MustCompile(`^[0-9]{6}$`)
	totpSecretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// NewTOTPSecret returns a random TOTP secret, base32 encoded without padding.
// A nil source means crypto/rand.
func NewTOTPSecret(source io.Reader) (string, error) {
	if source == nil {
		source = rand.Reader
	}
	raw := make([]byte, totpSecretBytes)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", fmt.Errorf("unable to generate a TOTP secret: %w", err)
	}

	return totpSecretEncoding.EncodeToString(raw), nil
}

// TOTPIssuer is the name shown next to the account in the authenticator
// application: the short name of the node, so that the accounts of several
// nodes can be told apart.
func (c Client) TOTPIssuer() string {
	node, err := c.nodeName()
	if err != nil {
		return defaultTOTPIssuer
	}
	node, _, _ = strings.Cut(node, ".")
	if node == "" || strings.ContainsAny(node, ":/?#& ") {
		return defaultTOTPIssuer
	}

	return node
}

// TOTPURI builds the otpauth URI an authenticator application reads from a QR
// code: SHA-1, 6 digits and 30 seconds, the defaults every application supports.
// The issuer is the prefix of the label (the application shows it next to the
// account). There is no separate issuer parameter: the text stays short, and a
// short text is a smaller QR code, which fits a terminal of 30 rows.
func TOTPURI(issuer string, userid string, secret string) (string, error) {
	if err := validate.UserID(userid); err != nil {
		return "", err
	}
	if _, err := totpSecretEncoding.DecodeString(secret); err != nil || secret == "" {
		return "", fmt.Errorf("invalid TOTP secret")
	}
	if issuer == "" || strings.ContainsAny(issuer, ":/?#& ") {
		return "", fmt.Errorf("invalid TOTP issuer %q", issuer)
	}
	label := url.PathEscape(issuer + ":" + userid)

	return "otpauth://totp/" + label + "?secret=" + secret, nil
}

// ValidTOTPCode reports whether code looks like a TOTP code: 6 digits.
func ValidTOTPCode(code string) bool {
	return totpCodePattern.MatchString(code)
}

// AddTOTP enrolls a TOTP factor for a user. Proxmox checks code against the
// secret of uri before it saves anything, so a wrong code enrolls nothing.
func (c Client) AddTOTP(ctx context.Context, userid string, uri string, code string, description string) error {
	if err := validate.UserID(userid); err != nil {
		return err
	}
	if !ValidTOTPCode(code) {
		return fmt.Errorf("invalid TOTP code: expected 6 digits")
	}
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		return fmt.Errorf("invalid TOTP uri")
	}
	// The user is in the path: POST /access/tfa has no handler.
	_, err := c.Runner.Run(ctx, "pvesh", "create", "/access/tfa/"+userid,
		"-type", "totp", "-totp", uri, "-value", code, "-description", description)
	if err != nil {
		// The secret and the code are in the arguments: they must not reach the log.
		err = command.RedactArgs(err, "-totp", "-value")
		return fmt.Errorf("unable to add a TOTP for user %s: %w", userid, err)
	}

	return nil
}

// AddRecoveryCodes creates the recovery codes of a user and returns them.
// Proxmox shows them once and keeps a single set per user.
func (c Client) AddRecoveryCodes(ctx context.Context, userid string, description string) ([]string, error) {
	if err := validate.UserID(userid); err != nil {
		return nil, err
	}
	result, err := c.Runner.Run(ctx, "pvesh", "create", "/access/tfa/"+userid,
		"-type", "recovery", "-description", description, "--output-format", "json")
	if err != nil {
		return nil, fmt.Errorf("unable to create recovery codes for user %s: %w", userid, err)
	}
	var reply struct {
		Recovery []string `json:"recovery"`
	}
	if err := json.Unmarshal(result.Stdout, &reply); err != nil {
		return nil, fmt.Errorf("unable to read recovery codes for user %s: %w", userid, err)
	}
	if len(reply.Recovery) == 0 {
		return nil, fmt.Errorf("unable to read recovery codes for user %s: none returned", userid)
	}

	return reply.Recovery, nil
}
