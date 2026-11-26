package proxmox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"pvc/internal/validate"
)

// expiringSoon is how long before its expiry a token is reported as expiring.
const expiringSoon = 14 * 24 * time.Hour

// Token is an API token of a user. Its secret is shown by Proxmox once, when the
// token is created, and is never part of this type.
type Token struct {
	UserID  string `json:"userid"`
	ID      string `json:"tokenid"`
	Comment string `json:"comment,omitempty"`
	// Expire is a Unix time; 0 means the token never expires.
	Expire int64 `json:"expire,omitempty"`
	// PrivSep is true when the token has its own permissions. When false, it
	// inherits every permission of its user.
	PrivSep Bool `json:"privsep"`
}

// FullID is the identifier of the token as Proxmox writes it in ACLs and as
// clients (OpenTofu, Grafana exporters) expect it: "user@realm!name".
func (t Token) FullID() string {
	return t.UserID + "!" + t.ID
}

// Expired reports whether the token has an expiry that is already past.
func (t Token) Expired(now time.Time) bool {
	return t.Expire > 0 && t.Expire <= now.Unix()
}

// ExpiresSoon reports whether the token expires within the next two weeks.
func (t Token) ExpiresSoon(now time.Time) bool {
	return t.Expire > now.Unix() && time.Unix(t.Expire, 0).Sub(now) <= expiringSoon
}

// ACLEntry is one permission of the access control list: a role given to a user,
// a group or a token on a path.
type ACLEntry struct {
	Path      string `json:"path"`
	RoleID    string `json:"roleid"`
	Type      string `json:"type"`
	UGID      string `json:"ugid"`
	Propagate Bool   `json:"propagate"`
}

// Role is a set of privileges that can be given in an ACL.
type Role struct {
	ID      string `json:"roleid"`
	Special Bool   `json:"special,omitempty"`
}

// UserTokens lists the tokens of one user.
func (c Client) UserTokens(ctx context.Context, userid string) ([]Token, error) {
	if err := validate.UserID(userid); err != nil {
		return nil, err
	}
	var tokens []Token
	if err := c.pveshJSON(ctx, &tokens, "get", "/access/users/"+userid+"/token", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list the tokens of user %s: %w", userid, err)
	}
	for i := range tokens {
		tokens[i].UserID = userid
	}

	return tokens, nil
}

// Tokens lists the tokens of the given users, sorted by user then name. A user
// whose tokens cannot be read does not hide the others: the tokens that were
// read are returned along with the joined errors.
func (c Client) Tokens(ctx context.Context, users []User) ([]Token, error) {
	var (
		all  []Token
		errs []error
	)
	for _, user := range users {
		tokens, err := c.UserTokens(ctx, user.UserID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, tokens...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].UserID != all[j].UserID {
			return all[i].UserID < all[j].UserID
		}
		return all[i].ID < all[j].ID
	})

	return all, errors.Join(errs...)
}

// ACLs lists every entry of the access control list.
func (c Client) ACLs(ctx context.Context) ([]ACLEntry, error) {
	var entries []ACLEntry
	if err := c.pveshJSON(ctx, &entries, "get", "/access/acl", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list the permissions: %w", err)
	}

	return entries, nil
}

// TokenPermissions returns the privileges a token really has, by path, sorted:
// what Proxmox computes from the ACL of the token, or from those of its user
// and of the groups of its user when the token has no privilege separation.
func (c Client) TokenPermissions(ctx context.Context, token Token) (map[string][]string, error) {
	if err := validate.UserID(token.UserID); err != nil {
		return nil, err
	}
	if err := validate.TokenID(token.ID); err != nil {
		return nil, err
	}
	var raw map[string]map[string]Bool
	if err := c.pveshJSON(ctx, &raw, "get", "/access/permissions", "-userid", token.FullID(), "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to read the privileges of token %s: %w", token.FullID(), err)
	}
	permissions := make(map[string][]string, len(raw))
	for path, privileges := range raw {
		for privilege, granted := range privileges {
			if granted {
				permissions[path] = append(permissions[path], privilege)
			}
		}
		sort.Strings(permissions[path])
		if len(permissions[path]) == 0 {
			delete(permissions, path)
		}
	}

	return permissions, nil
}

// TokenACLs keeps the entries that give a role to the token, sorted by path.
func TokenACLs(entries []ACLEntry, token Token) []ACLEntry {
	var kept []ACLEntry
	for _, entry := range entries {
		if entry.Type == "token" && strings.EqualFold(entry.UGID, token.FullID()) {
			kept = append(kept, entry)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Path != kept[j].Path {
			return kept[i].Path < kept[j].Path
		}
		return kept[i].RoleID < kept[j].RoleID
	})

	return kept
}
