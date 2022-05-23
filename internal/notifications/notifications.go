package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/ianrose14/solarsnoop/internal/powertrend"
)

type Kind string

const (
	SMS    Kind = "sms"
	Email  Kind = "email"
	Ecobee Kind = "ecobee"
)

func (k Kind) IsValid() bool {
	switch k {
	case SMS, Email, Ecobee:
		return true
	default:
		return false
	}
}

type Sender struct {
	sendgridApiKey string
}

func NewSender(sendgridApiKey string) *Sender {
	return &Sender{
		sendgridApiKey: sendgridApiKey,
	}
}

func (n *Sender) Send(ctx context.Context, kind Kind, recipient string, phase powertrend.Phase) error {
	switch kind {
	case SMS:
		return n.sendSMSNotification(ctx, recipient, phase)
	case Email:
		return n.sendEmailNotification(ctx, recipient, phase)
	case Ecobee:
		return n.sendEcobeeNotification(ctx, recipient, phase)
	default:
		return fmt.Errorf("unsupported Kind: %s", kind)
	}
}

func (n *Sender) sendEcobeeNotification(context.Context, string, powertrend.Phase) error {
	return errors.New("unimplemented")
}

func (n *Sender) sendEmailNotification(context.Context, string, powertrend.Phase) error {
	/*
		from := mail.NewEmail("Example User", "test@example.com")
			subject := "Sending with Twilio SendGrid is Fun"
			to := mail.NewEmail("Example User", "test@example.com")
			plainTextContent := "and easy to do anywhere, even with Go"
			htmlContent := "<strong>and easy to do anywhere, even with Go</strong>"
			message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
			client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
			response, err := client.Send(message)
			if err != nil {
				log.Println(err)
			} else {
				fmt.Println(response.StatusCode)
				fmt.Println(response.Body)
				fmt.Println(response.Headers)
			}
	*/
	return errors.New("unimplemented")
}

func (n *Sender) sendSMSNotification(context.Context, string, powertrend.Phase) error {
	return errors.New("unimplemented")
}
