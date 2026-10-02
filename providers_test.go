/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"errors"
	"maps"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/tranquildata/config/mocks"
)

func newEnvironmentProvider(t *testing.T, prefix string, scheme NamingScheme) Provider {
	t.Helper()
	provider, err := EnvironmentProvider(prefix, scheme)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return provider
}

func Test_listProviderEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	if provider, err := ListProvider(nil, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if _, present := provider.Value("root"); present {
		t.Error("expected no value from an empty provider")
	}
}

func Test_listProviderValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("root").Return(nil)
	scheme.EXPECT().Validate("port").Return(nil)
	scheme.EXPECT().Validate("empty").Return(nil)

	if provider, err := ListProvider([]string{"root=/opt/app", "  port=8080\t", "empty="}, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if value, present := provider.Value("root"); !present || value != "/opt/app" {
		t.Errorf("unexpected value for root: %q (%t)", value, present)
	} else if value, present = provider.Value("port"); !present || value != "8080" {
		t.Errorf("unexpected value for trimmed port: %q (%t)", value, present)
	} else if value, present = provider.Value("empty"); !present || value != "" {
		t.Errorf("unexpected value for empty: %q (%t)", value, present)
	} else if _, present = provider.Value("missing"); present {
		t.Error("expected no value for missing property")
	}
}

func Test_listProviderMisformed(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	for _, element := range []string{"root", "", "   "} {
		if provider, err := ListProvider([]string{element}, scheme); err == nil {
			t.Errorf("expected an error for misformed element %q", element)
		} else if provider != nil {
			t.Errorf("expected no provider for misformed element %q", element)
		}
	}
}

func Test_listProviderInvalidName(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("Root").Return(errors.New("invalid"))

	if provider, err := ListProvider([]string{"Root=/opt/app"}, scheme); err == nil {
		t.Error("expected an error for an invalid property name")
	} else if provider != nil {
		t.Error("expected no provider for an invalid property name")
	}
}

func Test_listProviderRepeated(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := acceptingScheme(ctrl)

	for _, elements := range [][]string{{"root=/a", "root=/b"}, {"root=/a", "port=1", "root=/a"}, {"empty=", " empty= "}} {
		if provider, err := ListProvider(elements, scheme); err == nil {
			t.Errorf("expected an error for repeated elements %q", elements)
		} else if provider != nil {
			t.Errorf("expected no provider for repeated elements %q", elements)
		}
	}
}

func Test_environmentProviderValidPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	for _, prefix := range []string{NoEnvironmentPrefix, "   ", "app", "App2", "my_app", "MY_APP_", " app "} {
		if provider, err := EnvironmentProvider(prefix, scheme); err != nil {
			t.Errorf("unexpected error for prefix %q: %v", prefix, err)
		} else if provider == nil {
			t.Errorf("expected a provider for prefix %q", prefix)
		}
	}
}

func Test_environmentProviderInvalidPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	for _, prefix := range []string{"my-app", "my app", "my.app", "1app", "_app", "app$", "app=x", "äpp"} {
		if provider, err := EnvironmentProvider(prefix, scheme); err == nil {
			t.Errorf("expected an error for prefix %q", prefix)
		} else if provider != nil {
			t.Errorf("expected no provider for prefix %q", prefix)
		}
	}
}

func Test_mapProviderValues(t *testing.T) {
	provider := MapProvider(map[string]string{"root": "/opt/app", "empty": ""})

	if value, present := provider.Value("root"); !present || value != "/opt/app" {
		t.Errorf("unexpected value for root: %q (%t)", value, present)
	} else if value, present = provider.Value("empty"); !present || value != "" {
		t.Errorf("unexpected value for empty: %q (%t)", value, present)
	} else if _, present = provider.Value("missing"); present {
		t.Error("expected no value for missing property")
	}
}

func Test_mapProviderNil(t *testing.T) {
	if _, present := MapProvider(nil).Value("root"); present {
		t.Error("expected no value from a nil map")
	}
}

func Test_mapProviderIsolated(t *testing.T) {
	properties := map[string]string{"root": "/opt/app"}
	provider := MapProvider(properties)
	properties["root"] = "/changed"
	properties["added"] = "value"

	if value, _ := provider.Value("root"); value != "/opt/app" {
		t.Errorf("provider was affected by change to source map: %q", value)
	} else if _, present := provider.Value("added"); present {
		t.Error("provider was affected by addition to source map")
	}
}

func Test_environmentProviderWithPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Components("rootDirectory").Return([]string{"root", "Directory"})
	t.Setenv("CONFIGTEST_ROOT_DIRECTORY", "/opt/app")

	if value, present := newEnvironmentProvider(t, " configTest ", scheme).Value("rootDirectory"); !present || value != "/opt/app" {
		t.Errorf("unexpected value from environment: %q (%t)", value, present)
	}
}

func Test_environmentProviderNoPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Components("configtest_root").Return([]string{"configtest", "root"})
	t.Setenv("CONFIGTEST_ROOT", "/opt/app")

	if value, present := newEnvironmentProvider(t, NoEnvironmentPrefix, scheme).Value("configtest_root"); !present || value != "/opt/app" {
		t.Errorf("unexpected value from environment: %q (%t)", value, present)
	}
}

func Test_environmentProviderEmptyValue(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Components("root").Return([]string{"root"})
	t.Setenv("CONFIGTEST_ROOT", "")

	if value, present := newEnvironmentProvider(t, "configtest", scheme).Value("root"); !present || value != "" {
		t.Errorf("unexpected value from environment: %q (%t)", value, present)
	}
}

func Test_environmentProviderMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Components("missing").Return([]string{"missing"})

	if _, present := newEnvironmentProvider(t, "configtest", scheme).Value("missing"); present {
		t.Error("expected no value for missing environment variable")
	}
}

func Test_chainedProviderFirst(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().Value("root").Return("/first", true)
	next := mocks.NewMockProvider(ctrl)

	if value, present := ChainedProvider(first, next).Value("root"); !present || value != "/first" {
		t.Errorf("unexpected value from chain: %q (%t)", value, present)
	}
}

func Test_chainedProviderNext(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().Value("root").Return("", false)
	next := mocks.NewMockProvider(ctrl)
	next.EXPECT().Value("root").Return("/next", true)

	if value, present := ChainedProvider(first, next).Value("root"); !present || value != "/next" {
		t.Errorf("unexpected value from chain: %q (%t)", value, present)
	}
}

func Test_chainedProviderFirstEmptyValue(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().Value("root").Return("", true)
	next := mocks.NewMockProvider(ctrl)

	if value, present := ChainedProvider(first, next).Value("root"); !present || value != "" {
		t.Errorf("unexpected value from chain: %q (%t)", value, present)
	}
}

func Test_chainedProviderMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().Value("root").Return("", false)
	next := mocks.NewMockProvider(ctrl)
	next.EXPECT().Value("root").Return("", false)

	if _, present := ChainedProvider(first, next).Value("root"); present {
		t.Error("expected no value from chain")
	}
}

func Test_chainedProviderNested(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().Value("root").Return("", false)
	second := mocks.NewMockProvider(ctrl)
	second.EXPECT().Value("root").Return("", false)
	third := mocks.NewMockProvider(ctrl)
	third.EXPECT().Value("root").Return("/third", true)

	chain := ChainedProvider(ChainedProvider(first, second), third)
	if value, present := chain.Value("root"); !present || value != "/third" {
		t.Errorf("unexpected value from chain: %q (%t)", value, present)
	}
}

func Test_listProviderKnownProperties(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := acceptingScheme(ctrl)

	if provider, err := ListProvider([]string{"root=/opt/app", "port=8080", "empty="}, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if known := provider.KnownProperties(); !maps.Equal(known, map[string]string{"root": "/opt/app", "port": "8080", "empty": ""}) {
		t.Errorf("unexpected known properties: %v", known)
	}
}

func Test_listProviderKnownPropertiesEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	if provider, err := ListProvider(nil, scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if known := provider.KnownProperties(); known == nil || len(known) != 0 {
		t.Errorf("expected empty known properties: %v", known)
	}
}

func Test_mapProviderKnownProperties(t *testing.T) {
	properties := map[string]string{"root": "/opt/app", "empty": ""}
	provider := MapProvider(properties)

	if known := provider.KnownProperties(); !maps.Equal(known, properties) {
		t.Errorf("unexpected known properties: %v", known)
	}
	if known := MapProvider(nil).KnownProperties(); len(known) != 0 {
		t.Errorf("expected no known properties from a nil map: %v", known)
	}
}

func Test_mapProviderKnownPropertiesIsolated(t *testing.T) {
	provider := MapProvider(map[string]string{"root": "/opt/app"})

	known := provider.KnownProperties()
	known["root"] = "/changed"
	known["added"] = "value"

	if value, _ := provider.Value("root"); value != "/opt/app" {
		t.Errorf("provider was affected by change to known properties: %q", value)
	} else if again := provider.KnownProperties(); len(again) != 1 || again["root"] != "/opt/app" {
		t.Errorf("known properties were affected by caller: %v", again)
	}
}

func Test_environmentProviderKnownProperties(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	t.Setenv("CONFIGTEST_ROOT", "/opt/app")

	// the environment can't be enumerated as property names, so nothing is reported as known
	if known := newEnvironmentProvider(t, "configtest", scheme).KnownProperties(); known == nil || len(known) != 0 {
		t.Errorf("expected empty known properties: %v", known)
	}
}

func Test_chainedProviderKnownProperties(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().KnownProperties().Return(map[string]string{"root": "/first", "port": "1"})
	next := mocks.NewMockProvider(ctrl)
	next.EXPECT().KnownProperties().Return(map[string]string{"root": "/next", "host": "nexthost"})

	// known properties must agree with Value, so the first provider takes priority
	expected := map[string]string{"root": "/first", "port": "1", "host": "nexthost"}
	if known := ChainedProvider(first, next).KnownProperties(); !maps.Equal(known, expected) {
		t.Errorf("unexpected known properties: %v", known)
	}
}

func Test_chainedProviderKnownPropertiesNested(t *testing.T) {
	first := MapProvider(map[string]string{"a": "first"})
	second := MapProvider(map[string]string{"a": "second", "b": "second"})
	third := MapProvider(map[string]string{"a": "third", "b": "third", "c": "third"})

	expected := map[string]string{"a": "first", "b": "second", "c": "third"}
	for name, chain := range map[string]Provider{
		"left":  ChainedProvider(ChainedProvider(first, second), third),
		"right": ChainedProvider(first, ChainedProvider(second, third)),
	} {
		if known := chain.KnownProperties(); !maps.Equal(known, expected) {
			t.Errorf("unexpected known properties for %s-nested chain: %v", name, known)
		}
		for property, value := range expected {
			if provided, _ := chain.Value(property); provided != value {
				t.Errorf("known properties disagree with value for %s in %s-nested chain: %q", property, name, provided)
			}
		}
	}
}

func Test_chainedProviderKnownPropertiesDoesNotModifyProviders(t *testing.T) {
	ctrl := gomock.NewController(t)
	firstProperties := map[string]string{"root": "/first"}
	nextProperties := map[string]string{"host": "nexthost"}
	first := mocks.NewMockProvider(ctrl)
	first.EXPECT().KnownProperties().Return(firstProperties)
	next := mocks.NewMockProvider(ctrl)
	next.EXPECT().KnownProperties().Return(nextProperties)

	// a provider may return its own map, so the chain must not merge into either result
	ChainedProvider(first, next).KnownProperties()
	if len(firstProperties) != 1 || len(nextProperties) != 1 {
		t.Errorf("provider maps were modified: %v, %v", firstProperties, nextProperties)
	}
}
