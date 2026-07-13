package version

import "testing"

func TestCompareSemanticVersions(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "numeric components are not lexical", left: "v0.1.10", right: "v0.1.9", want: 1},
		{name: "equal", left: "v1.2.3", right: "1.2.3", want: 0},
		{name: "major", left: "v2.0.0", right: "v1.99.99", want: 1},
		{name: "prerelease before stable", left: "v1.0.0-rc.1", right: "v1.0.0", want: -1},
		{name: "numeric prerelease ordering", left: "v1.0.0-alpha.2", right: "v1.0.0-alpha.10", want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left, err := Parse(test.left)
			if err != nil {
				t.Fatalf("parse left: %v", err)
			}
			right, err := Parse(test.right)
			if err != nil {
				t.Fatalf("parse right: %v", err)
			}
			if got := Compare(left, right); got != test.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestIsStable(t *testing.T) {
	if !IsStable("v0.1.3") {
		t.Fatal("expected stable release")
	}
	if IsStable("v0.1.3-rc.1") {
		t.Fatal("expected prerelease to be unstable")
	}
	if IsStable("candidate-a1b2c3d") {
		t.Fatal("expected candidate value to be unstable")
	}
}
