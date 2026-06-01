package assistant

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
)

// ConversationTextCodec encodes a ConversationTranscript as plain text.
type ConversationTextCodec struct{}

func (c *ConversationTextCodec) Format() format.Format {
	return "text"
}

func (c *ConversationTextCodec) Encode(dst io.Writer, value any) error {
	transcript, ok := value.(ConversationTranscript)
	if !ok {
		return fmt.Errorf("expected ConversationTranscript, got %T", value)
	}
	_, err := io.WriteString(dst, transcript.FormatText())
	return err
}

func (c *ConversationTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("decode not supported for text format")
}
