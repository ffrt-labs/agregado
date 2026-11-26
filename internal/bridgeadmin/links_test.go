package bridgeadmin

import (
	"reflect"
	"testing"
)

func TestExtractLinks_TakesWebHrefsFromTheHTMLInOrderOnce(t *testing.T) {
	html := `<html><body>
		<p>Hi! <a href="https://tldr.tech/confirm?t=abc&amp;u=1">  Confirm   your
		subscription </a></p>
		<a href="mailto:dan@tldr.tech">mail us</a>
		<a href="javascript:alert(1)">evil</a>
		<a href="https://tldr.tech/confirm?t=abc&amp;u=1">again</a>
		<a href="http://tldr.tech/unsubscribe"><img src="cid:x"></a>
		<a>no href</a>
	</body></html>`
	got := ExtractLinks(html, "https://ignored.example/when-html-exists")
	want := []Link{
		{URL: "https://tldr.tech/confirm?t=abc&u=1", Text: "Confirm your subscription"},
		{URL: "http://tldr.tech/unsubscribe", Text: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links = %+v\nwant %+v", got, want)
	}
}

func TestExtractLinks_FallsBackToURLsInTheTextPart(t *testing.T) {
	text := "Confirm here: https://example.com/c?x=1.\nOr (https://example.com/other) - thanks"
	got := ExtractLinks("", text)
	want := []Link{{URL: "https://example.com/c?x=1"}, {URL: "https://example.com/other"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links = %+v\nwant %+v", got, want)
	}
}

func TestExtractLinks_NoBodyNoLinks(t *testing.T) {
	if got := ExtractLinks("", ""); len(got) != 0 {
		t.Errorf("links = %+v", got)
	}
}
