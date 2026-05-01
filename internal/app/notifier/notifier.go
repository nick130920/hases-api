package notifier

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hases/hases-api/internal/app/mailer"
)

// Channel describe el canal por el que se envía un mensaje en el outbox.
type Channel string

const (
	ChannelEmail    Channel = "email"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelSMS      Channel = "sms"
)

// Sender es la interfaz que cualquier transporte (email, whatsapp, sms)
// debe implementar para que el worker del outbox lo invoque.
type Sender interface {
	Channel() Channel
	Send(to, subject, body string) error
}

// Notifier orquesta los Senders disponibles y procesa el outbox con backoff.
type Notifier struct {
	Pool    *pgxpool.Pool
	senders map[Channel]Sender
}

// New construye un Notifier registrando un EmailSender (si el mailer está
// habilitado). Otros canales se registran con `Register()`.
func New(pool *pgxpool.Pool, m *mailer.Mailer) *Notifier {
	n := &Notifier{Pool: pool, senders: map[Channel]Sender{}}
	if m != nil && m.Enabled() {
		n.Register(&EmailSender{Mailer: m})
	}
	return n
}

func (n *Notifier) Register(s Sender) {
	n.senders[s.Channel()] = s
}

// Enqueue persiste un mensaje en el outbox para envío diferido.
func (n *Notifier) Enqueue(ctx context.Context, channel Channel, to, subject, body string) (uuid.UUID, error) {
	if strings.TrimSpace(to) == "" {
		return uuid.Nil, errors.New("to required")
	}
	var id uuid.UUID
	err := n.Pool.QueryRow(ctx, `
		INSERT INTO outbox_messages (channel, to_address, subject, body)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		string(channel), to, subject, body).Scan(&id)
	return id, err
}

// Tick procesa hasta `batch` mensajes pendientes. Aplica backoff exponencial
// truncado a 1h. Idempotente: cada mensaje incrementa attempts en cada intento.
func (n *Notifier) Tick(ctx context.Context, batch int) (sent, failed int) {
	rows, err := n.Pool.Query(ctx, `
		SELECT id, channel, to_address, subject, body, attempts
		FROM outbox_messages
		WHERE status='pending' AND scheduled_for <= now()
		ORDER BY scheduled_for ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, batch)
	if err != nil {
		log.Printf("notifier.Tick query: %v", err)
		return 0, 0
	}
	defer rows.Close()
	type job struct {
		ID       uuid.UUID
		Channel  Channel
		To       string
		Subject  string
		Body     string
		Attempts int
	}
	var jobs []job
	for rows.Next() {
		var j job
		var ch string
		_ = rows.Scan(&j.ID, &ch, &j.To, &j.Subject, &j.Body, &j.Attempts)
		j.Channel = Channel(ch)
		jobs = append(jobs, j)
	}
	rows.Close()
	for _, j := range jobs {
		sender, ok := n.senders[j.Channel]
		if !ok {
			n.markFailed(ctx, j.ID, j.Attempts+1, "no sender for channel "+string(j.Channel))
			failed++
			continue
		}
		if err := sender.Send(j.To, j.Subject, j.Body); err != nil {
			n.markRetry(ctx, j.ID, j.Attempts+1, err.Error())
			failed++
			continue
		}
		_, _ = n.Pool.Exec(ctx, `
			UPDATE outbox_messages SET status='sent', sent_at=now(), attempts=$2, last_error=''
			WHERE id=$1`, j.ID, j.Attempts+1)
		sent++
	}
	return sent, failed
}

func (n *Notifier) markRetry(ctx context.Context, id uuid.UUID, attempts int, errMsg string) {
	if attempts >= 8 {
		n.markFailed(ctx, id, attempts, errMsg)
		return
	}
	delay := time.Duration(1<<attempts) * time.Minute
	if delay > time.Hour {
		delay = time.Hour
	}
	_, _ = n.Pool.Exec(ctx, `
		UPDATE outbox_messages
		SET attempts=$2, last_error=$3, scheduled_for=now() + ($4::interval)
		WHERE id=$1`,
		id, attempts, truncate(errMsg, 500), delay.String())
}

func (n *Notifier) markFailed(ctx context.Context, id uuid.UUID, attempts int, errMsg string) {
	_, _ = n.Pool.Exec(ctx, `
		UPDATE outbox_messages
		SET status='failed', attempts=$2, last_error=$3
		WHERE id=$1`,
		id, attempts, truncate(errMsg, 500))
}

func (n *Notifier) RequeueByID(ctx context.Context, id uuid.UUID) error {
	tag, err := n.Pool.Exec(ctx, `
		UPDATE outbox_messages
		SET status='pending', attempts=0, last_error='', scheduled_for=now()
		WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Run lanza un loop de ticks bloqueante. Útil para invocarlo en una goroutine
// desde main(). Cancelar `ctx` lo detiene de forma ordenada.
func (n *Notifier) Run(ctx context.Context, interval time.Duration, batch int) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if batch <= 0 {
		batch = 25
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.Tick(ctx, batch)
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// EmailSender adapta `mailer.Mailer` a la interfaz Sender.
type EmailSender struct {
	Mailer *mailer.Mailer
}

func (e *EmailSender) Channel() Channel { return ChannelEmail }
func (e *EmailSender) Send(to, subject, body string) error {
	if e.Mailer == nil {
		return errors.New("mailer not configured")
	}
	return e.Mailer.Send(to, subject, body)
}
