package proxmox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pvc/internal/command"
)

func TestUserTokens(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /access/users/alice@pve/token --output-format json": {Stdout: []byte(
			`[{"tokenid":"grafana","comment":"metrics","privsep":1,"expire":0},{"tokenid":"tofu","privsep":"0","expire":1893456000}]`)},
	}}
	tokens, err := NewClient(runner).UserTokens(context.Background(), "alice@pve")
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("%d tokens", len(tokens))
	}
	if tokens[0].FullID() != "alice@pve!grafana" || !bool(tokens[0].PrivSep) || tokens[0].Comment != "metrics" {
		t.Errorf("first token: %+v", tokens[0])
	}
	if bool(tokens[1].PrivSep) || tokens[1].Expire != 1893456000 {
		t.Errorf("second token: %+v", tokens[1])
	}
}

func TestUserTokensRefusesBadUserBeforeAnyCommand(t *testing.T) {
	runner := &command.MockRunner{}
	for _, userid := range []string{"", "alice", "../x@pve", "a b@pve"} {
		if _, err := NewClient(runner).UserTokens(context.Background(), userid); err == nil {
			t.Errorf("%q accepted", userid)
		}
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("commands run: %v", runner.Calls)
	}
}

func TestTokensKeepsWhatWasReadWhenOneUserFails(t *testing.T) {
	failure := errors.New("boom")
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /access/users/bob@pve/token --output-format json":   {Stdout: []byte(`[{"tokenid":"b2"},{"tokenid":"b1"}]`)},
			"pvesh get /access/users/alice@pve/token --output-format json": {Stdout: []byte(`[{"tokenid":"a1"}]`)},
		},
		Errors: map[string]error{"pvesh get /access/users/carol@pve/token --output-format json": failure},
	}
	users := []User{{UserID: "bob@pve"}, {UserID: "carol@pve"}, {UserID: "alice@pve"}}
	tokens, err := NewClient(runner).Tokens(context.Background(), users)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "carol@pve") {
		t.Fatalf("error: %v", err)
	}
	var got []string
	for _, token := range tokens {
		got = append(got, token.FullID())
	}
	if want := "alice@pve!a1 bob@pve!b1 bob@pve!b2"; strings.Join(got, " ") != want {
		t.Fatalf("tokens %v, want %s", got, want)
	}
}

func TestTokenExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name          string
		expire        int64
		expired, soon bool
	}{
		{name: "never", expire: 0},
		{name: "past", expire: now.Unix() - 1, expired: true},
		{name: "now", expire: now.Unix(), expired: true},
		{name: "soon", expire: now.Add(24 * time.Hour).Unix(), soon: true},
		{name: "far", expire: now.Add(90 * 24 * time.Hour).Unix()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := Token{Expire: tt.expire}
			if token.Expired(now) != tt.expired || token.ExpiresSoon(now) != tt.soon {
				t.Fatalf("expired %v soon %v", token.Expired(now), token.ExpiresSoon(now))
			}
		})
	}
}

func TestTokenACLs(t *testing.T) {
	entries := []ACLEntry{
		{Path: "/vms", RoleID: "PVEVMAdmin", Type: "token", UGID: "alice@pve!tofu"},
		{Path: "/", RoleID: "PVEAuditor", Type: "token", UGID: "alice@pve!tofu"},
		{Path: "/", RoleID: "Administrator", Type: "user", UGID: "alice@pve"},
		{Path: "/", RoleID: "PVEAuditor", Type: "token", UGID: "alice@pve!grafana"},
	}
	got := TokenACLs(entries, Token{UserID: "alice@pve", ID: "tofu"})
	if len(got) != 2 || got[0].Path != "/" || got[1].Path != "/vms" {
		t.Fatalf("%+v", got)
	}
}

func TestTokenPermissions(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /access/permissions -userid alice@pve!tofu --output-format json": {Stdout: []byte(
			`{"/vms":{"VM.Audit":1,"VM.Console":1,"VM.Config.CPU":0},"/":{"Sys.Audit":"1"},"/pool/x":{"VM.Audit":0}}`)},
	}}
	got, err := NewClient(runner).TokenPermissions(context.Background(), Token{UserID: "alice@pve", ID: "tofu"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || strings.Join(got["/vms"], " ") != "VM.Audit VM.Console" || strings.Join(got["/"], " ") != "Sys.Audit" {
		t.Fatalf("%v", got)
	}
}

func TestTokenPermissionsRefusesBadIDsBeforeAnyCommand(t *testing.T) {
	runner := &command.MockRunner{}
	for _, token := range []Token{{UserID: "alice", ID: "tofu"}, {UserID: "alice@pve", ID: "a b"}, {UserID: "alice@pve", ID: "../x"}} {
		if _, err := NewClient(runner).TokenPermissions(context.Background(), token); err == nil {
			t.Errorf("%+v accepted", token)
		}
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("commands run: %v", runner.Calls)
	}
}

func TestTokenPermissionsError(t *testing.T) {
	failure := errors.New("denied")
	runner := &command.MockRunner{Errors: map[string]error{
		"pvesh get /access/permissions -userid alice@pve!tofu --output-format json": failure,
	}}
	if _, err := NewClient(runner).TokenPermissions(context.Background(), Token{UserID: "alice@pve", ID: "tofu"}); !errors.Is(err, failure) {
		t.Fatalf("%v", err)
	}
}
