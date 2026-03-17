package remotewrite

import (
	"flag"
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/envtemplate"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/flagutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/fs/fscore"
	"gopkg.in/yaml.v2"

	"github.com/VictoriaMetrics/VictoriaLogs/lib/logstorage"
)

var (
	transformsConfigPathGlobal = flag.String("remoteWrite.transformsConfig", "", "Optional path to transforms config, which are applied "+
		"to all the logs before sending them to -remoteWrite.url. See also -remoteWrite.urlTransformsConfig. "+
		"The path can point either to local file or to http url. "+
		"See https://docs.victoriametrics.com/victorialogs/vlagent/#transforming")

	transformConfigPaths = flagutil.NewArrayString("remoteWrite.urlTransformsConfig", "Optional path to transforms config for the corresponding -remoteWrite.url. "+
		"See also -remoteWrite.transformsConfig. The path can point either to local file or to http url. "+
		"See https://docs.victoriametrics.com/victorialogs/vlagent/#transforming")
)

type transformConfigs struct {
	global transformConfig
	perURL []transformConfig
}

func loadTransformConfigs() (*transformConfigs, error) {
	configs := &transformConfigs{}

	if *transformsConfigPathGlobal != "" {
		cfg, err := loadTransformConfig(*transformsConfigPathGlobal)
		if err != nil {
			return nil, err
		}
		configs.global = *cfg
	}

	for _, configPath := range *transformConfigPaths {
		if configPath == "" {
			continue
		}
		cfg, err := loadTransformConfig(configPath)
		if err != nil {
			return nil, err
		}
		configs.perURL = append(configs.perURL, *cfg)
	}
	return configs, nil
}

type transformConfig struct {
	transforms []transform
}

type transform struct {
	filter     *logstorage.Filter `yaml:"filter"`
	pipe       *logstorage.Query  `yaml:"pipe"`
	action     action             `yaml:"action"`
	transforms []transform        `yaml:"transforms"`
}

func (t *transform) String() string {
	return fmt.Sprintf("filter=%q, pipe=%q, action=%d, transforms=%v", t.filter, t.pipe, t.action, t.transforms)
}

type action byte

const (
	actionContinue action = iota
	actionDrop
	actionSend
)

func loadTransformConfig(configPath string) (*transformConfig, error) {
	data, err := fscore.ReadFileOrHTTP(configPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read transforms config from %q: %w", configPath, err)
	}
	data = envtemplate.ReplaceBytes(data)

	cfg, err := unmarshalTransformsConfig(data)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal transforms config from %q: %w", configPath, err)
	}
	return cfg, nil
}

type rawTransformConfig struct {
	Filter     string               `yaml:"filter"`
	Pipe       string               `yaml:"pipe"`
	Action     string               `yaml:"action"`
	Transforms []rawTransformConfig `yaml:"transforms"`
}

func unmarshalTransformsConfig(data []byte) (*transformConfig, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	// Set strict mode to fail on unknown fields.
	dec.SetStrict(true)

	root := struct {
		Transforms []rawTransformConfig `yaml:"transforms"`
	}{}
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}

	cfg := &transformConfig{}
	if err := cfg.initFromRawConfig(root.Transforms); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (t *transformConfig) initFromRawConfig(raws []rawTransformConfig) error {
	t.transforms = make([]transform, 0, len(raws))
	for i, raw := range raws {
		var tr transform
		if err := tr.initFromRawConfig(raw); err != nil {
			return fmt.Errorf("cannot initialize transform with index %d: %w", i, err)
		}
		t.transforms = append(t.transforms, tr)
	}
	return nil
}

func (t *transform) initFromRawConfig(raw rawTransformConfig) error {
	if err := t.initFilter(raw.Filter); err != nil {
		return err
	}

	if err := t.unmarshalPipe(raw.Pipe); err != nil {
		return err
	}

	if err := t.initAction(raw.Action); err != nil {
		return err
	}

	if err := t.initNestedTransforms(raw.Transforms); err != nil {
		return err
	}

	return nil
}

func (t *transform) initFilter(s string) error {
	filter, err := logstorage.ParseFilter(s)
	if err != nil {
		return fmt.Errorf("cannot parse LogsQL filter: %w", err)
	}
	t.filter = filter
	return nil
}

func (t *transform) unmarshalPipe(s string) error {
	q, err := logstorage.ParsePipes(s)
	if err != nil {
		return err
	}
	if !q.CanLiveTail() {
		return fmt.Errorf("cannot use pipe %q in transformations because it accumulates state", q)
	}
	t.pipe = q
	return nil
}

func (t *transform) initAction(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "continue":
		t.action = actionContinue
	case "drop":
		t.action = actionDrop
	case "send":
		t.action = actionSend
	default:
		return fmt.Errorf("unknown action: %q", s)
	}
	return nil
}

func (t *transform) initNestedTransforms(cfgs []rawTransformConfig) error {
	t.transforms = make([]transform, 0, len(cfgs))
	for i, cfg := range cfgs {
		var nested transform
		if err := nested.initFromRawConfig(cfg); err != nil {
			return fmt.Errorf("cannot initialize transform with index %d and filter %q: %w", i, t.filter, err)
		}
		t.transforms = append(t.transforms, nested)
	}
	return nil
}
