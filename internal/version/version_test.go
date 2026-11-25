package version

import "testing"

const sha = "3f9a2c1d5e7b9a0c2d4e6f8a1b3c5d7e9f0a1b2c"

func TestClassify(t *testing.T) {
	tests := []struct {
		name, version, commit string
		want                  Kind
	}{
		{"release", "0.2.0", sha, KindStable},
		{"release candidate", "0.3.0-rc1", sha, KindStable},
		{"rolling build", "latest", sha, KindEdge},
		{"local build", "0.1.0", "none", KindDev},
		{"local build without commit", "0.2.0", "", KindDev},
		{"CI build named after a commit", "ab12cd3", "ab12cd3", KindDev},
		{"CI build with no known commit", "ab12cd3", "none", KindDev},
		{"rolling build without commit", "latest", "none", KindDev},
		{"not a version", "0.2", sha, KindDev},
		{"with the v", "v0.2.0", sha, KindDev},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.version, tt.commit); got != tt.want {
				t.Fatalf("Classify(%q, %q) = %q, want %q", tt.version, tt.commit, got, tt.want)
			}
		})
	}
}

func TestDisplayOf(t *testing.T) {
	for _, tt := range []struct{ version, commit, want string }{
		{"0.2.0", sha, "v0.2.0"},
		{"latest", sha, "edge 3f9a2c1"},
		{"latest", "abc", "edge abc"},
		{"0.1.0", "none", "dev"},
		{"ab12cd3", "none", "dev"},
	} {
		if got := DisplayOf(tt.version, tt.commit); got != tt.want {
			t.Errorf("DisplayOf(%q, %q) = %q, want %q", tt.version, tt.commit, got, tt.want)
		}
	}
}

func TestDisplayUsesTheLinkedVariables(t *testing.T) {
	previousVersion, previousCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = previousVersion, previousCommit })

	Version, Commit = "1.4.2", sha
	if got := Display(); got != "v1.4.2" {
		t.Fatalf("Display() = %q", got)
	}
}
