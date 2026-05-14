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
	msg.FromFormat(m.config.FromName, m.config.Username)
	msg.To(to)
	msg.Subject(email.Subject)
	msg.SetBodyString(mail.TypeTextHTML, email.HTML)
	msg.AddAlternativeString(mail.TypeTextPlain, email.Text)

	c, err := mail.NewClient(
		m.config.Host,
		mail.WithPort(m.config.Port),
		mail.WithSMTPAuth(mail.SMTPAuthPlain),
		mail.WithUsername(m.config.Username),
		mail.WithPassword(m.config.Password),
	)

	if err != nil {
		return err
	}

	err = c.DialAndSendWithContext(ctx, msg)

	return err
}
