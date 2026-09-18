package k6

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

type authValidationTableCodec struct{}

func (authValidationTableCodec) Format() format.Format { return "table" }
func (authValidationTableCodec) Encode(w io.Writer, value any) error {
	v, ok := value.(*AuthValidation)
	if !ok {
		return fmt.Errorf("auth table: expected *AuthValidation, got %T", value)
	}
	t := style.NewTable("STACK ID", "DEFAULT PROJECT ID").Row(strconv.Itoa(v.StackID), strconv.Itoa(v.DefaultProjectID))
	return t.Render(w)
}
func (authValidationTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type labelKeyTableCodec struct{}

func (labelKeyTableCodec) Format() format.Format { return "table" }
func (labelKeyTableCodec) Encode(w io.Writer, value any) error {
	var rows []LabelKey
	switch v := value.(type) {
	case []LabelKey:
		rows = v
	case *LabelKey:
		rows = []LabelKey{*v}
	default:
		return fmt.Errorf("label-key table: got %T", value)
	}
	t := style.NewTable("ID", "KEY", "DESCRIPTION")
	for _, v := range rows {
		description := "-"
		if v.Description != nil {
			description = *v.Description
		}
		t.Row(strconv.Itoa(v.ID), v.Key, description)
	}
	return t.Render(w)
}
func (labelKeyTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type projectLabelTableCodec struct{}

func (projectLabelTableCodec) Format() format.Format { return "table" }
func (projectLabelTableCodec) Encode(w io.Writer, value any) error {
	rows, ok := value.([]ProjectLabel)
	if !ok {
		return fmt.Errorf("project-label table: got %T", value)
	}
	t := style.NewTable("KEY", "VALUE")
	for _, v := range rows {
		t.Row(v.Key, v.Value)
	}
	return t.Render(w)
}
func (projectLabelTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type projectLimitsTableCodec struct{}

func (projectLimitsTableCodec) Format() format.Format { return "table" }
func (projectLimitsTableCodec) Encode(w io.Writer, value any) error {
	var rows []ProjectLimits
	switch v := value.(type) {
	case *ProjectLimitsList:
		rows = v.Value
	case *ProjectLimits:
		rows = []ProjectLimits{*v}
	case []ProjectLimits:
		rows = v
	default:
		return fmt.Errorf("project-limits table: got %T", value)
	}
	show := func(v *int) string {
		if v == nil {
			return "-"
		}
		return strconv.Itoa(*v)
	}
	t := style.NewTable("PROJECT ID", "MONTHLY VUH", "MAX VUS", "MAX BROWSER VUS", "MAX DURATION (S)")
	for _, v := range rows {
		t.Row(strconv.Itoa(v.ProjectID), show(v.VUHMaxPerMonth), show(v.VUMaxPerTest), show(v.VUBrowserMaxPerTest), show(v.DurationMaxPerTestSecs))
	}
	return t.Render(w)
}
func (projectLimitsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type validateOptionsTableCodec struct{}

func (validateOptionsTableCodec) Format() format.Format { return "table" }
func (validateOptionsTableCodec) Encode(w io.Writer, value any) error {
	v, ok := value.(*ValidateOptionsResult)
	if !ok {
		return fmt.Errorf("options table: got %T", value)
	}
	t := style.NewTable("ESTIMATED VUH", "PROTOCOL VUH", "BROWSER VUH").Row(fmt.Sprint(v.VUHUsage), fmt.Sprint(v.Breakdown.ProtocolVUH), fmt.Sprint(v.Breakdown.BrowserVUH))
	return t.Render(w)
}
func (validateOptionsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type testRunTableCodec struct{}

func (testRunTableCodec) Format() format.Format { return "table" }
func (testRunTableCodec) Encode(w io.Writer, value any) error {
	var rows []TestRun
	switch v := value.(type) {
	case []TestRun:
		rows = v
	case *TestRun:
		rows = []TestRun{*v}
	default:
		return fmt.Errorf("test-run table: got %T", value)
	}
	t := style.NewTable("ID", "TEST ID", "STATUS", "RESULT", "CREATED", "ENDED")
	for _, v := range rows {
		result := "-"
		if v.Result != nil {
			result = *v.Result
		}
		ended := "-"
		if v.Ended != nil {
			ended = *v.Ended
		}
		t.Row(strconv.Itoa(v.ID), strconv.Itoa(v.TestID), v.Status, result, v.Created, ended)
	}
	return t.Render(w)
}
func (testRunTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type distributionTableCodec struct{}

func (distributionTableCodec) Format() format.Format { return "table" }
func (distributionTableCodec) Encode(w io.Writer, value any) error {
	v, ok := value.(*TestRunDistribution)
	if !ok {
		return fmt.Errorf("distribution table: got %T", value)
	}
	keys := make([]string, 0, len(v.Distribution))
	for key := range v.Distribution {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	t := style.NewTable("LOAD ZONE", "PERCENTAGE", "NODES")
	for _, key := range keys {
		d := v.Distribution[key]
		nodes := make([]string, len(d.Nodes))
		for i, node := range d.Nodes {
			nodes[i] = node.Size + "@" + node.PublicIP
		}
		t.Row(key, fmt.Sprint(d.Percentage), strings.Join(nodes, ","))
	}
	return t.Render(w)
}
func (distributionTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type scheduleSingleTableCodec struct{}

func (scheduleSingleTableCodec) Format() format.Format { return "table" }
func (scheduleSingleTableCodec) Encode(w io.Writer, value any) error {
	v, ok := value.(*CloudSchedule)
	if !ok {
		return fmt.Errorf("schedule table: expected *CloudSchedule, got %T", value)
	}
	nextRun := "-"
	if v.NextRun != nil {
		nextRun = *v.NextRun
	}
	t := style.NewTable("ID", "LOAD TEST ID", "STARTS", "DEACTIVATED", "NEXT RUN").Row(strconv.Itoa(v.ID), strconv.Itoa(v.LoadTestID), v.Starts, strconv.FormatBool(v.Deactivated), nextRun)
	return t.Render(w)
}
func (scheduleSingleTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}
