package errors

import (
	"errors"
	"fmt"
)

var (
	ErrReservationNotFound           = errors.New("reservation not found")
	ErrHotelNotFound                 = errors.New("hotel not found")
	ErrNoRoomsAvailable              = errors.New("no rooms available")
	ErrReservationStorage            = errors.New("internal reservation storage error")
	ErrMapperConversion              = errors.New("reservation: failed mapping from schema to domain")
	ErrCreateReservationFailed       = errors.New("reservation: failed creating new reservation")
	ErrUpdateReservationStatusFailed = errors.New("reservation: failed updating reservation status")
	ErrGetRowsAffectedFailed         = errors.New("reservation: failed fetching updated rows")
)

type OpError struct {
	Op   string
	Kind error
	Err  error
}

func (e *OpError) Error() string {
	switch {
	case e.Op != "" && e.Kind != nil && e.Err != nil:
		return fmt.Sprintf("%s: %v: %v", e.Op, e.Kind, e.Err)
	case e.Op != "" && e.Kind != nil:
		return fmt.Sprintf("%s: %v", e.Op, e.Kind)
	case e.Kind != nil && e.Err != nil:
		return fmt.Sprintf("%v: %v", e.Kind, e.Err)
	case e.Kind != nil:
		return e.Kind.Error()
	case e.Err != nil:
		return e.Err.Error()
	default:
		return "unknown error"
	}
}

func (e *OpError) Is(target error) bool {
	return errors.Is(e.Kind, target)
}

func (e *OpError) Unwrap() error {
	return e.Err
}

func E(op string, kind error, err error) error {
	return &OpError{
		Op:   op,
		Kind: kind,
		Err:  err,
	}
}
