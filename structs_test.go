/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"crypto/tls"
	"errors"
	"log"
	"maps"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/tranquildata/config/mocks"
)

type scalarConfig struct {
	Name     string  `config:"name"`
	Enabled  bool    `config:"enabled"`
	Count    int     `config:"count"`
	Small    int8    `config:"small"`
	Big      int64   `config:"big"`
	Size     uint    `config:"size"`
	Tiny     uint16  `config:"tiny"`
	Ratio    float64 `config:"ratio"`
	Fraction float32 `config:"fraction"`
}

type defaultsConfig struct {
	Name    string `config:"name,fallback"`
	Port    int    `config:"port,8080"`
	Enabled bool   `config:" enabled,true "`
	Empty   string `config:"empty,"`
}

type untaggedConfig struct {
	Name      string `config:"name"`
	Untagged  string
	Blank     string `config:"  "`
	OtherTags string `json:"other"`
}

type sliceConfig struct {
	Names  []string `config:"names"`
	Counts []int    `config:"counts"`
}

type timeConfig struct {
	Start   time.Time     `config:"start"`
	Timeout time.Duration `config:"timeout"`
}

type innerConfig struct {
	Host string `config:"host"`
}

type nestedConfig struct {
	Name    string       `config:"name"`
	Inner   innerConfig  `config:""`
	Pointer *innerConfig `config:""`
}

type hiddenConfig struct {
	User     string `config:"user"`
	Password string `hiddenconfig:"password"`
	Token    string `hiddenconfig:"token,none"`
	Missing  string `hiddenconfig:"missing"`
}

type nestedHiddenConfig struct {
	Name        string        `config:"name"`
	Credentials hiddenConfig  `config:""`
	Pointer     *hiddenConfig `config:""`
}

type processedConfig struct {
	Name      string `config:"name"`
	processor Postprocessor
}

func (pc *processedConfig) Process() error {
	return pc.processor.Process()
}

func acceptingScheme(ctrl *gomock.Controller) *mocks.MockNamingScheme {
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate(gomock.Any()).Return(nil).AnyTimes()
	return scheme
}

func mapProvider(ctrl *gomock.Controller, properties map[string]string) *mocks.MockProvider {
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value(gomock.Any()).DoAndReturn(func(propertyName string) (string, bool) {
		value, present := properties[propertyName]
		return value, present
	}).AnyTimes()
	return provider
}

func Test_populateNotPointer(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)

	if resolved, hidden, err := PopulateStruct(scalarConfig{}, provider, scheme); err == nil {
		t.Error("expected an error for a non-pointer input")
	} else if resolved != nil || hidden != nil {
		t.Error("expected no resolved properties for a non-pointer input")
	}
}

func Test_populateNotStruct(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)
	value := "not a struct"

	if resolved, hidden, err := PopulateStruct(&value, provider, scheme); err == nil {
		t.Error("expected an error for a pointer to a non-struct")
	} else if resolved != nil || hidden != nil {
		t.Error("expected no resolved properties for a pointer to a non-struct")
	}
}

func Test_populateEmptyStruct(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)

	if resolved, _, err := PopulateStruct(&struct{}{}, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if len(resolved) != 0 {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populateScalars(t *testing.T) {
	ctrl := gomock.NewController(t)
	properties := map[string]string{
		"name":     "app",
		"enabled":  "true",
		"count":    "-42",
		"small":    "-8",
		"big":      "9223372036854775807",
		"size":     "42",
		"tiny":     "65535",
		"ratio":    "0.25",
		"fraction": "1.5",
	}
	provider := mapProvider(ctrl, properties)
	scheme := acceptingScheme(ctrl)

	config := &scalarConfig{}
	resolved, _, err := PopulateStruct(config, provider, scheme)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := scalarConfig{Name: "app", Enabled: true, Count: -42, Small: -8, Big: 9223372036854775807,
		Size: 42, Tiny: 65535, Ratio: 0.25, Fraction: 1.5}
	if *config != expected {
		t.Errorf("unexpected populated struct: %+v", *config)
	}
	if len(resolved) != len(properties) {
		t.Errorf("unexpected number of resolved properties: %d", len(resolved))
	}
	for name, value := range properties {
		if resolved[name] != value {
			t.Errorf("unexpected resolved value for %s: %q", name, resolved[name])
		}
	}
}

func Test_populateValidatesNames(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("localhost", true)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil)

	config := &innerConfig{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Host != "localhost" {
		t.Errorf("unexpected host: %q", config.Host)
	}
}

func Test_populateInvalidName(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(errors.New("invalid"))

	if resolved, hidden, err := PopulateStruct(&innerConfig{}, provider, scheme); err == nil {
		t.Error("expected an error for an invalid property name")
	} else if resolved != nil || hidden != nil {
		t.Error("expected no resolved properties for an invalid property name")
	}
}

func Test_populateTooManyTagValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)
	config := &struct {
		Name string `config:"name,default,extra"`
	}{}

	if resolved, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for too many tag values")
	} else if resolved != nil || hidden != nil {
		t.Error("expected no resolved properties for too many tag values")
	}
}

func Test_populateUntagged(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("name").Return("app", true)
	scheme := acceptingScheme(ctrl)

	config := &untaggedConfig{Untagged: "keep", Blank: "keep", OtherTags: "keep"}
	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Name != "app" {
		t.Errorf("unexpected name: %q", config.Name)
	} else if config.Untagged != "keep" || config.Blank != "keep" || config.OtherTags != "keep" {
		t.Error("untagged fields were modified")
	} else if len(resolved) != 1 || resolved["name"] != "app" {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populateDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{})
	scheme := acceptingScheme(ctrl)

	config := &defaultsConfig{}
	resolved, _, err := PopulateStruct(config, provider, scheme)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := defaultsConfig{Name: "fallback", Port: 8080, Enabled: true, Empty: ""}
	if *config != expected {
		t.Errorf("unexpected populated struct: %+v", *config)
	}
	expectedResolved := map[string]string{"name": "fallback", "port": "8080", "enabled": "true", "empty": ""}
	if len(resolved) != len(expectedResolved) {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
	for name, value := range expectedResolved {
		if resolvedValue, present := resolved[name]; !present || resolvedValue != value {
			t.Errorf("unexpected resolved value for %s: %q (%t)", name, resolvedValue, present)
		}
	}
}

func Test_populateProvidedOverridesDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"name": "provided", "port": "9090", "enabled": "false"})
	scheme := acceptingScheme(ctrl)

	config := &defaultsConfig{}
	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Name != "provided" || config.Port != 9090 || config.Enabled {
		t.Errorf("unexpected populated struct: %+v", *config)
	} else if resolved["name"] != "provided" || resolved["port"] != "9090" || resolved["enabled"] != "false" {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populateMissingWithoutDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("", false)
	scheme := acceptingScheme(ctrl)

	config := &innerConfig{}
	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Host != "" {
		t.Errorf("expected zero value for missing property: %q", config.Host)
	} else if len(resolved) != 0 {
		t.Errorf("expected no resolved properties for missing property: %v", resolved)
	}
}

func Test_populateInvalidDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := acceptingScheme(ctrl)
	config := &struct {
		Port int `config:"port,eighty"`
	}{}

	if resolved, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid default value")
	} else if resolved != nil || hidden != nil {
		t.Error("expected no resolved properties for an invalid default value")
	}
}

func Test_populateInvalidProvided(t *testing.T) {
	scheme := func(ctrl *gomock.Controller) NamingScheme { return acceptingScheme(ctrl) }
	cases := map[string]any{
		"bool": &struct {
			V bool `config:"v"`
		}{},
		"int": &struct {
			V int `config:"v"`
		}{},
		"uint": &struct {
			V uint `config:"v"`
		}{},
		"float": &struct {
			V float64 `config:"v"`
		}{},
		"time": &struct {
			V time.Time `config:"v"`
		}{},
		"duration": &struct {
			V time.Duration `config:"v"`
		}{},
	}
	for name, config := range cases {
		ctrl := gomock.NewController(t)
		provider := mocks.NewMockProvider(ctrl)
		provider.EXPECT().Value("v").Return("not-a-value", true)

		if _, _, err := PopulateStruct(config, provider, scheme(ctrl)); err == nil {
			t.Errorf("expected an error for an invalid %s value", name)
		}
	}
}

func Test_populateNegativeUnsigned(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("v").Return("-1", true)
	scheme := acceptingScheme(ctrl)

	if _, _, err := PopulateStruct(&struct {
		V uint `config:"v"`
	}{}, provider, scheme); err == nil {
		t.Error("expected an error for a negative unsigned value")
	}
}

func Test_populateOutOfRange(t *testing.T) {
	cases := map[string]any{
		"int8": &struct {
			V int8 `config:"v"`
		}{},
		"uint8": &struct {
			V uint8 `config:"v"`
		}{},
		"int16": &struct {
			V int16 `config:"v"`
		}{},
		"float32": &struct {
			V float32 `config:"v"`
		}{},
	}
	values := map[string]string{"int8": "300", "uint8": "256", "int16": "70000", "float32": "1e40"}
	for name, config := range cases {
		ctrl := gomock.NewController(t)
		provider := mocks.NewMockProvider(ctrl)
		provider.EXPECT().Value("v").Return(values[name], true)

		if _, _, err := PopulateStruct(config, provider, acceptingScheme(ctrl)); err == nil {
			t.Errorf("expected an error for an out-of-range %s value", name)
		}
	}
}

func Test_populateUnsupportedType(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("v").Return("a=b", true).AnyTimes()
	scheme := acceptingScheme(ctrl)

	if _, _, err := PopulateStruct(&struct {
		V map[string]string `config:"v"`
	}{}, provider, scheme); err == nil {
		t.Error("expected an error for an unsupported field type")
	}
}

func Test_populateSliceOfStructs(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("servers").Return("2026-09-28T12:30:00Z", true)
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Servers []innerConfig `config:"servers"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for a slice of structs")
	} else if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("unexpected error for a slice of structs: %v", err)
	}
}

func Test_populateSlices(t *testing.T) {
	ctrl := gomock.NewController(t)
	names := strings.Join([]string{"a", "b", "c"}, SliceValueSeparator)
	counts := strings.Join([]string{"1", "2", "3"}, SliceValueSeparator)
	provider := mapProvider(ctrl, map[string]string{"names": names, "counts": counts})
	scheme := acceptingScheme(ctrl)

	config := &sliceConfig{}
	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !slices.Equal(config.Names, []string{"a", "b", "c"}) {
		t.Errorf("unexpected names: %v", config.Names)
	} else if !slices.Equal(config.Counts, []int{1, 2, 3}) {
		t.Errorf("unexpected counts: %v", config.Counts)
	} else if resolved["names"] != names || resolved["counts"] != counts {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populateSliceWhitespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	names := strings.Join([]string{" a", "b ", "  c  "}, SliceValueSeparator)
	counts := strings.Join([]string{"1", " 2", "\t3 "}, SliceValueSeparator)
	provider := mapProvider(ctrl, map[string]string{"names": names, "counts": counts})
	scheme := acceptingScheme(ctrl)

	config := &sliceConfig{}
	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !slices.Equal(config.Names, []string{"a", "b", "c"}) {
		t.Errorf("unexpected names: %q", config.Names)
	} else if !slices.Equal(config.Counts, []int{1, 2, 3}) {
		t.Errorf("unexpected counts: %v", config.Counts)
	} else if resolved["names"] != names || resolved["counts"] != counts {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populateEmptySlice(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"names": "", "counts": ""})
	scheme := acceptingScheme(ctrl)

	config := &sliceConfig{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Names == nil || len(config.Names) != 0 {
		t.Errorf("expected an empty names slice: %v", config.Names)
	} else if config.Counts == nil || len(config.Counts) != 0 {
		t.Errorf("expected an empty counts slice: %v", config.Counts)
	}
}

func Test_populateSliceDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("names").Return("", false)
	scheme := acceptingScheme(ctrl)

	// the slice separator must differ from the tag separator so a default can hold several elements
	config := &struct {
		Names []string `config:"names,a;b"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !slices.Equal(config.Names, []string{"a", "b"}) {
		t.Errorf("unexpected names: %v", config.Names)
	}
}

func Test_populateInvalidSliceElement(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"names": "a", "counts": strings.Join([]string{"1", "two", "3"}, SliceValueSeparator)})
	scheme := acceptingScheme(ctrl)

	if _, _, err := PopulateStruct(&sliceConfig{}, provider, scheme); err == nil {
		t.Error("expected an error for an invalid slice element")
	}
}

func Test_populateTimes(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"start": "2026-09-28T12:30:00Z", "timeout": "1m30s"})
	scheme := acceptingScheme(ctrl)

	config := &timeConfig{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !config.Start.Equal(time.Date(2026, 9, 28, 12, 30, 0, 0, time.UTC)) {
		t.Errorf("unexpected start: %v", config.Start)
	} else if config.Timeout != 90*time.Second {
		t.Errorf("unexpected timeout: %v", config.Timeout)
	}
}

func Test_populateNested(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"name": "app", "host": "localhost"})
	scheme := acceptingScheme(ctrl)

	config := &nestedConfig{}
	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Name != "app" {
		t.Errorf("unexpected name: %q", config.Name)
	} else if config.Inner.Host != "localhost" {
		t.Errorf("unexpected inner host: %q", config.Inner.Host)
	} else if config.Pointer == nil {
		t.Error("expected pointer to struct to be allocated")
	} else if config.Pointer.Host != "localhost" {
		t.Errorf("unexpected pointer host: %q", config.Pointer.Host)
	} else if len(resolved) != 2 || resolved["name"] != "app" || resolved["host"] != "localhost" {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populateUntaggedStructSkipped(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)

	// untagged structs whose types expect no config handling are skipped without being
	// inspected or allocated, as are untagged pointers of any kind, and self-referencing
	// types are checked without recursing forever
	type link struct {
		Next *link
	}
	config := &struct {
		TLS     tls.Config
		Pointer *tls.Config
		Link    link
		String  *string
		Nested  **innerConfig
		Skipped struct {
			Inner innerConfig `config:"-"`
		}
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.TLS.RootCAs != nil || config.Pointer != nil || config.Link.Next != nil || config.String != nil || config.Nested != nil {
		t.Error("untagged fields were modified")
	} else if len(visible) != 0 || len(hidden) != 0 {
		t.Errorf("unexpected properties: %v, %v", visible, hidden)
	}
}

func Test_populateUntaggedStructExpectsConfig(t *testing.T) {
	type Embedded struct {
		Host string `config:"host"`
	}
	type node struct {
		Name string `config:"name"`
		Next *node
	}
	cases := map[string]any{
		"struct": &struct {
			Inner innerConfig
		}{},
		"pointer": &struct {
			Inner *innerConfig
		}{},
		"hidden": &struct {
			Credentials hiddenConfig
		}{},
		"embedded": &struct {
			Embedded
		}{},
		"deep": &struct {
			Outer struct {
				Inner innerConfig
			}
		}{},
		"opted-in": &struct {
			Outer struct {
				Inner struct{} `config:""`
			}
		}{},
		"unexported": &struct {
			Outer struct {
				port int `config:"port"`
			}
		}{},
		"self-referencing": &node{},
	}
	for name, config := range cases {
		ctrl := gomock.NewController(t)
		provider := mapProvider(ctrl, map[string]string{})
		scheme := acceptingScheme(ctrl)

		if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
			t.Errorf("expected an error for an untagged %s struct", name)
		} else if !strings.Contains(err.Error(), "is not tagged") {
			t.Errorf("unexpected error for an untagged %s struct: %v", name, err)
		} else if visible != nil || hidden != nil {
			t.Errorf("expected no properties for an untagged %s struct", name)
		}
	}
}

func Test_populateSkippedStruct(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("name").Return("app", true)
	scheme := acceptingScheme(ctrl)

	// skipped structs aren't populated even though their types expect config handling, and
	// the skip directive is accepted on unexported fields since it can't be a mistake there
	config := &struct {
		Name     string        `config:"name"`
		Inner    innerConfig   `config:"-"`
		Pointer  *innerConfig  `config:" - "`
		Hidden   hiddenConfig  `config:"-"`
		inner    innerConfig   `config:"-"`
		password string        `hiddenconfig:"-"`
		Wrapped  *nestedConfig `config:"-"`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Name != "app" {
		t.Errorf("unexpected name: %q", config.Name)
	} else if config.Inner.Host != "" || config.Pointer != nil || config.Hidden.User != "" || config.Wrapped != nil {
		t.Errorf("skipped fields were modified: %+v", *config)
	} else if len(visible) != 1 || len(hidden) != 0 {
		t.Errorf("unexpected properties: %v, %v", visible, hidden)
	}
}

func Test_populateInvalidStructTag(t *testing.T) {
	cases := map[string]any{
		"default": &struct {
			Inner innerConfig `config:",value"`
		}{},
		"prefixed-default": &struct {
			Inner innerConfig `config:"inner,value"`
		}{},
		"empty-default": &struct {
			Inner innerConfig `config:"inner,"`
		}{},
		"pointer-default": &struct {
			Inner *innerConfig `config:"inner,value"`
		}{},
		"skip-default": &struct {
			Inner innerConfig `config:"-,value"`
		}{},
		"hidden": &struct {
			Inner innerConfig `hiddenconfig:""`
		}{},
		"hidden-name": &struct {
			Inner innerConfig `hiddenconfig:"inner"`
		}{},
		"hidden-skip": &struct {
			Inner innerConfig `config:"-" hiddenconfig:"-"`
		}{},
	}
	for name, config := range cases {
		ctrl := gomock.NewController(t)
		provider := mocks.NewMockProvider(ctrl)
		scheme := mocks.NewMockNamingScheme(ctrl)

		if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
			t.Errorf("expected an error for a struct tagged with %s", name)
		} else if strings.Contains(name, "default") && !strings.Contains(err.Error(), "cannot have a default value") {
			t.Errorf("unexpected error for a struct tagged with %s: %v", name, err)
		} else if visible != nil || hidden != nil {
			t.Errorf("expected no properties for a struct tagged with %s", name)
		}
	}
}

type postgresConfig struct {
	Port     int    `config:"postgresPort,5432"`
	Host     string `config:"postgresHost"`
	Password string `hiddenconfig:"postgresPassword"`
}

func Test_populatePrefixedStructs(t *testing.T) {
	provider := MapProvider(map[string]string{
		"contextPostgresHost":     "context.example.com",
		"indexPostgresHost":       "index.example.com",
		"indexPostgresPort":       "6543",
		"indexPostgresPassword":   "secret",
		"postgresHost":            "unprefixed.example.com",
		"contextPostgresPassword": "other",
	})

	// the same struct type is used under different prefixes, by value and by pointer, and
	// with an empty tag for no prefix, so each property name is distinct
	config := &struct {
		Context    postgresConfig  `config:"context"`
		Index      *postgresConfig `config:"index"`
		Unprefixed postgresConfig  `config:""`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Context.Host != "context.example.com" || config.Context.Port != 5432 || config.Context.Password != "other" {
		t.Errorf("unexpected context config: %+v", config.Context)
	} else if config.Index == nil || config.Index.Host != "index.example.com" || config.Index.Port != 6543 || config.Index.Password != "secret" {
		t.Errorf("unexpected index config: %+v", config.Index)
	} else if config.Unprefixed.Host != "unprefixed.example.com" || config.Unprefixed.Port != 5432 {
		t.Errorf("unexpected unprefixed config: %+v", config.Unprefixed)
	} else if !maps.Equal(visible, map[string]string{
		"contextPostgresHost": "context.example.com", "contextPostgresPort": "5432",
		"indexPostgresHost": "index.example.com", "indexPostgresPort": "6543",
		"postgresHost": "unprefixed.example.com", "postgresPort": "5432",
	}) {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if !maps.Equal(hidden, map[string]string{"contextPostgresPassword": "other", "indexPostgresPassword": "secret"}) {
		t.Errorf("unexpected hidden properties: %v", hidden)
	}
}

func Test_populateNestedPrefixes(t *testing.T) {
	provider := MapProvider(map[string]string{"contextStorePrimaryPostgresPort": "1", "contextCacheSize": "2", "contextName": "app"})

	// prefixes accumulate through nested struct fields, and an empty tag adds nothing to the
	// prefix that is already in place
	type cacheConfig struct {
		Size int `config:"size"`
	}
	type storeConfig struct {
		Primary postgresConfig `config:"primary"`
	}
	config := &struct {
		Context struct {
			Name  string      `config:"name"`
			Store storeConfig `config:"store"`
			Cache struct {
				Inner cacheConfig `config:"cache"`
			} `config:""`
		} `config:"context"`
	}{}
	if visible, _, err := PopulateStruct(config, provider, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Context.Name != "app" || config.Context.Store.Primary.Port != 1 || config.Context.Cache.Inner.Size != 2 {
		t.Errorf("unexpected populated struct: %+v", *config)
	} else if !maps.Equal(visible, map[string]string{"contextName": "app", "contextStorePrimaryPostgresPort": "1", "contextCacheSize": "2"}) {
		t.Errorf("unexpected visible properties: %v", visible)
	}
}

func Test_populatePrefixSchemes(t *testing.T) {
	cases := map[string]struct {
		scheme   NamingScheme
		prefix   string
		name     string
		expected string
	}{
		"camel":         {CamelNamingScheme(), "index", "postgresPort", "indexPostgresPort"},
		"camel-acronym": {CamelNamingScheme(), "maxHTTP", "port", "maxHTTPPort"},
		"snake":         {SnakeNamingScheme(), "index", "postgres_port", "index_postgres_port"},
		"kebab":         {KebabNamingScheme(), "index", "postgres-port", "index-postgres-port"},
		"custom":        {dottedScheme{}, "index", "postgres.port", "index.postgres.port"},
	}
	for name, testCase := range cases {
		// the struct tags are built at runtime so that each scheme gets names that are valid
		inner := reflect.StructOf([]reflect.StructField{{
			Name: "Port", Type: reflect.TypeFor[int](), Tag: reflect.StructTag(`config:"` + testCase.name + `"`),
		}})
		outer := reflect.StructOf([]reflect.StructField{{
			Name: "Inner", Type: inner, Tag: reflect.StructTag(`config:"` + testCase.prefix + `"`),
		}})
		config := reflect.New(outer)

		provider := MapProvider(map[string]string{testCase.expected: "42"})
		if visible, _, err := PopulateStruct(config.Interface(), provider, testCase.scheme); err != nil {
			t.Errorf("unexpected error for %s: %v", name, err)
		} else if port := config.Elem().Field(0).Field(0).Int(); port != 42 {
			t.Errorf("unexpected port for %s: %d", name, port)
		} else if !maps.Equal(visible, map[string]string{testCase.expected: "42"}) {
			t.Errorf("unexpected properties for %s: %v", name, visible)
		}
	}
}

func Test_populateInvalidPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)

	// the prefix must be a valid name on its own
	if _, _, err := PopulateStruct(&struct {
		Inner innerConfig `config:"Index"`
	}{}, provider, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an invalid prefix")
	}
	if _, _, err := PopulateStruct(&struct {
		Inner innerConfig `config:"index_store"`
	}{}, provider, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a prefix in the wrong scheme")
	}
}

func Test_populateInvalidComposedName(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)

	// the composed name is validated too, since a custom scheme may compose valid names into
	// one that isn't valid
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("index").Return(nil)
	scheme.EXPECT().Validate("host").Return(nil)
	scheme.EXPECT().Components("index").Return([]string{"index"})
	scheme.EXPECT().Components("host").Return([]string{"host"})
	scheme.EXPECT().Compose([]string{"index", "host"}).Return("index!host")
	scheme.EXPECT().Validate("index!host").Return(errors.New("invalid"))

	if visible, hidden, err := PopulateStruct(&struct {
		Inner innerConfig `config:"index"`
	}{}, provider, scheme); err == nil {
		t.Error("expected an error for an invalid composed name")
	} else if visible != nil || hidden != nil {
		t.Error("expected no properties for an invalid composed name")
	}
}

func Test_populatePrefixedInvalidValue(t *testing.T) {
	provider := MapProvider(map[string]string{"indexPostgresPort": "eighty", "contextPostgresPassword": "s3cr3t-value"})

	// a struct used in several places has the same field names in each, so errors report
	// the property name to show which use of the struct failed
	if _, _, err := PopulateStruct(&struct {
		Index postgresConfig `config:"index"`
	}{}, provider, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an invalid value")
	} else if !strings.Contains(err.Error(), "indexPostgresPort") {
		t.Errorf("expected the property name in the error: %v", err)
	}

	type intConfig struct {
		Password int `hiddenconfig:"postgresPassword"`
	}
	if _, _, err := PopulateStruct(&struct {
		Context intConfig `config:"context"`
	}{}, provider, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an invalid hidden value")
	} else if !strings.Contains(err.Error(), "contextPostgresPassword") || strings.Contains(err.Error(), "s3cr3t-value") {
		t.Errorf("unexpected error for an invalid hidden value: %v", err)
	}
}

func Test_populatePointerToBaseType(t *testing.T) {
	cases := map[string]any{
		"string": &struct {
			V *string `config:"v"`
		}{},
		"hidden": &struct {
			V *string `hiddenconfig:"v"`
		}{},
		"int": &struct {
			V *int `config:"v"`
		}{},
		"slice": &struct {
			V *[]string `config:"v"`
		}{},
		"time": &struct {
			V *time.Time `config:"v"`
		}{},
		"duration": &struct {
			V *time.Duration `config:"v"`
		}{},
		"url": &struct {
			V *url.URL `config:"v"`
		}{},
		"pointer": &struct {
			V **innerConfig `config:"v"`
		}{},
	}
	for name, config := range cases {
		ctrl := gomock.NewController(t)
		provider := mocks.NewMockProvider(ctrl)
		scheme := mocks.NewMockNamingScheme(ctrl)

		if resolved, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
			t.Errorf("expected an error for a pointer to %s", name)
		} else if !strings.Contains(err.Error(), "pointers to base types are not supported") {
			t.Errorf("unexpected error for a pointer to %s: %v", name, err)
		} else if resolved != nil || hidden != nil {
			t.Errorf("expected no resolved properties for a pointer to %s", name)
		}
	}
}

func Test_populateNestedError(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"name": "app"})
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("name").Return(nil).AnyTimes()
	scheme.EXPECT().Validate("host").Return(errors.New("invalid"))

	if resolved, hidden, err := PopulateStruct(&nestedConfig{}, provider, scheme); err == nil {
		t.Error("expected an error from a nested struct")
	} else if resolved != nil || hidden != nil {
		t.Error("expected no resolved properties for a nested error")
	}
}

func Test_populatePostprocessor(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("name").Return("app", true)
	scheme := acceptingScheme(ctrl)
	processor := mocks.NewMockPostprocessor(ctrl)

	config := &processedConfig{processor: processor}
	processor.EXPECT().Process().DoAndReturn(func() error {
		if config.Name != "app" {
			t.Errorf("postprocessor called before struct was populated: %q", config.Name)
		}
		return nil
	})

	if resolved, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if resolved["name"] != "app" {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_populatePostprocessorError(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("name").Return("app", true)
	scheme := acceptingScheme(ctrl)
	processor := mocks.NewMockPostprocessor(ctrl)
	processor.EXPECT().Process().Return(errors.New("failed"))

	if _, _, err := PopulateStruct(&processedConfig{processor: processor}, provider, scheme); err == nil {
		t.Error("expected an error from the postprocessor")
	}
}

func Test_populateNestedPostprocessor(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"name": "app"})
	scheme := acceptingScheme(ctrl)
	processor := mocks.NewMockPostprocessor(ctrl)
	processor.EXPECT().Process().Return(nil)

	config := &struct {
		Processed processedConfig `config:""`
	}{Processed: processedConfig{processor: processor}}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Processed.Name != "app" {
		t.Errorf("unexpected nested name: %q", config.Processed.Name)
	}
}

func Test_populateUnexportedFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("localhost", true).Times(2)
	scheme := acceptingScheme(ctrl)

	// untagged unexported fields are never inspected, whatever their type, so the provider
	// is only asked for the exported fields
	type private struct {
		Host string `config:"host"`
	}
	config := &struct {
		Host    string `config:"host"`
		mu      sync.Mutex
		logger  *log.Logger
		inner   innerConfig
		pointer *innerConfig
		private
		Exported innerConfig `config:""`
	}{}
	config.mu.Lock()
	defer config.mu.Unlock()
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Host != "localhost" || config.Exported.Host != "localhost" {
		t.Errorf("unexpected exported values: %q, %q", config.Host, config.Exported.Host)
	} else if config.inner.Host != "" || config.pointer != nil || config.private.Host != "" || config.logger != nil {
		t.Errorf("unexported fields were modified: %q, %v, %q, %v", config.inner.Host, config.pointer, config.private.Host, config.logger)
	} else if len(visible) != 1 || visible["host"] != "localhost" || len(hidden) != 0 {
		t.Errorf("unexpected properties: %v, %v", visible, hidden)
	}
}

func Test_populateTaggedUnexportedField(t *testing.T) {
	cases := map[string]any{
		"visible": &struct {
			Host string `config:"host"`
			port int    `config:"port,80"`
		}{},
		"hidden": &struct {
			Host     string `config:"host"`
			password string `hiddenconfig:"password"`
		}{},
		"nested": &struct {
			Inner struct {
				port int `config:"port"`
			} `config:""`
		}{},
		"struct": &struct {
			inner innerConfig `config:""`
		}{},
		"pointer": &struct {
			inner *innerConfig `config:""`
		}{},
	}
	for name, config := range cases {
		ctrl := gomock.NewController(t)
		provider := mapProvider(ctrl, map[string]string{"host": "localhost"})
		scheme := acceptingScheme(ctrl)

		if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
			t.Errorf("expected an error for a tagged unexported %s field", name)
		} else if !strings.Contains(err.Error(), "not exported") {
			t.Errorf("unexpected error for a tagged unexported %s field: %v", name, err)
		} else if visible != nil || hidden != nil {
			t.Errorf("expected no properties for a tagged unexported %s field", name)
		}
	}
}

func Test_populateEmptyTagUnexportedField(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)

	// an empty tag is treated as no tag, the same as for exported fields
	config := &struct {
		port int `config:" " hiddenconfig:""`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func Test_populateHidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"user": "admin", "password": "secret", "token": "abc123"})
	scheme := acceptingScheme(ctrl)

	config := &hiddenConfig{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.User != "admin" || config.Password != "secret" || config.Token != "abc123" || config.Missing != "" {
		t.Errorf("unexpected populated struct: %+v", *config)
	} else if len(visible) != 1 || visible["user"] != "admin" {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if len(hidden) != 2 || hidden["password"] != "secret" || hidden["token"] != "abc123" {
		t.Errorf("unexpected hidden properties: %v", hidden)
	}
}

func Test_populateHiddenDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{})
	scheme := acceptingScheme(ctrl)

	config := &hiddenConfig{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Token != "none" {
		t.Errorf("unexpected token: %q", config.Token)
	} else if len(visible) != 0 {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if len(hidden) != 1 || hidden["token"] != "none" {
		t.Errorf("unexpected hidden properties: %v", hidden)
	}
}

func Test_populateHiddenValidatesName(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("Password").Return(errors.New("invalid"))

	config := &struct {
		Password string `hiddenconfig:"Password"`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid hidden property name")
	} else if visible != nil || hidden != nil {
		t.Error("expected no properties for an invalid hidden property name")
	}
}

func Test_populateHiddenTooManyTagValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)

	config := &struct {
		Password string `hiddenconfig:"password,default,extra"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for too many hidden tag values")
	}
}

func Test_populateHiddenInvalidDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Port int `hiddenconfig:"port,eighty"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid hidden default value")
	} else if strings.Contains(err.Error(), "eighty") {
		t.Errorf("hidden default value was exposed in the error: %v", err)
	} else if !strings.Contains(err.Error(), "Port") || !strings.Contains(err.Error(), "int") {
		t.Errorf("expected the field name and type in the error: %v", err)
	}
}

func Test_populateHiddenInvalidProvided(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("pin").Return("s3cr3t-value", true)
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Pin int `hiddenconfig:"pin"`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid hidden value")
	} else if strings.Contains(err.Error(), "s3cr3t-value") {
		t.Errorf("hidden value was exposed in the error: %v", err)
	} else if !strings.Contains(err.Error(), "Pin") || !strings.Contains(err.Error(), "int") {
		t.Errorf("expected the field name and type in the error: %v", err)
	} else if visible != nil || hidden != nil {
		t.Error("expected no properties for an invalid hidden value")
	}
}

func Test_populateHiddenSlice(t *testing.T) {
	ctrl := gomock.NewController(t)
	keys := strings.Join([]string{"k1", "k2"}, SliceValueSeparator)
	provider := mapProvider(ctrl, map[string]string{"keys": keys})
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Keys []string `hiddenconfig:"keys"`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !slices.Equal(config.Keys, []string{"k1", "k2"}) {
		t.Errorf("unexpected keys: %v", config.Keys)
	} else if len(visible) != 0 || len(hidden) != 1 || hidden["keys"] != keys {
		t.Errorf("unexpected properties: %v, %v", visible, hidden)
	}
}

func Test_populateHiddenNested(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"name": "app", "user": "admin", "password": "secret"})
	scheme := acceptingScheme(ctrl)

	config := &nestedHiddenConfig{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Credentials.Password != "secret" || config.Pointer == nil || config.Pointer.Password != "secret" {
		t.Errorf("unexpected populated struct: %+v", *config)
	} else if len(visible) != 2 || visible["name"] != "app" || visible["user"] != "admin" {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if len(hidden) != 2 || hidden["password"] != "secret" || hidden["token"] != "none" {
		t.Errorf("unexpected hidden properties: %v", hidden)
	}
}

func Test_populateHiddenInvalidSliceElement(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("pins").Return(strings.Join([]string{"1234", "s3cr3t-value"}, SliceValueSeparator), true)
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Pins []int `hiddenconfig:"pins"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid hidden slice element")
	} else if strings.Contains(err.Error(), "s3cr3t-value") || strings.Contains(err.Error(), "1234") {
		t.Errorf("hidden value was exposed in the error: %v", err)
	}
}

func Test_populateVisibleInvalidProvided(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("port").Return("eighty", true)
	scheme := acceptingScheme(ctrl)

	// visible fields keep the full parsing error, which includes the value, to help diagnose problems
	config := &struct {
		Port int `config:"port"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid visible value")
	} else if !strings.Contains(err.Error(), "eighty") {
		t.Errorf("expected the value in the error: %v", err)
	}
}

func Test_populateBothTags(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"password": "secret"})
	scheme := acceptingScheme(ctrl)

	// a field can only be visible or hidden, so being tagged as both is ambiguous
	config := &struct {
		Password string `config:"password" hiddenconfig:"password"`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for a field with both tags")
	} else if visible != nil || hidden != nil {
		t.Error("expected no properties for a field with both tags")
	}
}

func Test_populateURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	endpoint := "https://user@example.com:8443/api/v1?limit=10#top"
	provider := mapProvider(ctrl, map[string]string{"endpoint": endpoint})
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Endpoint url.URL `config:"endpoint"`
	}{}
	if visible, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Endpoint.Scheme != "https" || config.Endpoint.Host != "example.com:8443" || config.Endpoint.Path != "/api/v1" {
		t.Errorf("unexpected endpoint: %+v", config.Endpoint)
	} else if config.Endpoint.RawQuery != "limit=10" || config.Endpoint.Fragment != "top" || config.Endpoint.User.Username() != "user" {
		t.Errorf("unexpected endpoint details: %+v", config.Endpoint)
	} else if config.Endpoint.String() != endpoint {
		t.Errorf("endpoint does not round-trip: %s", config.Endpoint.String())
	} else if visible["endpoint"] != endpoint {
		t.Errorf("unexpected resolved properties: %v", visible)
	}
}

func Test_populateURLDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{})
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Endpoint url.URL `config:"endpoint,http://localhost:8080/api"`
	}{}
	if visible, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Endpoint.String() != "http://localhost:8080/api" {
		t.Errorf("unexpected endpoint: %s", config.Endpoint.String())
	} else if visible["endpoint"] != "http://localhost:8080/api" {
		t.Errorf("unexpected resolved properties: %v", visible)
	}
}

func Test_populateURLMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{})
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Endpoint url.URL `config:"endpoint"`
	}{}
	if visible, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Endpoint != (url.URL{}) {
		t.Errorf("expected zero value for missing endpoint: %+v", config.Endpoint)
	} else if len(visible) != 0 {
		t.Errorf("unexpected resolved properties: %v", visible)
	}
}

func Test_populateInvalidURL(t *testing.T) {
	for _, value := range []string{"http://[::1", "http://example.com/%zz", "://missing-scheme", "http://example.com:port/"} {
		ctrl := gomock.NewController(t)
		provider := mocks.NewMockProvider(ctrl)
		provider.EXPECT().Value("endpoint").Return(value, true)
		scheme := acceptingScheme(ctrl)

		config := &struct {
			Endpoint url.URL `config:"endpoint"`
		}{}
		if visible, hidden, err := PopulateStruct(config, provider, scheme); err == nil {
			t.Errorf("expected an error for invalid URL %q", value)
		} else if visible != nil || hidden != nil {
			t.Errorf("expected no properties for invalid URL %q", value)
		}
	}
}

func Test_populateInvalidURLDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Endpoint url.URL `config:"endpoint,http://[::1"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid default URL")
	}
}

func Test_populateHiddenURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	endpoint := "postgres://admin:s3cr3t@db.example.com:5432/app"
	provider := mapProvider(ctrl, map[string]string{"database": endpoint})
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Database url.URL `hiddenconfig:"database"`
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if password, _ := config.Database.User.Password(); password != "s3cr3t" || config.Database.Host != "db.example.com:5432" {
		t.Errorf("unexpected database: %+v", config.Database)
	} else if len(visible) != 0 || hidden["database"] != endpoint {
		t.Errorf("unexpected properties: %v, %v", visible, hidden)
	}
}

func Test_populateHiddenInvalidURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("database").Return("postgres://admin:s3cr3t@[db", true)
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Database url.URL `hiddenconfig:"database"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err == nil {
		t.Error("expected an error for an invalid hidden URL")
	} else if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(err.Error(), "admin") {
		t.Errorf("hidden URL was exposed in the error: %v", err)
	} else if !strings.Contains(err.Error(), "url.URL") {
		t.Errorf("expected the type in the error: %v", err)
	}
}

func Test_populateURLSlice(t *testing.T) {
	ctrl := gomock.NewController(t)
	endpoints := strings.Join([]string{"http://a.example.com", " https://b.example.com:8443/path "}, SliceValueSeparator)
	provider := mapProvider(ctrl, map[string]string{"endpoints": endpoints})
	scheme := acceptingScheme(ctrl)

	config := &struct {
		Endpoints []url.URL `config:"endpoints"`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if len(config.Endpoints) != 2 {
		t.Errorf("unexpected endpoints: %v", config.Endpoints)
	} else if config.Endpoints[0].String() != "http://a.example.com" || config.Endpoints[1].String() != "https://b.example.com:8443/path" {
		t.Errorf("unexpected endpoints: %s, %s", config.Endpoints[0].String(), config.Endpoints[1].String())
	}
}

func Test_populateNestedURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mapProvider(ctrl, map[string]string{"endpoint": "http://localhost:8080"})
	scheme := acceptingScheme(ctrl)

	type serverConfig struct {
		Endpoint url.URL `config:"endpoint"`
	}
	config := &struct {
		Server  serverConfig  `config:""`
		Pointer *serverConfig `config:""`
	}{}
	if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Server.Endpoint.Host != "localhost:8080" {
		t.Errorf("unexpected server endpoint: %+v", config.Server.Endpoint)
	} else if config.Pointer == nil || config.Pointer.Endpoint.Host != "localhost:8080" {
		t.Errorf("unexpected pointer endpoint: %+v", config.Pointer)
	}
}

func Test_populateUntaggedURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)

	// a url.URL is a base value, so an untagged one is skipped rather than populated as a struct
	config := &struct {
		Endpoint url.URL
	}{}
	if visible, hidden, err := PopulateStruct(config, provider, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if len(visible) != 0 || len(hidden) != 0 || config.Endpoint != (url.URL{}) {
		t.Errorf("untagged URL was populated: %+v, %v, %v", config.Endpoint, visible, hidden)
	}
}

func Test_populateRelativeURL(t *testing.T) {
	// relative references are deliberately supported, so values without a scheme or host are
	// kept as parsed rather than rejected
	cases := map[string]url.URL{
		"/api/v1":         {Path: "/api/v1"},
		"api/v1?limit=10": {Path: "api/v1", RawQuery: "limit=10"},
		"../shared":       {Path: "../shared"},
		"#section":        {Fragment: "section"},
		"//cdn.example":   {Host: "cdn.example"},
	}
	for value, expected := range cases {
		ctrl := gomock.NewController(t)
		provider := mocks.NewMockProvider(ctrl)
		provider.EXPECT().Value("endpoint").Return(value, true)
		scheme := acceptingScheme(ctrl)

		config := &struct {
			Endpoint url.URL `config:"endpoint"`
		}{}
		if _, _, err := PopulateStruct(config, provider, scheme); err != nil {
			t.Errorf("unexpected error for relative URL %q: %v", value, err)
		} else if config.Endpoint != expected {
			t.Errorf("unexpected URL for %q: %+v", value, config.Endpoint)
		} else if config.Endpoint.String() != value {
			t.Errorf("relative URL %q does not round-trip: %s", value, config.Endpoint.String())
		}
	}
}
