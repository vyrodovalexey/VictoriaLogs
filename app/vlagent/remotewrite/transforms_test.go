package remotewrite

import (
	"reflect"
	"testing"

	"github.com/VictoriaMetrics/VictoriaLogs/lib/logstorage"
)

func TestExecuteTransform(t *testing.T) {
	f := func(config string, row string, expected string) {
		t.Helper()

		cfg, err := unmarshalTransformsConfig([]byte(config))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		p := logstorage.GetJSONParser()
		defer logstorage.PutJSONParser(p)

		if err := p.ParseLogMessage([]byte(row), nil); err != nil {
			t.Fatalf("cannot parse row: %s", err)
		}

		for _, t := range cfg.transforms {
			executor := logstorage.NewQueryRowExecutor(t.pipe)
			p.Fields = executor.ApplyToRow(p.Fields)
		}

		got := logstorage.MarshalFieldsToJSON(nil, p.Fields)
		if string(got) != expected {
			t.Fatalf("unexpected result\ngot:\n%s\nwant:\n%s", got, expected)
		}
	}

	f(`transforms:
  - filter: '*'
    pipe: unpack_json from payload | delete payload`, `{"payload":"{\"foo\":\"bar\"}"}`, `{"foo":"bar"}`)
}

func TestUnmarshalTransforms(t *testing.T) {
	f := func(configStr string, expected []transform) {
		t.Helper()

		got, err := unmarshalTransformsConfig([]byte(configStr))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if !reflect.DeepEqual(got.transforms, expected) {
			t.Fatalf("unexpected transforms config\ngot:\n%+v\nwant:\n%+v", got.transforms, expected)
		}
	}

	s := `transforms:
  # Parse JSON payload embedded in incoming logs
  - filter: '*'
    pipe: unpack_json from payload | delete payload`
	expected := []transform{
		{
			filter: mustParseFilter(t, "*"),
			pipe:   mustParseQuery(t, "unpack_json from payload | delete payload"),
		},
	}
	f(s, expected)

	s = `transforms:
  - filter: '*'
    pipe: unpack_nginx from error_log | delete error_log`
	expected = []transform{
		{
			filter: mustParseFilter(t, "*"),
			pipe:   mustParseQuery(t, "unpack_nginx from error_log | delete error_log"),
		},
	}
	f(s, expected)

	s = `transforms:
  - filter: service:=checkout
    transforms:
      # Mark critical errors for alerts sink, then forward immediately
      - filter: "level:=i('ERROR')"
        pipe: "extract 'order_id=<id>' from _msg | format 'high' as priority | format 'alerts' as __target"
        action: send
      # Drop noisy retries
      - filter: "event:='retry_attempt'"
        action: drop`
	expected = []transform{
		{
			filter: mustParseFilter(t, "service:=checkout"),
			transforms: []transform{
				{
					filter: mustParseFilter(t, "level:i('ERROR')"),
					pipe:   mustParseQuery(t, "extract 'order_id=<id>' from _msg | format 'high' as priority | format 'alerts' as __target"),
					action: actionSend,
				},
				{
					filter: mustParseFilter(t, "event:='retry_attempt'"),
					action: actionDrop,
				},
			},
		},
	}
	f(s, expected)

	s = `transforms:
  - filter: "kubernetes.pod_namespace:=staging"
    action: drop`
	expected = []transform{
		{
			filter: mustParseFilter(t, "kubernetes.pod_namespace:=staging"),
			action: actionDrop,
		},
	}
	f(s, expected)
}

func TestUnmarshalTransformsFailure(t *testing.T) {
	f := func(configStr string) {
		t.Helper()

		_, err := unmarshalTransformsConfig([]byte(configStr))
		if err == nil {
			t.Fatalf("expecting non-empty error")
		}
	}

	// Empty file
	f(``)

	// Invalid YAML
	f(`foo: bar`)

	// Unknown fields
	f(`transforms:
  - filter: error
    action: send
    foo: bar`)
}

func mustParseFilter(t *testing.T, filterStr string) *logstorage.Filter {
	t.Helper()
	f, err := logstorage.ParseFilter(filterStr)
	if err != nil {
		t.Fatalf("cannot parse filter %q: %s", filterStr, err)
	}
	return f
}

func mustParseQuery(t *testing.T, queryStr string) *logstorage.Query {
	t.Helper()
	ql, err := logstorage.ParseQuery(queryStr)
	if err != nil {
		t.Fatalf("cannot parse query %q: %s", queryStr, err)
	}
	return ql
}
