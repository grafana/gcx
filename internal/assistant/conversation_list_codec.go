package assistant

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/grafana/gcx/internal/format"
)

// ConversationListCodec renders []Chat as a table.
type ConversationListCodec struct {
	Wide bool
}

func (c *ConversationListCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ConversationListCodec) Encode(dst io.Writer, value any) error {
	chats, ok := value.([]Chat)
	if !ok {
		return fmt.Errorf("expected []Chat, got %T", value)
	}

	tw := tabwriter.NewWriter(dst, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tTITLE\tSOURCE\tCATEGORY\tUPDATED")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tSOURCE\tUPDATED")
	}

	for _, chat := range chats {
		title := truncate(chat.Name, 50)
		if title == "" {
			title = "-"
		}
		source := chat.Source
		if source == "" {
			source = "-"
		}
		updated := formatChatTime(chat.UpdatedAt)

		if c.Wide {
			category := chat.Category
			if category == "" {
				category = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", chat.ID, title, source, category, updated)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", chat.ID, title, source, updated)
		}
	}
	return tw.Flush()
}

func (c *ConversationListCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("decode not supported for table format")
}

// formatChatTime renders an RFC3339 timestamp string for table display.
func formatChatTime(value string) string {
	if value == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return t.UTC().Format("2006-01-02 15:04")
}
