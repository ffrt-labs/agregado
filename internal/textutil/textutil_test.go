package textutil

import "testing"

func TestStrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "style block contents are removed, not just the tags",
			in:   "<style>.a{color:red}</style><p>Hi <b>x</b></p>",
			want: "Hi x",
		},
		{
			name: "script block contents are removed",
			in:   "<script>var x=1;</script>Text",
			want: "Text",
		},
		{
			name: "multiple style/script blocks all removed",
			in:   "<style>.a{}</style>Keep<script>f()</script> this <style>.b{}</style>",
			want: "Keep this",
		},
		{
			name: "style tag attributes and mixed case are handled",
			in:   "<STYLE type=\"text/css\">body{margin:0}</STYLE>Visible",
			want: "Visible",
		},
		{
			name: "plaintext passes through unchanged",
			in:   "just plain text",
			want: "just plain text",
		},
		{
			name: "entities are unescaped",
			in:   "a &amp; b &lt;3",
			want: "a & b <3",
		},
		{
			name: "ordinary tags still stripped as before",
			in:   "<div><p>Hello <em>world</em></p></div>",
			want: "Hello world",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Strip(tc.in); got != tc.want {
				t.Errorf("Strip(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClean(t *testing.T) {
	t.Run("truncates to max runes after stripping", func(t *testing.T) {
		in := "<style>.a{color:red}</style>" + "日本語abcdef"
		got := Clean(in, 5)
		want := "日本語ab"
		if got != want {
			t.Errorf("Clean(...) = %q, want %q", got, want)
		}
	})

	t.Run("no truncation needed", func(t *testing.T) {
		got := Clean("<p>short</p>", 100)
		if got != "short" {
			t.Errorf("Clean(...) = %q, want %q", got, "short")
		}
	})
}
