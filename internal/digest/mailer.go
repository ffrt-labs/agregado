package digest

import (
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

func(m *Mailer) Send(ctx context.Context, to string, email DigestEmail) error {
	msg := mail.NewMsg()
	msg.FromFormat(m.config.FromName, m.config.FromMail)
	msg.To(to)
	msg.Subject(email.Subject)
	msg.SetBodyString(mail.TypeTextHTML, email.HTML)
	msg.AddAlternativeString(mail.TypeTextPlain, email.Text)

	var c *mail.Client
	var err error

	if m.config.Password != "" {
		c, err = mail.NewClient(
			m.config.Host,
			mail.WithPort(m.config.Port),
			// mail.WithTLSPortPolicy(mail.NoTLS),
			mail.WithTLSPortPolicy(mail.TLSMandatory),
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(m.config.Username),
			mail.WithPassword(m.config.Password),
		)
	} else {
		// No password → local dev sink (e.g. Mailpit), which speaks plain SMTP
		// with no auth and no TLS. Forcing TLS here would fail the connection.
		c, err = mail.NewClient(
			m.config.Host,
			mail.WithPort(m.config.Port),
			mail.WithTLSPortPolicy(mail.NoTLS),
		)
	}


	if err != nil {
		return err
	}

	err = c.DialAndSendWithContext(ctx, msg)

	return err
}
