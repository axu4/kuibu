package update

import "testing"

func TestParseVersion(t *testing.T) {
	valid := []struct {
		in   string
		want Version
	}{
		{"1.2.3", Version{Major: 1, Minor: 2, Patch: 3}},
		{"v1.2.3", Version{Major: 1, Minor: 2, Patch: 3}},
		{"V1.2.3", Version{Major: 1, Minor: 2, Patch: 3}},
		{"0.0.0-dev", Version{Major: 0, Minor: 0, Patch: 0, Prerelease: "dev"}},
		{"1.2.3-rc1", Version{Major: 1, Minor: 2, Patch: 3, Prerelease: "rc1"}},
		{"1.2.3-beta.1", Version{Major: 1, Minor: 2, Patch: 3, Prerelease: "beta.1"}},
		{"10.20.30", Version{Major: 10, Minor: 20, Patch: 30}},
	}
	for _, tc := range valid {
		got, err := ParseVersion(tc.in)
		if err != nil {
			t.Errorf("ParseVersion(%q) 返回错误: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseVersion(%q) = %+v，期望 %+v", tc.in, got, tc.want)
		}
	}

	invalid := []string{
		"",
		"v",
		"1",
		"1.2",
		"1.2.3.4",
		"1.2.x",
		"1.2.-3",
		"1.2.3-",
		"v1.2",
		"abc",
	}
	for _, in := range invalid {
		if _, err := ParseVersion(in); err == nil {
			t.Errorf("ParseVersion(%q) 应返回错误", in)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	ordered := []string{
		"0.0.0-dev",
		"0.0.0",
		"0.0.1",
		"0.1.0",
		"0.2.0-rc1",
		"0.2.0-rc2",
		"0.2.0",
		"0.2.1",
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-beta",
		"1.0.0",
		"1.0.1",
	}
	for i := 0; i < len(ordered); i++ {
		for j := 0; j < len(ordered); j++ {
			got, err := Compare(ordered[i], ordered[j])
			if err != nil {
				t.Fatalf("Compare(%q, %q) 返回错误: %v", ordered[i], ordered[j], err)
			}
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got != want {
				t.Errorf("Compare(%q, %q) = %d，期望 %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"v0.2.0", "0.1.0", true},
		// 开发版不提示更新。
		{"0.2.0", "0.0.0-dev", false},
		{"0.2.0", "dev", false},
		{"0.2.0", "", false},
		// latest 无法解析时返回 false。
		{"not-a-version", "0.1.0", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v，期望 %v", tc.latest, tc.current, got, tc.want)
		}
	}
}
