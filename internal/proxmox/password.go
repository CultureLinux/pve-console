package proxmox

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"pvc/internal/command"
	"pvc/internal/validate"
)

// Proxmox accepts passwords of 8 to 64 characters.
const (
	minPasswordLength = 8
	maxPasswordLength = 64
)

// ValidatePassword checks a password the way Proxmox does, before anything is
// sent: 8 to 64 characters, none of them a control character.
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < minPasswordLength || length > maxPasswordLength {
		return fmt.Errorf("the password must have %d to %d characters", minPasswordLength, maxPasswordLength)
	}
	if strings.IndexFunc(password, unicode.IsControl) >= 0 {
		return fmt.Errorf("the password must not contain control characters")
	}

	return nil
}

// PasswordRealm returns the realm of a user id and whether its password can be
// changed through the Proxmox API: only the "pve" realm (stored by Proxmox) and
// the "pam" realm (the Linux accounts of the node) can. LDAP, AD and OpenID
// passwords belong to their directory.
func PasswordRealm(userid string) (string, bool) {
	_, realm, found := strings.Cut(userid, "@")
	if !found {
		return "", false
	}

	return realm, realm == "pve" || realm == "pam"
}

// SetPassword changes the password of a user. Run as root, Proxmox does not ask
// for the current password.
func (c Client) SetPassword(ctx context.Context, userid string, password string) error {
	if err := validate.UserID(userid); err != nil {
		return err
	}
	if realm, ok := PasswordRealm(userid); !ok {
		return fmt.Errorf("the password of realm %q cannot be changed here", realm)
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	_, err := c.Runner.Run(ctx, "pvesh", "set", "/access/password", "-userid", userid, "-password", password)
	if err != nil {
		// The password is in the arguments: it must not reach the log.
		err = command.RedactArgs(err, "-password")
		return fmt.Errorf("unable to change the password of user %s: %w", userid, err)
	}

	return nil
}
