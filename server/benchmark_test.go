package server

import "testing"

func TestURLENcodeEscapesQueryValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "slashes and spaces", in: "web/app test", want: "web%2Fapp+test"},
		{name: "reserved characters", in: "a+b?c=d", want: "a%2Bb%3Fc%3Dd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := urlEncode(tc.in); got != tc.want {
				t.Fatalf("urlEncode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
