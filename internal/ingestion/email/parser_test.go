package email

import "testing"

func TestResolveSender(t *testing.T) {
	cases := []struct {
		name         string
		headers      Headers
		envelopeFrom string
		wantIdentity string
		wantAddress  string
		wantName     string
	}{
		{
			name: "list-id preferred over from-header address",
			headers: Headers{
				"list-id": "TLDR <tldr.lists.tldrnewsletter.com>",
				"from":    "TLDR <dan@tldrnewsletter.com>",
			},
			envelopeFrom: "0100019f5b865e15-f4ae5968@dailyupdate.tldrnewsletter.com",
			wantIdentity: "tldr.lists.tldrnewsletter.com",
			wantAddress:  "dan@tldrnewsletter.com",
			wantName:     "TLDR",
		},
		{
			name: "falls back to from-header address when list-id absent",
			headers: Headers{
				"from": "Newsletters <newsletters@felipefreitas.dev>",
			},
			envelopeFrom: "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantIdentity: "newsletters@felipefreitas.dev",
			wantAddress:  "newsletters@felipefreitas.dev",
			wantName:     "Newsletters",
		},
		{
			name:         "falls back to envelope sender when headers missing",
			headers:      Headers{},
			envelopeFrom: "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantIdentity: "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantAddress:  "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantName:     "bounces+27731166-8a63@em6054.thenewscc.com.br",
		},
		{
			name: "list-id without angle brackets used as-is",
			headers: Headers{
				"list-id": "list.example.com",
			},
			envelopeFrom: "envelope@example.com",
			wantIdentity: "list.example.com",
			wantAddress:  "envelope@example.com",
			wantName:     "envelope@example.com",
		},
		{
			name: "malformed from header falls back to envelope",
			headers: Headers{
				"from": "not a valid address",
			},
			envelopeFrom: "envelope@example.com",
			wantIdentity: "envelope@example.com",
			wantAddress:  "envelope@example.com",
			wantName:     "envelope@example.com",
		},
		{
			name: "from header with no display name uses address as name",
			headers: Headers{
				"from": "dan@tldrnewsletter.com",
			},
			envelopeFrom: "0100019f5b865e15@dailyupdate.tldrnewsletter.com",
			wantIdentity: "dan@tldrnewsletter.com",
			wantAddress:  "dan@tldrnewsletter.com",
			wantName:     "dan@tldrnewsletter.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSender(tc.headers, tc.envelopeFrom)
			if got.Identity != tc.wantIdentity {
				t.Errorf("Identity = %q, want %q", got.Identity, tc.wantIdentity)
			}
			if got.Address != tc.wantAddress {
				t.Errorf("Address = %q, want %q", got.Address, tc.wantAddress)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
		})
	}
}

func TestResolveSenderStableAcrossRotatingEnvelope(t *testing.T) {
	headers := Headers{
		"list-id": "TLDR <tldr.lists.tldrnewsletter.com>",
		"from":    "TLDR <dan@tldrnewsletter.com>",
	}

	envelopes := []string{
		"0100019f5b2407cf-98d50c0e-1305-4815-98ae-43a5c29eba5b-000000@dailyupdate.tldrnewsletter.com",
		"0100019f5b4de805-4d0927be-2b04-4dc2-9704-5bec308ed9a0-000000@dailyupdate.tldrnewsletter.com",
		"0100019f5b865e15-f4ae5968-8f69-4a56-98d4-8e6289fcca6f-000000@dailyupdate.tldrnewsletter.com",
	}

	var firstIdentity string
	for i, envelope := range envelopes {
		sender := resolveSender(headers, envelope)
		if i == 0 {
			firstIdentity = sender.Identity
			continue
		}
		if sender.Identity != firstIdentity {
			t.Errorf("identity changed across rotating envelope senders: %q != %q", sender.Identity, firstIdentity)
		}
	}
}
