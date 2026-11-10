package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Encode writes e as one line of a session's events, without the newline.
// It refuses an event that breaks the model's invariants.
func Encode(e Event) ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// Decode reads one line of a session's events. It refuses a field it does
// not know, trailing data and an event that breaks the model's invariants,
// so what Decode accepts Encode writes back byte for byte when the line was
// written by Encode.
func Decode(line []byte) (Event, error) {
	var e Event
	if err := strict(line, &e); err != nil {
		return Event{}, fmt.Errorf("event: decode: %w", err)
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Header is the first line of a session's events. A reader trusts the file
// only when Host and Session are the folder's.
type Header struct {
	V       int    `json:"v"`
	K       string `json:"k"`
	Host    string `json:"host"`
	Session string `json:"session"`
	Machine string `json:"machine,omitempty"`
	Created string `json:"created,omitempty"`
}

// HeaderVersion is the version of the session format this package writes.
const HeaderVersion = 2

// EncodeHeader writes a session's header line, without the newline.
func EncodeHeader(h Header) ([]byte, error) {
	h.V, h.K = HeaderVersion, "header"
	if h.Host == "" || h.Session == "" {
		return nil, errors.New("event: a header names its host and session")
	}
	return json.Marshal(h)
}

// DecodeHeader reads a session's header line.
func DecodeHeader(line []byte) (Header, error) {
	var h Header
	if err := strict(line, &h); err != nil {
		return Header{}, fmt.Errorf("event: decode header: %w", err)
	}
	if h.V != HeaderVersion || h.K != "header" || h.Host == "" || h.Session == "" {
		return Header{}, errors.New("event: not a header this reader knows")
	}
	return h, nil
}

func strict(line []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

// MarshalJSON writes a span as [start,end,kind,tier] or, with a handle,
// [start,end,kind,tier,handle].
func (s Span) MarshalJSON() ([]byte, error) {
	a := []any{s.Start, s.End, s.Kind, s.Tier}
	if s.Handle != "" {
		a = append(a, s.Handle)
	}
	return json.Marshal(a)
}

// UnmarshalJSON reads a span written by MarshalJSON.
func (s *Span) UnmarshalJSON(data []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if len(a) != 4 && len(a) != 5 {
		return errors.New("a span has four or five parts")
	}
	var out Span
	for i, dst := range []any{&out.Start, &out.End, &out.Kind, &out.Tier, &out.Handle}[:len(a)] {
		if err := strict(a[i], dst); err != nil {
			return err
		}
	}
	if len(a) == 5 && out.Handle == "" {
		return errors.New("an empty handle is left out")
	}
	*s = out
	return nil
}
