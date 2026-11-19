package errortracking

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const commentNote = "A comment is plain text of at most 10,000 characters, and only its author can edit or delete it."

type commentOpts struct {
	errorTarget

	Body    string
	Force   bool
	comment int64
}

func (o *commentOpts) setup(flags *pflag.FlagSet, verb string) {
	o.Project.setup(flags)
	if verb == "delete" {
		flags.BoolVar(&o.Force, "force", false, "Delete without asking")
	} else {
		flags.StringVar(&o.Body, "body", "", "The comment's text (required)")
		_ = cobra.MarkFlagRequired(flags, "body")
	}
	bindText(&o.IO, flags, mutationText{})
}

func (o *commentOpts) Validate(cmd *cobra.Command, args []string) error {
	if len(args) == 2 {
		id, _, ok := parseID(strings.TrimSpace(args[1]))
		if !ok {
			see := argHelp(cmd, "<comment>", args)
			return usage(cmd, fmt.Sprintf("<comment> %q must be a positive comment ID", args[1]), "See them: "+see)
		}
		o.comment = id
	}
	if cmd.Flags().Lookup("body") != nil {
		body := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(o.Body, "\r\n", "\n"), "\r", "\n"))
		if body == "" || utf8.RuneCountInString(body) > 10000 {
			return usage(cmd, "--body must be 1 to 10,000 characters")
		}
		o.Body = body
	}
	return o.errorTarget.Validate(cmd, args[0])
}

func newCommentCommand(loader *providers.ConfigLoader, verb string) *cobra.Command {
	opts := &commentOpts{}
	cmd := &cobra.Command{
		Use:   verb + " <error> <comment>",
		Short: map[string]string{"update": "Edit a comment you wrote on an error.", "delete": "Delete a comment you wrote on an error."}[verb],
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			force := true
			if verb == "delete" {
				var err error
				if force, err = deleteBypass(cmd, opts.Force); err != nil {
					return err
				}
			}
			ctx := cmd.Context()
			c, path, err := opts.connect(ctx, loader, nil)
			if err != nil {
				return err
			}
			path += "/comments"
			var entry activityEntry
			switch verb {
			case "create":
				err = c.do(ctx, http.MethodPost, path, nil, map[string]string{"body": opts.Body}, &entry)
			case "update":
				err = c.do(ctx, http.MethodPatch, path+"/"+strconv.FormatInt(opts.comment, 10), nil, map[string]string{"body": opts.Body}, &entry)
			case "delete":
				if !force {
					if err := confirm(cmd, fmt.Sprintf("Delete comment %d on Error Tracking %s?", opts.comment, opts.shown(c)),
						"delete cancelled", fmt.Sprintf("The prompt was declined; comment %d was not deleted.", opts.comment)); err != nil {
						return err
					}
				}
				err = c.do(ctx, http.MethodDelete, path+"/"+strconv.FormatInt(opts.comment, 10), nil, nil, nil)
			}
			if err != nil {
				return err
			}
			id := opts.comment
			if entry.Comment != nil {
				id = entry.Comment.ID
			}
			m := commentMutation{mutationBase: c.mutation(verb+"d", "Comment", id, true), ErrorID: strconv.FormatInt(opts.id, 10)}
			done := map[string]string{"create": "Created", "update": "Edited", "delete": "Deleted"}[verb]
			m.text = fmt.Sprintf("%s comment %d on %s", done, id, opts.shown(c))
			if id == 0 {
				m.text = fmt.Sprintf("%s a comment on %s; the response did not include its ID", done, opts.shown(c))
			}
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: map[string]string{
			"create": "<error> --project 314 --body <text> -o json",
			"update": "<error> <comment> --project 314 --body <text> -o json",
			"delete": "<error> <comment> --project 314 --force -o json",
		}[verb]},
	}
	switch verb {
	case "create":
		cmd.Use, cmd.Short, cmd.Args = "create <error>", "Comment on an error.", cobra.ExactArgs(1)
		cmd.Long = `Add a comment to one error (its ID from errors list) as you; it shows in the
error's activity (errors activity list), and the result names its ID when the server returns it. ` + commentNote + `
` + noDryRun
		cmd.Example = `  # Comment on error 42.
  gcx error-tracking errors comments create 42 --project 314 --body "Caused by the pricing rollout."

  # As an agent: comment and keep the new comment's ID.
  gcx error-tracking errors comments create 42 --project 314 --body "Fix is in review." -o json --jq .target.id

  # Read the comments back.
  gcx et errors activity list 42 --project 314 --type comment`
	case "update":
		cmd.Long = `Replace the text of one comment you wrote on an error; <comment> is its ID
from errors activity list. It keeps its place in the activity and shows when it
was edited. ` + commentNote + ` ` + noDryRun
		cmd.Example = `  # Edit comment 7 on error 42.
  gcx error-tracking errors comments update 42 7 --project 314 --body "Fixed in v1.2.4."

  # As an agent: edit it and read the result.
  gcx error-tracking errors comments update 42 7 --project 314 --body "Reverted." -o json

  # Find the comment's ID.
  gcx et errors activity list 42 --project 314 --type comment`
	case "delete":
		cmd.Long = `Delete one comment you wrote on an error; <comment> is its ID from errors
activity list. Its text is not kept, so it cannot be undone. gcx asks first;
--force skips the question, agent mode needs --force, and declining exits 5.
` + noDryRun
		cmd.Example = `  # Delete comment 7 on error 42, after a prompt.
  gcx error-tracking errors comments delete 42 7 --project 314

  # As an agent: delete it without a prompt.
  gcx error-tracking errors comments delete 42 7 --project 314 --force -o json

  # Find the comment's ID.
  gcx et errors activity list 42 --project 314 --type comment`
	}
	opts.setup(cmd.Flags(), verb)
	return cmd
}
