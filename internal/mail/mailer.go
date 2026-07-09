package mail

import (
	"bytes"
	"context"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/wneessen/go-mail"
)

type Mailer struct {
	config config.SMTP
}

func NewMailer(cfg config.SMTP) *Mailer {
	return &Mailer{config: cfg}
}

// dial builds an SMTP client using the configured credentials, falling back to
// a plain, unauthenticated, no-TLS connection for local dev sinks (e.g.
// Mailpit) that speak plain SMTP with no auth and no TLS. Forcing TLS in that
// case would fail the connection.
func (m *Mailer) dial() (*mail.Client, error) {
	if m.config.Password != "" {
		return mail.NewClient(
			m.config.Host,
			mail.WithPort(m.config.Port),
			mail.WithTLSPortPolicy(mail.TLSMandatory),
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(m.config.Username),
			mail.WithPassword(m.config.Password),
		)
	}

	return mail.NewClient(
		m.config.Host,
		mail.WithPort(m.config.Port),
		mail.WithTLSPortPolicy(mail.NoTLS),
	)
}

func (m *Mailer) Send(ctx context.Context, to, subject, html, text string) error {
	msg := mail.NewMsg()
	msg.FromFormat(m.config.FromName, m.config.FromMail)
	msg.To(to)
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextHTML, html)
	msg.AddAlternativeString(mail.TypeTextPlain, text)

	c, err := m.dial()
	if err != nil {
		return err
	}

	return c.DialAndSendWithContext(ctx, msg)
}

func (m *Mailer) SendAttachment(ctx context.Context, to, subject, bodyText, filename string, data []byte) error {
	msg := mail.NewMsg()
	msg.FromFormat(m.config.FromName, m.config.FromMail)
	msg.To(to)
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, bodyText)
	msg.AttachReader(filename, bytes.NewReader(data), mail.WithFileContentType("application/xml"))

	c, err := m.dial()
	if err != nil {
		return err
	}

	return c.DialAndSendWithContext(ctx, msg)
}
