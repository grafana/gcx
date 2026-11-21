package errortracking_test

import (
	"testing"
)

func TestErrorsListFields(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors/42/customFields"
	f.reply(route, 200, `{"customFields":[
		{"key":"browser","valueCount":2,"eventCount":9,"saturated":true,"values":[
			{"value":"Firefox","eventCount":6},{"value":"Chrome","eventCount":3}]}]}`)

	f.runCases(t, []cliCase{
		{name: "selected fields", args: []string{"errors", "fields", "list", "42", "--project", "314", "--json", "key,saturated"},
			calls: []string{route}, json: `{"items":[{"key":"browser","saturated":true}]}`},
		{name: "the table shows the top values", args: []string{"errors", "fields", "list", "42", "--project", "314", "-o", "table"},
			calls: []string{route}, has: []string{"Firefox, Chrome"}},
		{name: "no fields", setup: func(f *fakeET) { f.reply(route, 200, `{"customFields":[]}`) },
			args: []string{"errors", "fields", "list", "42", "--project", "314", "-o", "json"}, calls: []string{route}, json: `{"items":[]}`},
		{name: "a field without values lists none, not null", setup: func(f *fakeET) {
			f.reply(route, 200, `{"customFields":[{"key":"browser","valueCount":0,"eventCount":0,"saturated":false}]}`)
		}, args: []string{"errors", "fields", "list", "42", "--project", "314", "--jq", ".items[0].values", "-o", "json"},
			calls: []string{route}, json: `[]`},
		{name: "a bad error ID", args: []string{"errors", "fields", "list", "0", "--project", "314", "-o", "json"}, exit: 2, err: `<error> "0" must be a positive error ID`},
	})
}
