package mdm

import (
	"context"

	"go.uber.org/zap"

	"github.com/flarexio/mdm/checkin"
	"github.com/flarexio/mdm/command"
	"github.com/flarexio/mdm/enrollment"
)

func LoggingMiddleware(log *zap.Logger) ServiceMiddleware {
	return func(next Service) Service {
		return &loggingMiddleware{
			log.With(
				zap.String("service", "mdm"),
			),
			next,
		}
	}
}

type loggingMiddleware struct {
	log  *zap.Logger
	next Service
}

func (mw *loggingMiddleware) Authenticate(id enrollment.ID, msg *checkin.Authenticate) error {
	log := mw.log.With(
		zap.String("action", "authenticate"),
		zap.String("enrollment_id", id.String()),
		zap.String("udid", msg.UDID),
	)

	if err := mw.next.Authenticate(id, msg); err != nil {
		log.Error(err.Error())
		return err
	}

	log.Info("device authenticated")
	return nil
}

func (mw *loggingMiddleware) TokenUpdate(id enrollment.ID, msg *checkin.TokenUpdate) error {
	log := mw.log.With(
		zap.String("action", "token_update"),
		zap.String("enrollment_id", id.String()),
		zap.String("topic", msg.Topic),
	)

	if err := mw.next.TokenUpdate(id, msg); err != nil {
		log.Error(err.Error())
		return err
	}

	log.Info("push token updated")
	return nil
}

func (mw *loggingMiddleware) CheckOut(id enrollment.ID, msg *checkin.CheckOut) error {
	log := mw.log.With(
		zap.String("action", "check_out"),
		zap.String("enrollment_id", id.String()),
	)

	if err := mw.next.CheckOut(id, msg); err != nil {
		log.Error(err.Error())
		return err
	}

	log.Info("device checked out")
	return nil
}

func (mw *loggingMiddleware) CheckIn(id enrollment.ID, msg any) error {
	log := mw.log.With(
		zap.String("action", "check_in"),
		zap.String("enrollment_id", id.String()),
		zap.String("message_type", checkinType(msg)),
	)

	if err := mw.next.CheckIn(id, msg); err != nil {
		log.Error(err.Error())
		return err
	}

	log.Info("check-in dispatched")
	return nil
}

func (mw *loggingMiddleware) Enqueue(id enrollment.ID, cmd *command.Command) error {
	log := mw.log.With(
		zap.String("action", "enqueue"),
		zap.String("enrollment_id", id.String()),
		zap.String("command_uuid", cmd.CommandUUID),
		zap.String("request_type", string(cmd.Command.RequestType)),
	)

	if err := mw.next.Enqueue(id, cmd); err != nil {
		log.Error(err.Error())
		return err
	}

	log.Info("command enqueued")
	return nil
}

func (mw *loggingMiddleware) Command(id enrollment.ID, result *command.Result) (*command.Command, error) {
	log := mw.log.With(
		zap.String("action", "command"),
		zap.String("enrollment_id", id.String()),
		zap.String("status", string(result.Status)),
		zap.String("command_uuid", result.CommandUUID),
	)

	log.Debug("result payload", zap.ByteString("raw", result.Raw))

	next, err := mw.next.Command(id, result)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	if next == nil {
		log.Info("result recorded; queue empty")
		return nil, nil
	}

	log.Info("result recorded; next command dequeued",
		zap.String("next_command_uuid", next.CommandUUID),
		zap.String("next_request_type", string(next.Command.RequestType)),
	)
	return next, nil
}

func (mw *loggingMiddleware) Enrollments() ([]*enrollment.Enrollment, error) {
	log := mw.log.With(
		zap.String("action", "enrollments"),
	)

	enrollments, err := mw.next.Enrollments()
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	log.Info("enrollments listed", zap.Int("count", len(enrollments)))
	return enrollments, nil
}

func (mw *loggingMiddleware) Enrollment(id enrollment.ID) (*enrollment.Enrollment, error) {
	log := mw.log.With(
		zap.String("action", "enrollment"),
		zap.String("enrollment_id", id.String()),
	)

	e, err := mw.next.Enrollment(id)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	log.Info("enrollment found")
	return e, nil
}

func EnrollerLoggingMiddleware(log *zap.Logger) EnrollerMiddleware {
	return func(next Enroller) Enroller {
		return &enrollerLoggingMiddleware{
			log.With(
				zap.String("service", "mdm"),
				zap.String("component", "enroller"),
			),
			next,
		}
	}
}

type enrollerLoggingMiddleware struct {
	log  *zap.Logger
	next Enroller
}

func (mw *enrollerLoggingMiddleware) Profile(ctx context.Context, subject string) ([]byte, error) {
	log := mw.log.With(
		zap.String("action", "profile"),
		zap.String("subject", subject),
	)

	profile, err := mw.next.Profile(ctx, subject)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	log.Info("enrollment profile issued", zap.Int("bytes", len(profile)))
	return profile, nil
}

// checkinType reports the concrete check-in message type for logging, so a
// dispatched message is identifiable without decoding the body again.
func checkinType(msg any) string {
	switch msg.(type) {
	case *checkin.Authenticate:
		return "Authenticate"
	case *checkin.TokenUpdate:
		return "TokenUpdate"
	case *checkin.CheckOut:
		return "CheckOut"
	default:
		return "unknown"
	}
}
