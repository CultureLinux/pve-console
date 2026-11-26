package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"pvc/internal/proxmox"
)

func TestTokensTabOpensTheViewAndLoads(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"get /access/users --output-format json":                 `[{"userid":"alice@pve","enable":1}]`,
		"get /access/users/alice@pve/token --output-format json": `[{"tokenid":"opentofu","privsep":1}]`,
		"get /access/acl": `[{"path":"/vms","roleid":"PVEVMAdmin","type":"token","ugid":"alice@pve!opentofu","propagate":1}]`,
	}}
	m := newTestModel(runner)

	m, cmd := press(t, m, "T")
	if m.viewMode != viewTokens || !m.loading {
		t.Fatalf("view %q loading %v", m.viewMode, m.loading)
	}
	m, _ = execute(t, m, cmd)
	if m.loading || len(m.data.tokens) != 1 || len(m.data.acls) != 1 {
		t.Fatalf("loading %v tokens %v acls %v", m.loading, m.data.tokens, m.data.acls)
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "alice@pve!opentofu") || !strings.Contains(got, "/vms  PVEVMAdmin") {
		t.Fatalf("view: %s", got)
	}

	// Already loaded: the tab shows what it has, the refresh key reads again.
	m, _ = press(t, m, "g")
	m, cmd = press(t, m, "T")
	if cmd != nil || m.viewMode != viewTokens {
		t.Fatalf("cmd %v view %q", cmd, m.viewMode)
	}
	m, cmd = press(t, m, "r")
	if cmd == nil || !m.loading {
		t.Fatal("r must refresh the tokens")
	}
}

func TestTokensKeepWhatWasReadOnPartialFailure(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewTokens
	m, _ = deliver(t, m, tokensMsg{
		tokens: []proxmox.Token{{UserID: "alice@pve", ID: "a"}},
		err:    errors.New("unable to list the tokens of user bob@pve: denied"),
	})
	if len(m.data.tokens) != 1 {
		t.Fatalf("tokens: %v", m.data.tokens)
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "alice@pve!a") || !strings.Contains(got, "user bob@pve:") {
		t.Fatalf("view: %s", got)
	}
}

func TestTokensSelectionAndSearch(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewTokens
	m.data.tokens = []proxmox.Token{
		{UserID: "alice@pve", ID: "grafana", Comment: "metrics"},
		{UserID: "alice@pve", ID: "opentofu"},
		{UserID: "bob@pve", ID: "other"},
	}
	m, _ = press(t, m, "down", "down", "down")
	if token, _ := m.selectedTokenModel(); token.ID != "other" {
		t.Fatalf("selected %q", token.ID)
	}
	m.query = "metrics"
	m.clampSelections()
	if got := m.filteredTokens(); len(got) != 1 || got[0].ID != "grafana" || m.selectedToken != 0 {
		t.Fatalf("filtered %v selected %d", got, m.selectedToken)
	}
}

func TestTokenDetailWarnsAboutMissingPrivilegeSeparation(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewTokens
	m.data.tokens = []proxmox.Token{
		{UserID: "root@pam", ID: "wide", PrivSep: false},
		{UserID: "root@pam", ID: "empty", PrivSep: true},
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "every permission of root@pam") {
		t.Fatalf("wide token: %s", got)
	}
	m, _ = press(t, m, "down")
	if got := stripANSI(m.View()); !strings.Contains(got, "this token can do nothing") {
		t.Fatalf("token without ACL: %s", got)
	}
}

func TestTokenExpiryLabel(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name   string
		expire int64
		want   string
		warn   bool
	}{
		{"never", 0, "never", false},
		{"expired", now.Unix() - 10, "expired", true},
		{"soon", now.Add(time.Hour).Unix(), time.Unix(now.Add(time.Hour).Unix(), 0).Format("2006-01-02"), true},
		{"far", now.Add(100 * 24 * time.Hour).Unix(), time.Unix(now.Add(100*24*time.Hour).Unix(), 0).Format("2006-01-02"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warn := tokenExpiryLabel(proxmox.Token{Expire: tt.expire}, now)
			if got != tt.want || warn != tt.warn {
				t.Fatalf("got %q %v", got, warn)
			}
		})
	}
}

func TestDemoTokens(t *testing.T) {
	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m, cmd := press(t, m, "T")
	m, _ = execute(t, m, cmd)
	if m.data.tokenErr != nil || len(m.data.tokens) < 3 {
		t.Fatalf("err %v tokens %v", m.data.tokenErr, m.data.tokens)
	}
}

func TestTokenDetailShowsRolesAndEffectivePrivileges(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewTokens
	m.data.tokens = []proxmox.Token{
		{UserID: "alice@pve", ID: "tofu", PrivSep: true},
		{UserID: "bob@pve", ID: "wide"},
	}
	m.data.acls = []proxmox.ACLEntry{
		{Path: "/vms", RoleID: "PVEVMAdmin", Type: "token", UGID: "alice@pve!tofu", Propagate: true},
		{Path: "/", RoleID: "PVEAuditor", Type: "user", UGID: "bob@pve", Propagate: true},
		{Path: "/", RoleID: "Administrator", Type: "user", UGID: "alice@pve", Propagate: true},
	}
	m.data.tokenPerms = map[string]map[string][]string{
		"alice@pve!tofu": {"/vms": {"VM.Audit", "VM.Clone"}},
		"bob@pve!wide":   {"/": {"Sys.Audit"}},
	}

	got := stripANSI(m.View())
	for _, want := range []string{"ROLES", "/vms  PVEVMAdmin", "EFFECTIVE PRIVILEGES", "VM.Audit VM.Clone"} {
		if !strings.Contains(got, want) {
			t.Errorf("privilege separation: missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "Administrator") {
		t.Errorf("the roles of the user are not those of a token with its own: %s", got)
	}

	m, _ = press(t, m, "down")
	got = stripANSI(m.View())
	for _, want := range []string{"ROLES OF BOB@PVE", "/  PVEAuditor", "Sys.Audit"} {
		if !strings.Contains(got, want) {
			t.Errorf("no separation: missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "PVEVMAdmin") {
		t.Errorf("the roles of another token leaked: %s", got)
	}
}

func TestTokenPrivilegesAreReadForEveryToken(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"get /access/users --output-format json":                 `[{"userid":"alice@pve"}]`,
		"get /access/users/alice@pve/token --output-format json": `[{"tokenid":"aa"},{"tokenid":"bb"}]`,
		"get /access/acl":      `[]`,
		"-userid alice@pve!aa": `{"/":{"Sys.Audit":1}}`,
	}, failing: map[string]error{"-userid alice@pve!bb": errors.New("denied")}}
	m := newTestModel(runner)
	m, cmd := press(t, m, "T")
	m, _ = execute(t, m, cmd)
	if len(m.data.tokenPerms["alice@pve!aa"]["/"]) != 1 {
		t.Fatalf("perms: %v", m.data.tokenPerms)
	}
	if _, ok := m.data.tokenPerms["alice@pve!bb"]; ok || m.data.tokenErr == nil || len(m.data.tokens) != 2 {
		t.Fatalf("failure of one token: perms %v err %v", m.data.tokenPerms, m.data.tokenErr)
	}
}

// longTokenModel is a Tokens view whose detail is much longer than the panel.
func longTokenModel() Model {
	m := newTestModel(&fakeRunner{})
	m.width, m.height = 120, 30
	m.viewMode = viewTokens
	m.data.tokens = []proxmox.Token{{UserID: "alice@pve", ID: "aa", PrivSep: true}, {UserID: "alice@pve", ID: "bb", PrivSep: true}}
	var privileges []string
	for i := 0; i < 80; i++ {
		privileges = append(privileges, "Privilege.Number"+strings.Repeat("x", i%7)+string(rune('A'+i%26)))
	}
	m.data.tokenPerms = map[string]map[string][]string{
		"alice@pve!aa": {"/": privileges, "/vms": privileges},
		"alice@pve!bb": {"/": privileges[:2]},
	}
	return m
}

func TestDetailFocusScrollsTheDetailAndNotTheList(t *testing.T) {
	m := longTokenModel()
	m, _ = press(t, m, "enter")
	if !m.detailFocus {
		t.Fatal("enter must give the focus to the detail")
	}
	if got := stripANSI(m.footerText()); !strings.Contains(got, "↑↓ scroll") || !strings.Contains(got, "esc back to list") {
		t.Fatalf("footer: %q", got)
	}

	m, _ = press(t, m, "down", "down", "down")
	if m.detailScroll != 3 || m.selectedToken != 0 {
		t.Fatalf("scroll %d selected %d", m.detailScroll, m.selectedToken)
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "4-") {
		t.Fatalf("the title must show the visible range: %s", got)
	}

	m, _ = press(t, m, "up", "up", "up", "up", "up")
	if m.detailScroll != 0 {
		t.Fatalf("scroll above the top: %d", m.detailScroll)
	}
	m, _ = press(t, m, "end")
	total, visible := m.detailLineCount()
	if m.detailScroll != total-visible || total <= visible {
		t.Fatalf("end: scroll %d total %d visible %d", m.detailScroll, total, visible)
	}
	m, _ = press(t, m, "down", "down")
	if m.detailScroll != total-visible {
		t.Fatalf("scroll past the end: %d", m.detailScroll)
	}
	m, _ = press(t, m, "pgup")
	if m.detailScroll >= total-visible {
		t.Fatalf("pgup: %d", m.detailScroll)
	}

	m, _ = press(t, m, "esc")
	if m.detailFocus || m.viewMode != viewTokens {
		t.Fatalf("esc: focus %v view %q", m.detailFocus, m.viewMode)
	}
	m, _ = press(t, m, "down")
	if m.selectedToken != 1 || m.detailScroll != 0 {
		t.Fatalf("list: selected %d scroll %d", m.selectedToken, m.detailScroll)
	}
}

func TestDetailFocusKeepsTheGlobalKeys(t *testing.T) {
	m := longTokenModel()
	m, _ = press(t, m, "right")
	if !m.detailFocus {
		t.Fatal("right must give the focus to the detail")
	}
	m, _ = press(t, m, "g")
	if m.viewMode != viewGuests || m.detailFocus {
		t.Fatalf("a view key must work and drop the focus: view %q focus %v", m.viewMode, m.detailFocus)
	}
	m, _ = press(t, m, "T")
	if m.detailFocus || m.detailScroll != 0 {
		t.Fatalf("coming back: focus %v scroll %d", m.detailFocus, m.detailScroll)
	}
	m, _ = press(t, m, "right", "left")
	if m.detailFocus {
		t.Fatal("left must give the focus back to the list")
	}
}

func TestDetailFocusIsRefusedOnANarrowScreen(t *testing.T) {
	m := longTokenModel()
	m.width = 90
	m, _ = press(t, m, "enter")
	if m.detailFocus {
		t.Fatal("no detail panel is shown below 100 columns")
	}
}

func TestRightArrowDoesNotOpenTheConfigOfAGuest(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, cmd := press(t, m, "right")
	if m.showConfig || cmd != nil {
		t.Fatalf("showConfig %v cmd %v", m.showConfig, cmd != nil)
	}
}

func TestUserDetailListsItsTokensWithoutRoles(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewUsers
	m.data.users = []proxmox.User{{UserID: "alice@pve", Enable: true}, {UserID: "bob@pve", Enable: true}}
	m.data.tokensRead = true
	m.data.tokens = []proxmox.Token{
		{UserID: "alice@pve", ID: "opentofu", Comment: "OpenTofu token", PrivSep: true},
		{UserID: "alice@pve", ID: "old", Expire: 1000},
		{UserID: "bob@pve", ID: "grafana"},
	}
	m.data.acls = []proxmox.ACLEntry{{Path: "/vms", RoleID: "PVEVMAdmin", Type: "token", UGID: "alice@pve!opentofu"}}

	got := stripANSI(m.View())
	for _, want := range []string{"API TOKENS", "opentofu", "OpenTofu token", "old", "expired"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "grafana") || strings.Contains(got, "PVEVMAdmin") {
		t.Errorf("another user's token or a role in the Users view: %s", got)
	}

	m, _ = press(t, m, "down")
	if got := stripANSI(m.View()); !strings.Contains(got, "grafana") || strings.Contains(got, "opentofu") {
		t.Errorf("bob: %s", got)
	}
	m.data.tokens = nil
	if got := stripANSI(m.View()); !strings.Contains(got, "None for bob@pve.") {
		t.Errorf("no token: %s", got)
	}
	m.data.tokensRead = false
	if got := stripANSI(m.View()); !strings.Contains(got, "reading tokens") {
		t.Errorf("not read yet: %s", got)
	}
}

func TestUsersViewReadsTheTokensWithTheUsers(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"get /access/users --output-format json":                 `[{"userid":"alice@pve"}]`,
		"get /access/users/alice@pve/token --output-format json": `[{"tokenid":"opentofu"}]`,
		"get /access/acl": `[]`,
	}}
	m := newTestModel(runner)
	m, cmd := press(t, m, "u")
	m, _ = execute(t, m, cmd)
	if !m.data.tokensRead || len(m.data.tokens) != 1 {
		t.Fatalf("tokensRead %v tokens %v", m.data.tokensRead, m.data.tokens)
	}
	// The Tokens view then has them already.
	m, cmd = press(t, m, "T")
	if cmd != nil || len(m.data.tokens) != 1 {
		t.Fatalf("cmd %v tokens %v", cmd != nil, m.data.tokens)
	}
}
